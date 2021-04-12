package server

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mholt/archiver/v3"

	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/zip"
)

func (s *Server) ReceiveFile(w http.ResponseWriter, r *http.Request) {
	user, name, _ := getRepoVars(r)

	console.Info("Received build request")
	streamLogger := logger.NewStreamLogger(w)
	mod, err := s.ReceiveModel(r, streamLogger, user, name)
	if err != nil {
		streamLogger.WriteError(err)
		console.Error(err.Error())
		return
	}
	streamLogger.WriteModel(mod)
}

func (s *Server) ReceiveModel(r *http.Request, logWriter logger.Logger, user string, name string) (*model.Model, error) {
	// max 5GB models
	if err := r.ParseMultipartForm(5 << 30); err != nil {
		return nil, fmt.Errorf("Failed to parse request: %w", err)
	}
	inputFile, header, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("Failed to read input file: %w", err)
	}
	defer inputFile.Close()

	logWriter.WriteStatus("Received model")

	parentDir, err := os.MkdirTemp("/tmp", "unzip")
	if err != nil {
		return nil, fmt.Errorf("Failed to make tempdir: %w", err)
	}
	dir := filepath.Join(parentDir, topLevelSourceDir)
	if err := os.Mkdir(dir, 0755); err != nil {
		return nil, fmt.Errorf("Failed to make source dir: %w", err)
	}
	zipCache, err := zip.NewRepoCache(user, name)
	if err != nil {
		return nil, err
	}
	z := zip.NewCachingZip()
	if err := z.ReaderUnarchive(inputFile, header.Size, dir, zipCache); err != nil {
		return nil, fmt.Errorf("Failed to unzip: %w", err)
	}
	id, err := computeID(dir)
	if err != nil {
		return nil, err
	}

	configRaw, err := os.ReadFile(filepath.Join(dir, global.ConfigFilename))
	if err != nil {
		return nil, fmt.Errorf("Failed to read %s: %w", global.ConfigFilename, err)
	}
	config, err := model.ConfigFromYAML(configRaw)
	if err != nil {
		return nil, err
	}

	if err := config.ValidateAndCompleteConfig(); err != nil {
		return nil, err
	}

	// make zip file for storage
	file := new(bytes.Buffer)
	z2 := &archiver.Zip{ImplicitTopLevelFolder: false}
	if err := z2.WriterArchive([]string{dir + "/"}, file); err != nil {
		return nil, fmt.Errorf("Failed to zip directory: %w", err)
	}

	if err := s.store.Upload(user, name, id, file); err != nil {
		return nil, fmt.Errorf("Failed to upload to storage: %w", err)
	}

	artifacts, err := s.buildDockerImages(dir, config, name, logWriter)
	if err != nil {
		return nil, err
	}
	mod := &model.Model{
		ID:        id,
		Artifacts: artifacts,
		Config:    config,
		Created:   time.Now(),
	}

	runArgs, err := s.testModel(mod, dir, logWriter)
	if err != nil {
		// TODO(andreas): return other response than 500 if validation fails
		return nil, err
	}
	mod.RunArguments = runArgs

	logWriter.WriteStatus("Inserting into database")
	if err := s.db.InsertModel(user, name, id, mod); err != nil {
		return nil, fmt.Errorf("Failed to insert into database: %w", err)
	}

	if err := s.runWebHooks(user, name, mod, dir, logWriter); err != nil {
		return nil, err
	}

	return mod, nil
}

func (s *Server) testModel(mod *model.Model, dir string, logWriter logger.Logger) (map[string]*model.RunArgument, error) {
	logWriter.WriteStatus("Testing model")
	target := model.TargetDockerCPU
	if _, ok := mod.ArtifactFor(model.TargetDockerCPU); !ok {
		if _, ok := mod.ArtifactFor(model.TargetDockerGPU); ok {
			target = model.TargetDockerGPU
		} else {
			return nil, fmt.Errorf("Model has neither CPU or GPU target")
		}
	}

	deployment, err := s.servingPlatform.Deploy(mod, target, logWriter)
	if err != nil {
		return nil, err
	}
	defer deployment.Undeploy()

	help, err := deployment.Help(logWriter)
	if err != nil {
		return nil, err
	}

	for _, example := range mod.Config.Examples {
		if err := validateServingExampleInput(help, example.Input); err != nil {
			return nil, fmt.Errorf("Example input doesn't match run arguments: %w", err)
		}
		input := serving.NewExampleWithBaseDir(example.Input, dir)

		result, err := deployment.RunInference(input, logWriter)
		if err != nil {
			return nil, err
		}
		output := result.Values["output"]
		outputBytes, err := io.ReadAll(output.Buffer)
		if err != nil {
			return nil, fmt.Errorf("Failed to read output: %w", err)
		}
		logWriter.Infof(fmt.Sprintf("Inference result length: %d, mime type: %s", len(outputBytes), output.MimeType))
		if example.Output != "" && strings.TrimSpace(string(outputBytes)) != example.Output {
			return nil, fmt.Errorf("Output %s doesn't match expected: %s", outputBytes, example.Output)
		}
	}

	return help.Arguments, nil
}

// TODO(andreas): include user in docker image name?
func (s *Server) buildDockerImages(dir string, config *model.Config, name string, logWriter logger.Logger) ([]*model.Artifact, error) {
	// TODO(andreas): parallelize
	artifacts := []*model.Artifact{}
	for _, arch := range config.Environment.Architectures {

		logWriter.WriteStatus("Building %s image", arch)

		generator := &docker.DockerfileGenerator{Config: config, Arch: arch}
		dockerfileContents, err := generator.Generate()
		if err != nil {
			return nil, fmt.Errorf("Failed to generate Dockerfile for %s: %w", arch, err)
		}
		// TODO(andreas): pipe dockerfile contents to builder
		relDockerfilePath := "Dockerfile." + arch
		dockerfilePath := filepath.Join(dir, relDockerfilePath)
		if err := os.WriteFile(dockerfilePath, []byte(dockerfileContents), 0644); err != nil {
			return nil, fmt.Errorf("Failed to write Dockerfile for %s", arch)
		}

		tag, err := s.dockerImageBuilder.BuildAndPush(dir, relDockerfilePath, name, logWriter)
		if err != nil {
			return nil, fmt.Errorf("Failed to build Docker image: %w", err)
		}
		var target string
		switch arch {
		case "cpu":
			target = model.TargetDockerCPU
		case "gpu":
			target = model.TargetDockerGPU
		}
		artifacts = append(artifacts, &model.Artifact{
			Target: target,
			URI:    tag,
		})

	}
	return artifacts, nil
}

func (s *Server) GetCacheHashes(w http.ResponseWriter, r *http.Request) {
	user, name, _ := getRepoVars(r)
	console.Infof("Received cache-hashes request for %s/%s", user, name)

	zipCache, err := zip.NewRepoCache(user, name)
	if err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	hashes, err := zipCache.GetHashes()
	if err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(hashes); err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func validateServingExampleInput(help *serving.HelpResponse, input map[string]string) error {
	// TODO(andreas): validate types
	missingNames := []string{}
	extraneousNames := []string{}

	for name, arg := range help.Arguments {
		if _, ok := input[name]; !ok && arg.Default == nil {
			missingNames = append(missingNames, name)
		}
	}
	for name := range input {
		if _, ok := help.Arguments[name]; !ok {
			extraneousNames = append(extraneousNames, name)
		}
	}
	errParts := []string{}
	if len(missingNames) > 0 {
		errParts = append(errParts, "Missing arguments: "+strings.Join(missingNames, ", "))
	}
	if len(extraneousNames) > 0 {
		errParts = append(errParts, "Extraneous arguments: "+strings.Join(extraneousNames, ", "))
	}
	if len(errParts) > 0 {
		return fmt.Errorf(strings.Join(errParts, "; "))
	}
	return nil
}

func computeID(dir string) (string, error) {
	hasher := sha1.New()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("Failed to open %s: %w", path, err)
		}
		if _, err := io.Copy(hasher, file); err != nil {
			return fmt.Errorf("Failed to read %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
