package predict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/shell"
)

type Predictor struct {
	runOptions docker.RunOptions

	// Running state
	containerID string
	port        int
}

func NewPredictor(runOptions docker.RunOptions) Predictor {
	return Predictor{runOptions: runOptions}
}

func (p *Predictor) Start() error {
	var err error
	p.port, err = shell.NextFreePort(5000 + rand.Intn(1000))
	if err != nil {
		return err
	}

	containerPort := 5000

	// TODO: put this in the image
	p.runOptions.Env = append(p.runOptions.Env, "LD_LIBRARY_PATH=/usr/local/nvidia/lib64:/usr/local/nvidia/bin")
	p.runOptions.Ports = append(p.runOptions.Ports, docker.Port{HostPort: p.port, ContainerPort: containerPort})

	p.containerID, err = docker.RunDaemon(p.runOptions)
	if err != nil {
		return fmt.Errorf("Failed to start container: %w", err)
	}

	return p.waitForContainerReady()
}

func (p *Predictor) waitForContainerReady() error {
	url := fmt.Sprintf("http://localhost:%d/ping", p.port)

	start := time.Now()
	for {
		now := time.Now()
		if now.Sub(start) > global.StartupTimeout {
			return fmt.Errorf("Timed out")
		}

		time.Sleep(100 * time.Millisecond)

		cont, err := docker.ContainerInspect(p.containerID)
		if err != nil {
			return fmt.Errorf("Failed to get container status: %w", err)
		}
		if cont.State != nil && (cont.State.Status == "exited" || cont.State.Status == "dead") {
			return fmt.Errorf("Container exited unexpectedly")
		}

		resp, err := http.Get(url)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			continue
		}
		return nil
	}
}

func (d *Predictor) Stop() error {
	return docker.Stop(d.containerID)
}

func (d *Predictor) Predict(inputs Inputs) (*Output, error) {
	bodyBuffer := new(bytes.Buffer)

	mwriter := multipart.NewWriter(bodyBuffer)
	for key, val := range inputs {
		if val.File != nil {
			w, err := mwriter.CreateFormFile(key, filepath.Base(*val.File))
			if err != nil {
				return nil, err
			}
			file, err := os.Open(*val.File)
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(w, file); err != nil {
				return nil, err
			}
			if err := file.Close(); err != nil {
				return nil, err
			}
		} else {
			w, err := mwriter.CreateFormField(key)
			if err != nil {
				return nil, err
			}
			if _, err = w.Write([]byte(*val.String)); err != nil {
				return nil, err
			}
		}
	}
	if err := mwriter.Close(); err != nil {
		return nil, fmt.Errorf("Failed to close form mime writer: %w", err)
	}

	url := fmt.Sprintf("http://localhost:%d/predict", d.port)
	req, err := http.NewRequest(http.MethodPost, url, bodyBuffer)
	if err != nil {
		return nil, fmt.Errorf("Failed to create HTTP request to %s: %w", url, err)
	}
	req.Header.Set("Content-Type", mwriter.FormDataContentType())
	req.Close = true

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Failed to POST HTTP request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		body := struct {
			Message string `json:"message"`
		}{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return nil, fmt.Errorf("/predict call return status 400, and the response body failed to decode: %w", err)
		}
		if body.Message == "" {
			return nil, fmt.Errorf("Bad request")
		}
		return nil, fmt.Errorf("Bad request: %s", body.Message)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/predict call returned status %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	mimeType := strings.Split(contentType, ";")[0]

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, resp.Body); err != nil {
		return nil, fmt.Errorf("Failed to read response: %w", err)
	}

	setupTime := -1.0
	runTime := -1.0
	setupTimeStr := resp.Header.Get("X-Setup-Time")
	if setupTimeStr != "" {
		setupTime, err = strconv.ParseFloat(setupTimeStr, 64)
		if err != nil {
			console.Errorf("Failed to parse setup time '%s' as float: %s", setupTimeStr, err)
		}
	}
	runTimeStr := resp.Header.Get("X-Run-Time")
	if runTimeStr != "" {
		runTime, err = strconv.ParseFloat(runTimeStr, 64)
		if err != nil {
			console.Errorf("Failed to parse run time '%s' as float: %s", runTimeStr, err)
		}
	}

	output := &Output{
		Values: map[string]OutputValue{
			// TODO(andreas): support multiple outputs?
			"output": {
				Buffer:   buf,
				MimeType: mimeType,
			},
		},
		SetupTime: setupTime,
		RunTime:   runTime,
	}
	return output, nil
}

func (d *Predictor) Help() (*HelpResponse, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%d/help", d.port), nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to create GET request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Failed to GET /help: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/help call returned status %d", resp.StatusCode)
	}

	help := new(HelpResponse)
	if err := json.NewDecoder(resp.Body).Decode(help); err != nil {
		return nil, fmt.Errorf("Failed to parse /help body: %w", err)
	}

	return help, nil
}
