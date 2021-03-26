package server

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mholt/archiver/v3"
	log "github.com/sirupsen/logrus"

	"github.com/replicate/cog/pkg/database"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/storage"
)

// TODO(andreas): decouple saving zip files from image building into two separate API calls?
// TODO(andreas): separate targets for different CUDA versions? how does that change the yaml design?

const topLevelSourceDir = "source"

type Server struct {
	port               int
	db                 database.Database
	dockerImageBuilder docker.ImageBuilder
	servingPlatform    serving.Platform
	store              storage.Storage
}

func NewServer(port int, db database.Database, dockerImageBuilder docker.ImageBuilder, servingPlatform serving.Platform, store storage.Storage) *Server {
	return &Server{
		port:               port,
		db:                 db,
		dockerImageBuilder: dockerImageBuilder,
		servingPlatform:    servingPlatform,
		store:              store,
	}
}

func (s *Server) Start() error {
	router := mux.NewRouter()
	router.Path("/ping").
		Methods(http.MethodGet).
		HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Info("Received ping request")
			w.Write([]byte("pong"))
		})
	router.Path("/v1/repos/{user}/{name}/packages/{id}.zip").
		Methods(http.MethodGet).
		HandlerFunc(s.SendModelPackage)
	router.Path("/v1/repos/{user}/{name}/packages/").
		Methods(http.MethodPut).
		HandlerFunc(s.ReceiveFile)
	router.Path("/v1/repos/{user}/{name}/packages/").
		Methods(http.MethodGet).
		HandlerFunc(s.ListPackages)
	router.Path("/v1/repos/{user}/{name}/packages/{id}").
		Methods(http.MethodGet).
		HandlerFunc(s.SendModelMetadata)
	router.Path("/v1/repos/{user}/{name}/packages/{id}").
		Methods(http.MethodDelete).
		HandlerFunc(s.DeletePackage)
	fmt.Println("Starting")
	return http.ListenAndServe(fmt.Sprintf(":%d", s.port), router)
}

func (s *Server) ReceiveFile(w http.ResponseWriter, r *http.Request) {
	user, name, _ := getRepoVars(r)

	log.Infof("Received build request")
	streamLogger := logger.NewStreamLogger(w)
	mod, err := s.ReceiveModel(r, streamLogger, user, name)
	if err != nil {
		streamLogger.WriteError(err)
		log.Error(err)
		return
	}
	streamLogger.WriteModel(mod)
}

func (s *Server) SendModelPackage(w http.ResponseWriter, r *http.Request) {
	user, name, id := getRepoVars(r)
	log.Infof("Received download request for %s/%s/%s", user, name, id)
	modTime := time.Now() // TODO

	mod, err := s.db.GetModel(user, name, id)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if mod == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	content, err := s.store.Download(user, name, id)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	log.Infof("Downloaded %d bytes", len(content))
	http.ServeContent(w, r, id+".zip", modTime, bytes.NewReader(content))
}

func (s *Server) SendModelMetadata(w http.ResponseWriter, r *http.Request) {
	user, name, id := getRepoVars(r)
	log.Infof("Received get request for %s/%s/%s", user, name, id)

	mod, err := s.db.GetModel(user, name, id)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if mod == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(mod); err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (s *Server) ListPackages(w http.ResponseWriter, r *http.Request) {
	user, name, _ := getRepoVars(r)
	log.Infof("Received list request for %s%s", user, name)

	models, err := s.db.ListModels(user, name)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(models); err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (s *Server) DeletePackage(w http.ResponseWriter, r *http.Request) {
	user, name, id := getRepoVars(r)
	log.Infof("Received delete request for %s/%s/%s", user, name, id)

	mod, err := s.db.GetModel(user, name, id)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if mod == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if err := s.store.Delete(user, name, id); err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err := s.db.DeleteModel(user, name, id); err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Deleted " + id))
}

func (s *Server) ReceiveModel(r *http.Request, streamLogger *logger.StreamLogger, user string, name string) (*model.Model, error) {
	// max 5GB models
	if err := r.ParseMultipartForm(5 << 30); err != nil {
		return nil, fmt.Errorf("Failed to parse request: %w", err)
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("Failed to read input file: %w", err)
	}
	defer file.Close()

	streamLogger.WriteStatus("Received model")

	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, fmt.Errorf("Failed to compute hash: %w", err)
	}
	id := fmt.Sprintf("%x", hasher.Sum(nil))

	parentDir, err := os.MkdirTemp("/tmp", "unzip")
	if err != nil {
		return nil, fmt.Errorf("Failed to make tempdir: %w", err)
	}
	dir := filepath.Join(parentDir, topLevelSourceDir)
	if err := os.Mkdir(dir, 0755); err != nil {
		return nil, fmt.Errorf("Failed to make source dir: %w", err)
	}
	z := archiver.Zip{}
	if err := z.ReaderUnarchive(file, header.Size, dir); err != nil {
		return nil, fmt.Errorf("Failed to unzip: %w", err)
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

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("Failed to rewind file: %w", err)
	}
	if err := s.store.Upload(user, name, id, file); err != nil {
		return nil, fmt.Errorf("Failed to upload to storage: %w", err)
	}

	artifacts, err := s.buildDockerImages(dir, config, name, streamLogger)
	if err != nil {
		return nil, err
	}
	mod := &model.Model{
		ID:        id,
		Artifacts: artifacts,
		Config:    config,
		Created:   time.Now(),
	}

	runArgs, err := s.testModel(mod, dir, streamLogger)
	if err != nil {
		// TODO(andreas): return other response than 500 if validation fails
		return nil, err
	}
	mod.RunArguments = runArgs

	streamLogger.WriteStatus("Inserting into database")
	if err := s.db.InsertModel(user, name, id, mod); err != nil {
		return nil, fmt.Errorf("Failed to insert into database: %w", err)
	}

	return mod, nil
}

func (s *Server) testModel(mod *model.Model, dir string, logWriter logger.Logger) (map[string]*model.RunArgument, error) {
	logWriter.WriteStatus("Testing model")
	deployment, err := s.servingPlatform.Deploy(mod, model.TargetDockerCPU, logWriter)
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
		logWriter.WriteLogLine(fmt.Sprintf("Inference result length: %d, mime type: %s", len(outputBytes), output.MimeType))
		if example.Output != "" && string(outputBytes) != example.Output {
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
		var target model.Target
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

func getRepoVars(r *http.Request) (user string, name string, id string) {
	vars := mux.Vars(r)
	return vars["user"], vars["name"], vars["id"]
}
