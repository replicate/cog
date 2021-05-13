package server

import (
	"context"
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
	"github.com/segmentio/ksuid"

	"github.com/replicate/cog/pkg/database"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/zip"
)

func (s *Server) ReceiveFile(w http.ResponseWriter, r *http.Request) {
	user, name, _ := getModelVars(r)

	console.Debug("Received build request")
	streamLogger := logger.NewStreamLogger(r.Context(), w)
	mod, err := s.ReceiveVersion(r, streamLogger, user, name)
	if err != nil {
		streamLogger.WriteError(err)
		console.Error(err.Error())
		return
	}
	streamLogger.WriteVersion(mod)
}

func (s *Server) ReceiveVersion(r *http.Request, logWriter logger.Logger, user string, name string) (*model.Version, error) {
	dir, err := s.UnzipInputToTempDir(r, user, name)
	if err != nil {
		return nil, err
	}

	id, err := ComputeID(dir)
	if err != nil {
		return nil, err
	}
	logWriter.Debugf("Received version %s", id)

	config, err := s.ReadConfig(dir)
	if err != nil {
		return nil, err
	}
	version := &model.Version{
		Config:   config,
		Created:  time.Now(),
		ID:       id,
		BuildIDs: make(map[string]string),
	}
	zipOutputPath, err := s.ZipToTempPath(dir)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(zipOutputPath)
	if err != nil {
		return nil, err
	}
	if err := s.store.Upload(user, name, id, file); err != nil {
		return nil, fmt.Errorf("Failed to upload to storage: %w", err)
	}
	if err := s.runHooks(s.postUploadHooks, user, name, id, version, nil, logWriter); err != nil {
		return nil, err
	}
	for _, arch := range config.Environment.Architectures {
		isPrimary := arch == "cpu" || (!config.HasCPU() && arch == "gpu")
		buildID := ksuid.New().String()
		version.BuildIDs[arch] = buildID
		arch := arch
		go func() {
			defer os.RemoveAll(dir)
			s.buildImage(buildID, dir, user, name, id, version, arch, isPrimary)
		}()
	}
	logWriter.WriteStatus("Inserting into database")
	if err := s.db.InsertVersion(user, name, id, version); err != nil {
		return nil, fmt.Errorf("Failed to insert into database: %w", err)
	}
	return version, nil
}

func (s *Server) buildImage(buildID, dir, user, name, id string, version *model.Version, arch string, isPrimary bool) {
	logWriter := database.NewBuildLogger(user, name, buildID, s.db)
	handleError := func(err error) {
		logWriter.WriteError(err)
		_ = s.db.InsertImage(user, name, id, arch, &model.Image{Arch: arch, BuildFailed: true, Created: time.Now()})
	}

	defer func() {
		if err := s.db.FinalizeBuildLog(user, name, buildID); err != nil {
			console.Errorf("Failed to finalize build log: %v", err)
		}
	}()

	// TODO(andreas): make it possible to cancel the build
	result, err := s.buildQueue.Build(context.Background(), dir, name, id, arch, version.Config, logWriter)
	if err != nil {
		handleError(err)
		return
	}

	if err := s.runHooks(s.postBuildHooks, user, name, id, nil, result.image, logWriter); err != nil {
		handleError(err)
		return
	}

	// only upload the zip and run post-build hooks on primary arch
	if isPrimary {
		if err := s.saveExamples(result, dir, version.Config); err != nil {
			handleError(err)
			return
		}
		zipOutputPath, err := s.ZipToTempPath(dir)
		if err != nil {
			handleError(err)
			return
		}
		file, err := os.Open(zipOutputPath)
		if err != nil {
			handleError(err)
			return
		}
		defer file.Close()

		logWriter.Debug("Re-saving version with updated examples")
		if err := s.store.Upload(user, name, id, file); err != nil {
			handleError(err)
			return
		}
		if err := s.db.InsertVersion(user, name, id, version); err != nil {
			handleError(err)
			return
		}

		if err := s.runHooks(s.postBuildPrimaryHooks, user, name, id, version, result.image, logWriter); err != nil {
			handleError(err)
			return
		}
	}

	if err := s.db.InsertImage(user, name, id, arch, result.image); err != nil {
		handleError(err)
		return
	}
	logWriter.Infof("Successfully built image %s", result.image.URI)
}

func (s *Server) saveExamples(result *BuildResult, dir string, config *model.Config) error {
	// get the examples from the first result
	testResult := result.testResult

	if len(testResult.NewExampleOutputs) > 0 {
		config.Examples = testResult.Examples

		for outputPath, outputBytes := range testResult.NewExampleOutputs {
			exampleDir := filepath.Dir(outputPath)
			if err := os.MkdirAll(filepath.Join(dir, exampleDir), 0755); err != nil {
				return fmt.Errorf("Failed to make output dir: %w", err)
			}
			if err := os.WriteFile(filepath.Join(dir, outputPath), outputBytes, 0644); err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
		}
	}
	return nil
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
	user, name, _ := getModelVars(r)
	console.Debugf("Received cache-hashes request for %s/%s", user, name)

	zipCache, err := zip.NewModelCache(user, name)
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
	zipCache, err := zip.NewModelCache(user, name)
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

func (s *Server) SendBuildLogs(w http.ResponseWriter, r *http.Request) {
	user, name, buildID := getModelVars(r)

	follow := r.URL.Query().Get("follow") == "true"
	logChan, err := s.db.GetBuildLogs(user, name, buildID, follow)
	if err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
	}
	encoder := json.NewEncoder(w)
	for entry := range logChan {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		if err := encoder.Encode(entry); err != nil {
			console.Error(err.Error())
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		} else {
			console.Warn("HTTP response writer can not be flushed")
		}
	}
}
