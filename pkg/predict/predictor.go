package predict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

type status string

type HealthcheckResponse struct {
	Status string `json:"status"`
}

type Request struct {
	// TODO: could this be Inputs?
	Input map[string]interface{} `json:"input"`
}

type Response struct {
	Status status       `json:"status"`
	Output *interface{} `json:"output"`
	Error  string       `json:"error"`
}

type ValidationErrorResponse struct {
	Detail []struct {
		Location []string `json:"loc"`
		Message  string   `json:"msg"`
		Type     string   `json:"type"`
	} `json:"detail"`
}

type Predictor struct {
	runOptions docker.RunOptions
	isTrain    bool

	// Running state
	containerID string
	port        int
}

func NewPredictor(runOptions docker.RunOptions, isTrain bool, fastFlag bool, dockerCommand command.Command, projectDir string) (*Predictor, error) {
	if fastFlag {
		console.Info("Fast predictor enabled.")
	}

	if global.Debug {
		runOptions.Env = append(runOptions.Env, "COG_LOG_LEVEL=debug")
	} else {
		runOptions.Env = append(runOptions.Env, "COG_LOG_LEVEL=warning")
	}

	runOptions, err := docker.FillInWeightsManifestVolumes(dockerCommand, runOptions, projectDir)
	if err != nil {
		return nil, err
	}

	return &Predictor{runOptions: runOptions, isTrain: isTrain}, nil
}

func (p *Predictor) Start(logsWriter io.Writer, timeout time.Duration) error {
	var err error
	containerPort := 5000

	p.runOptions.Ports = append(p.runOptions.Ports, docker.Port{HostPort: 0, ContainerPort: containerPort})

	p.containerID, err = docker.RunDaemon(p.runOptions, logsWriter)
	if err != nil {
		return fmt.Errorf("Failed to start container: %w", err)
	}

	p.port, err = docker.GetPort(p.containerID, containerPort)
	if err != nil {
		return fmt.Errorf("Failed to determine container port: %w", err)
	}

	go func() {
		if err := docker.ContainerLogsFollow(p.containerID, logsWriter); err != nil {
			// if user hits ctrl-c we expect an error signal
			if !strings.Contains(err.Error(), "signal: interrupt") {
				console.Warnf("Error getting container logs: %s", err)
			}
		}
	}()

	return p.waitForContainerReady(timeout)
}

func (p *Predictor) waitForContainerReady(timeout time.Duration) error {
	url := fmt.Sprintf("http://localhost:%d/health-check", p.port)

	start := time.Now()
	for {
		now := time.Now()
		if now.Sub(start) > timeout {
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

		resp, err := http.Get(url) //#nosec G107
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			continue
		}
		healthcheck := &HealthcheckResponse{}
		if err := json.NewDecoder(resp.Body).Decode(healthcheck); err != nil {
			return fmt.Errorf("Container healthcheck returned invalid response: %w", err)
		}
		// These status values are defined in python/cog/server/http.py
		switch healthcheck.Status {
		case "STARTING":
			continue
		case "SETUP_FAILED":
			return fmt.Errorf("Model setup failed")
		case "READY":
			return nil
		default:
			return fmt.Errorf("Container healthcheck returned unexpected status: %s", healthcheck.Status)
		}
	}
}

func (p *Predictor) Stop() error {
	return docker.Stop(p.containerID)
}

func (p *Predictor) Predict(inputs Inputs) (*Response, error) {
	inputMap, err := inputs.toMap()
	if err != nil {
		return nil, err
	}
	request := Request{Input: inputMap}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	url := p.url()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("Failed to create HTTP request to %s: %w", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Close = true

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Failed to POST HTTP request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		errorResponse := &ValidationErrorResponse{}
		if err := json.NewDecoder(resp.Body).Decode(errorResponse); err != nil {
			return nil, fmt.Errorf("/%s call returned status 422, and the response body failed to decode: %w", p.endpoint(), err)
		}

		return nil, p.buildInputValidationErrorMessage(errorResponse)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/%s call returned status %d", p.endpoint(), resp.StatusCode)
	}

	prediction := &Response{}
	if err = json.NewDecoder(resp.Body).Decode(prediction); err != nil {
		return nil, fmt.Errorf("Failed to decode prediction response: %w", err)
	}
	return prediction, nil
}

func (p *Predictor) GetSchema() (*openapi3.T, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/openapi.json", p.port))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to get OpenAPI schema: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return openapi3.NewLoader().LoadFromData(body)
}

func (p *Predictor) endpoint() string {
	if p.isTrain {
		return "trainings"
	}
	return "predictions"
}

func (p *Predictor) url() string {
	return fmt.Sprintf("http://localhost:%d/%s", p.port, p.endpoint())
}

func (p *Predictor) buildInputValidationErrorMessage(errorResponse *ValidationErrorResponse) error {
	errorMessages := []string{}

	for _, validationError := range errorResponse.Detail {
		if len(validationError.Location) != 3 || validationError.Location[0] != "body" || validationError.Location[1] != "input" {
			responseBody, _ := json.MarshalIndent(errorResponse, "", "\t")
			return fmt.Errorf("/%s call returned status 422, and there was an unexpected message in response:\n\n%s", p.endpoint(), responseBody)
		}

		errorMessages = append(errorMessages, fmt.Sprintf("- %s: %s", validationError.Location[2], validationError.Message))
	}

	command := "predict"
	if p.isTrain {
		command = "train"
	}

	return fmt.Errorf(
		`The inputs you passed to cog %[1]s could not be validated:

%[2]s

You can provide an input with -i. For example:

    cog %[1]s -i blur=3.5

If your input is a local file, you need to prefix the path with @ to tell Cog to read the file contents. For example:

    cog %[1]s -i path=@image.jpg`,
		command,
		strings.Join(errorMessages, "\n"),
	)
}
