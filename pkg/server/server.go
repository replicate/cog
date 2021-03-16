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
	"time"

	"github.com/gorilla/mux"
	"github.com/mholt/archiver/v3"
	log "github.com/sirupsen/logrus"

	"github.com/replicate/modelserver/pkg/database"
	"github.com/replicate/modelserver/pkg/docker"
	"github.com/replicate/modelserver/pkg/model"
	"github.com/replicate/modelserver/pkg/serving"
	"github.com/replicate/modelserver/pkg/storage"
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
	router.Path("/v1/packages/upload").
		Methods("POST").
		HandlerFunc(s.ReceiveFile)
	router.Path("/v1/packages/{id}.zip").
		Methods("GET").
		HandlerFunc(s.SendModelPackage)
	router.Path("/v1/packages/{id}").
		Methods("GET").
		HandlerFunc(s.SendModelMetadata)
	router.Path("/v1/packages/").
		Methods("GET").
		HandlerFunc(s.SendAllModelsMetadata)
	fmt.Println("Starting")
	return http.ListenAndServe(fmt.Sprintf(":%d", s.port), router)
}

func (s *Server) ReceiveFile(w http.ResponseWriter, r *http.Request) {
	mod, err := s.ReceiveModel(r)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
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

func (s *Server) SendModelPackage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	log.Infof("Received download request for %s", id)
	modTime := time.Now() // TODO
	content, err := s.store.Download(id)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	log.Infof("Downloaded %d bytes", len(content))
	http.ServeContent(w, r, id+".zip", modTime, bytes.NewReader(content))
}

func (s *Server) SendModelMetadata(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	log.Infof("Received get request for %s", id)

	mod, err := s.db.GetModelByID(id)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
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

func (s *Server) SendAllModelsMetadata(w http.ResponseWriter, r *http.Request) {
	log.Info("Received list request")

	models, err := s.db.ListModels()
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

func (s *Server) ReceiveModel(r *http.Request) (*model.Model, error) {
	//queryVars := r.URL.Query()

	// max 5GB models
	if err := r.ParseMultipartForm(5 << 30); err != nil {
		return nil, fmt.Errorf("Failed to parse request: %w", err)
	}
	file, header, err := r.FormFile("file")
	defer file.Close()

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

	configRaw, err := os.ReadFile(filepath.Join(dir, "cog.yaml"))
	if err != nil {
		return nil, fmt.Errorf("Failed to read cog.yaml: %w", err)
	}
	config, err := model.ConfigFromYAML(configRaw)
	if err != nil {
		return nil, err
	}
	log.Infof("Received model %s", config.Name)

	if err := config.ValidateAndCompleteConfig(); err != nil {
		return nil, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("Failed to rewind file: %w", err)
	}
	if err := s.store.Upload(file, id); err != nil {
		return nil, fmt.Errorf("Failed to upload to storage: %w", err)
	}

	artifacts, err := s.buildDockerImages(dir, config)
	if err != nil {
		return nil, err
	}
	mod := &model.Model{
		ID:        id,
		Name:      config.Name,
		Artifacts: artifacts,
		Config:    config,
	}

	if err := s.testModel(mod); err != nil {
		return nil, err
	}

	log.Info("Inserting into database")
	if err := s.db.InsertModel(mod); err != nil {
		return nil, fmt.Errorf("Failed to insert into database: %w", err)
	}

	return mod, nil
}

func (s *Server) testModel(mod *model.Model) error {
	log.Debug("Testing model")
	deployment, err := s.servingPlatform.Deploy(mod, model.TargetDockerCPU)
	if err != nil {
		return err
	}
	defer deployment.Undeploy()

	input := &serving.Example{}
	result, err := deployment.RunInference(input)
	if err != nil {
		return err
	}
	log.Infof("Inference result length: %d", len(result.Values["output"]))

	return nil
}

func (s *Server) buildDockerImages(dir string, config *model.Config) ([]*model.Artifact, error) {
	// TODO(andreas): parallelize
	artifacts := []*model.Artifact{}
	for _, arch := range config.Environment.Architectures {
		generator := &DockerfileGenerator{config, arch}
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

		tag, err := s.dockerImageBuilder.BuildAndPush(dir, relDockerfilePath, config.Name)
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
