package server

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/dchest/uniuri"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/replicate/modelserver/pkg/global"
)

type Server struct {
	db         *DB
	cloudbuild *CloudBuild
	ai         *AIPlatform
	storage    *Storage
}

func NewServer() (*Server, error) {
	s := new(Server)
	return s, nil
}

func (s *Server) Start() error {
	var err error
	s.db, err = NewDB()
	if err != nil {
		return err
	}
	defer s.db.Close()

	s.cloudbuild, err = NewCloudBuild()
	if err != nil {
		return fmt.Errorf("Failed to connect to CloudBuild: %w", err)
	}

	s.ai, err = NewAIPlatform()
	if err != nil {
		return fmt.Errorf("Failed to connect to AI Platform: %w", err)
	}

	s.storage, err = NewStorage()
	if err != nil {
		return fmt.Errorf("Failed to connect to GCS: %w", err)
	}

	router := mux.NewRouter()
	router.Path("/upload").
		Methods("POST").
		HandlerFunc(s.ReceiveFile)
	router.Path("/models/{username}/{model_name}/{model_hash}.zip").
		Methods("GET").
		HandlerFunc(s.SendModelPackage)
	fmt.Println("Starting")
	return http.ListenAndServe(fmt.Sprintf(":%d", global.Port), router)
}

func (s *Server) ReceiveFile(w http.ResponseWriter, r *http.Request) {
	response, err := s.ReceiveModel(r)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Pushed CPU model %s, GPU model %s", response.CPUImageTag, response.GPUImageTag)
}

func (s *Server) SendModelPackage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]
	modelName := vars["model_name"]
	modelHash := vars["model_hash"]
	log.Infof("Received download request for %s/%s:%s", username, modelName, modelHash)
	filename := fmt.Sprintf("%s_%s_%s.zip", username, modelName, modelHash)
	modTime := time.Now() // TODO
	gcsPath := fmt.Sprintf("%s/%s/%s.zip", username, modelName, modelHash)
	content, err := s.storage.Download(gcsPath)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	log.Infof("Downloaded %d bytes", len(content))
	http.ServeContent(w, r, filename, modTime, bytes.NewReader(content))
}

type ModelSuccessResponse struct {
	CPUImageTag        string
	GPUImageTag        string
	AIPlatformEndpoint string
}

func (s *Server) ReceiveModel(r *http.Request) (*ModelSuccessResponse, error) {
	queryVars := r.URL.Query()

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
	hash := fmt.Sprintf("%x", hasher.Sum(nil))

	reader, err := zip.NewReader(file, header.Size)
	if err != nil {
		return nil, fmt.Errorf("Failed to read zip file: %w", err)
	}

	parentDir, err := os.MkdirTemp("/tmp", "unzip")
	if err != nil {
		return nil, fmt.Errorf("Failed to make tempdir: %w", err)
	}
	dir := filepath.Join(parentDir, topLevelSourceDir)
	if err := os.Mkdir(dir, 0755); err != nil {
		return nil, fmt.Errorf("Failed to make source dir: %w", err)
	}
	if err := Unzip(reader, dir); err != nil {
		return nil, fmt.Errorf("Failed to unzip: %w", err)
	}

	configRaw, err := os.ReadFile(filepath.Join(dir, "jid.yaml"))
	if err != nil {
		return nil, fmt.Errorf("Failed to read config yaml: %w", err)
	}
	config, err := ConfigFromYAML(configRaw)
	if err != nil {
		return nil, err
	}
	log.Infof("Received model %s", config.Name)

	if err := config.ValidateAndCompleteConfig(); err != nil {
		return nil, err
	}

	gcsPath := config.Name + "/" + hash + ".zip"
	log.Infof("Uploading to gs://%s/%s", global.GCSBucket, gcsPath)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("Failed to rewind file: %w", err)
	}
	if err := s.storage.Upload(file, gcsPath); err != nil {
		return nil, fmt.Errorf("Failed to upload to storage: %w", err)
	}

	cpuImageTag, gpuImageTag, err := s.buildDockerImages(dir, config)
	if err != nil {
		return nil, err
	}

	aiPlatformEndpoint := ""
	if queryVars.Get("deploy") == "true" {
		aiPlatformEndpoint, err = s.ai.Deploy(cpuImageTag, hash)
		if err != nil {
			return nil, err
		}
	}

	// TODO: test model

	log.Info("Inserting into database")
	if err := s.db.InsertModel(config.Name, hash, gcsPath, cpuImageTag, gpuImageTag); err != nil {
		return nil, fmt.Errorf("Failed to insert into database: %w", err)
	}

	return &ModelSuccessResponse{cpuImageTag, gpuImageTag, aiPlatformEndpoint}, nil
}

func (s *Server) buildDockerImages(dir string, config *Config) (cpuImageTag string, gpuImageTag string, err error) {
	for _, arch := range config.Environment.Architectures {
		// TODO: use content-addressable hash
		hash := uniuri.NewLenChars(40, []byte("abcdef0123456789"))
		dockerTag := "us-central1-docker.pkg.dev/replicate/andreas-scratch/" + config.Name + ":" + hash

		switch arch {
		case "gpu":
			gpuImageTag = dockerTag
		case "cpu":
			cpuImageTag = dockerTag
		}

		generator := &DockerfileGenerator{config, arch}
		dockerfileContents, err := generator.Generate()
		if err != nil {
			return "", "", fmt.Errorf("Failed to generate Dockerfile for %s: %w", arch, err)
		}
		dockerfileName := "Dockerfile." + arch
		dockerfilePath := filepath.Join(dir, dockerfileName)
		if err := os.WriteFile(dockerfilePath, []byte(dockerfileContents), 0644); err != nil {
			return "", "", fmt.Errorf("Failed to write Dockerfile for %s", arch)
		}

		if err := s.cloudbuild.Submit(dir, hash, dockerTag, dockerfileName); err != nil {
			return "", "", fmt.Errorf("Failed to build Docker image: %w", err)
		}

	}
	return cpuImageTag, gpuImageTag, nil
}
