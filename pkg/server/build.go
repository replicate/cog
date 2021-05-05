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
	"time"

	"github.com/mholt/archiver/v3"

	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/zip"
)

func (s *Server) ReceiveFile(w http.ResponseWriter, r *http.Request) {
	user, name, _ := getRepoVars(r)

	console.Info("Received build request")
	streamLogger := logger.NewStreamLogger(r.Context(), w)
	mod, err := s.ReceiveModel(r, streamLogger, user, name)
	if err != nil {
		streamLogger.WriteError(err)
		console.Error(err.Error())
		return
	}
	streamLogger.WriteModel(mod)
}

func (s *Server) ReceiveModel(r *http.Request, logWriter logger.Logger, user string, name string) (*model.Model, error) {
	dir, err := s.UnzipInputToTempDir(r, user, name)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	logWriter.Infof("Received model")

	id, err := ComputeID(dir)
	if err != nil {
		return nil, err
	}
	config, err := s.ReadConfig(dir)
	if err != nil {
		return nil, err
	}
	logWriter.Infof("Received model %s", id)
	result, err := s.buildQueue.Build(r.Context(), dir, name, id, config, logWriter)
	if err != nil {
		return nil, err
	}
	mod := &model.Model{
		Artifacts:    result.Artifacts,
		Config:       config,
		Created:      time.Now(),
		ID:           id,
		RunArguments: result.RunArgs,
		Stats:        result.ModelStats,
	}

	zipOutputPath, err := s.ZipToTempPath(dir)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(zipOutputPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if err := s.store.Upload(user, name, id, file); err != nil {
		return nil, fmt.Errorf("Failed to upload to storage: %w", err)
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

func (s *Server) ReadConfig(dir string) (*model.Config, error) {
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
	return config, nil
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

func (s *Server) UnzipInputToTempDir(r *http.Request, user string, name string) (string, error) {
	// keep 128MB in memory while parsing the input
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		return "", fmt.Errorf("Failed to parse request: %w", err)
	}
	inputFile, header, err := r.FormFile("file")
	if err != nil {
		return "", fmt.Errorf("Failed to read input file: %w", err)
	}
	defer inputFile.Close()
	defer r.Body.Close()

	parentDir, err := os.MkdirTemp("/tmp", "unzip")
	if err != nil {
		return "", fmt.Errorf("Failed to make tempdir: %w", err)
	}
	dir := filepath.Join(parentDir, topLevelSourceDir)
	if err := os.Mkdir(dir, 0755); err != nil {
		return "", fmt.Errorf("Failed to make source dir: %w", err)
	}
	zipCache, err := zip.NewRepoCache(user, name)
	if err != nil {
		return "", err
	}
	z := zip.NewCachingZip()
	if err := z.ReaderUnarchive(inputFile, header.Size, dir, zipCache); err != nil {
		return "", fmt.Errorf("Failed to unzip: %w", err)
	}
	return dir, nil
}

func (s *Server) ZipToTempPath(dir string) (string, error) {
	z := &archiver.Zip{ImplicitTopLevelFolder: false}
	zipTempDir, err := os.MkdirTemp("/tmp", "zip")
	if err != nil {
		return "", fmt.Errorf("Failed to make tempdir: %w", err)
	}
	zipOutputPath := filepath.Join(zipTempDir, "out.zip")
	if err := z.Archive([]string{dir + "/"}, zipOutputPath); err != nil {
		return "", fmt.Errorf("Failed to zip directory: %w", err)
	}
	return zipOutputPath, nil
}

func ComputeID(dir string) (string, error) {
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
