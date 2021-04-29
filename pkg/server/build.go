package server

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
	// keep 128MB in memory while parsing the input
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		return nil, fmt.Errorf("Failed to parse request: %w", err)
	}
	inputFile, header, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("Failed to read input file: %w", err)
	}
	defer inputFile.Close()

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
	logWriter.Infof("Received model")

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

	artifacts, err := s.buildDockerImages(dir, config, name, logWriter)
	if err != nil {
		return nil, err
	}
	mod := &model.Model{
		Artifacts: artifacts,
		Config:    config,
		Created:   time.Now(),
	}

	testTarget := model.TargetDockerCPU
	if _, ok := mod.ArtifactFor(model.TargetDockerCPU); !ok {
		if _, ok := mod.ArtifactFor(model.TargetDockerGPU); ok {
			testTarget = model.TargetDockerGPU
		} else {
			return nil, fmt.Errorf("Model has neither CPU or GPU target")
		}
	}
	testArtifact, ok := mod.ArtifactFor(testTarget)
	if !ok {
		return nil, fmt.Errorf("Model has no %s target", testTarget)
	}
	runArgs, modelStats, err := serving.TestModel(s.servingPlatform, testArtifact.URI, mod.Config, dir, logWriter)
	if err != nil {
		// TODO(andreas): return other response than 500 if validation fails
		return nil, err
	}

	z2 := &archiver.Zip{ImplicitTopLevelFolder: false}
	zipTempDir, err := os.MkdirTemp("/tmp", "zip")
	if err != nil {
		return nil, fmt.Errorf("Failed to make tempdir: %w", err)
	}
	zipOutputPath := filepath.Join(zipTempDir, "out.zip")
	if err := z2.Archive([]string{dir + "/"}, zipOutputPath); err != nil {
		return nil, fmt.Errorf("Failed to zip directory: %w", err)
	}

	file, err := os.Open(zipOutputPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	id, err := computeID(dir)
	if err != nil {
		return nil, err
	}
	if err := s.store.Upload(user, name, id, file); err != nil {
		return nil, fmt.Errorf("Failed to upload to storage: %w", err)
	}
	mod.ID = id
	mod.RunArguments = runArgs
	mod.Stats = modelStats

	if err := s.pushDockerImages(dir, mod, logWriter); err != nil {
		return nil, err
	}

	logWriter.WriteStatus("Inserting into database")
	if err := s.db.InsertModel(user, name, id, mod); err != nil {
		return nil, fmt.Errorf("Failed to insert into database: %w", err)
	}

	if err := s.runWebHooks(user, name, mod, dir, logWriter); err != nil {
		return nil, err
	}

	return mod, nil
}

// TODO(andreas): include user in docker image name?
func (s *Server) buildDockerImages(dir string, config *model.Config, name string, logWriter logger.Logger) ([]*model.Artifact, error) {
	// TODO(andreas): parallelize
	artifacts := []*model.Artifact{}
	for _, arch := range config.Environment.Architectures {
		logWriter.WriteStatus("Building %s image", arch)

		generator := &docker.DockerfileGenerator{Config: config, Arch: arch, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
		dockerfileContents, err := generator.Generate()
		if err != nil {
			return nil, fmt.Errorf("Failed to generate Dockerfile for %s: %w", arch, err)
		}
		tag, err := s.dockerImageBuilder.Build(dir, dockerfileContents, name, logWriter)
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

func (s *Server) pushDockerImages(dir string, model *model.Model, logWriter logger.Logger) error {
	for _, artifact := range model.Artifacts {
		if err := s.dockerImageBuilder.Push(artifact.URI, logWriter); err != nil {
			return err
		}
	}
	return nil
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

func computeID(dir string) (string, error) {
	hasher := sha1.New()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		// TODO(andreas): test that non-deterministic output examples don't change ID
		if d.Name() == serving.ExampleOutputDir {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("Failed to open %s: %w", path, err)
		}
		defer file.Close()
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
