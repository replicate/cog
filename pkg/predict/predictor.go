package predict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/shell"
)

type status string

type Request struct {
	// TODO: could this be Inputs?
	Input map[string]string `json:"input"`
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

	// Running state
	containerID string
	port        int
}

func NewPredictor(runOptions docker.RunOptions) Predictor {
	if global.Debug {
		runOptions.Env = append(runOptions.Env, "COG_LOG_LEVEL=debug")
	} else {
		runOptions.Env = append(runOptions.Env, "COG_LOG_LEVEL=warning")
	}
	return Predictor{runOptions: runOptions}
}

func (p *Predictor) Start(logsWriter io.Writer) error {
	var err error
	p.port, err = shell.NextFreePort(5000 + rand.Intn(1000))
	if err != nil {
		return err
	}

	containerPort := 5000

	p.runOptions.Ports = append(p.runOptions.Ports, docker.Port{HostPort: p.port, ContainerPort: containerPort})

	p.containerID, err = docker.RunDaemon(p.runOptions)
	if err != nil {
		return fmt.Errorf("Failed to start container: %w", err)
	}
	go func() {
		if err := docker.ContainerLogsFollow(p.containerID, logsWriter); err != nil {
			console.Warnf("Error getting container logs: %s", err)
		}
	}()

	return p.waitForContainerReady()
}

func (p *Predictor) waitForContainerReady() error {
	url := fmt.Sprintf("http://localhost:%d/", p.port)

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

	url := fmt.Sprintf("http://localhost:%d/predictions", p.port)
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
			return nil, fmt.Errorf("/predictions call returned status 422, and the response body failed to decode: %w", err)
		}

		return nil, buildInputValidationErrorMessage(errorResponse)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/predictions call returned status %d", resp.StatusCode)
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

func buildInputValidationErrorMessage(errorResponse *ValidationErrorResponse) error {
	errorMessages := []string{}

	for _, validationError := range errorResponse.Detail {
		if len(validationError.Location) != 3 || validationError.Location[0] != "body" || validationError.Location[1] != "input" {
			responseBody, _ := json.MarshalIndent(errorResponse, "", "\t")
			return fmt.Errorf("/predictions call returned status 422, and there was an unexpected message in response:\n\n%s", responseBody)
		}

		errorMessages = append(errorMessages, fmt.Sprintf("- %s: %s", validationError.Location[2], validationError.Message))
	}

	return fmt.Errorf(
		`The inputs you passed to cog predict could not be validated:

%s

You can provide an input with -i. For example:

    cog predict -i blur=3.5

If your input is a local file, you need to prefix the path with @ to tell Cog to read the file contents. For example:

    cog predict -i path=@image.jpg`,
		strings.Join(errorMessages, "\n"),
	)
}
