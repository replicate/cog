package image

import (
	"bytes"
	"encoding/json"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/util/console"
)

type InputType string

const (
	InputTypeString InputType = "str"
	InputTypeInt    InputType = "int"
	InputTypeFloat  InputType = "float"
	InputTypeBool   InputType = "bool"
	InputTypePath   InputType = "Path"
)

type Input struct {
	Name    string    `json:"name"`
	Type    InputType `json:"type,omitempty"`
	Default *string   `json:"default,omitempty"`
	Min     *string   `json:"min,omitempty"`
	Max     *string   `json:"max,omitempty"`
	Options *[]string `json:"options,omitempty"`
	Help    *string   `json:"help,omitempty"`
}

type TypeSignature struct {
	Inputs []Input `json:"inputs,omitempty"`
}

func GetTypeSignature(imageName string, enableGPU bool) (*TypeSignature, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// FIXME(bfirsh): we could detect this by reading the config label on the image
	gpus := ""
	if enableGPU {
		gpus = "all"
	}

	err := docker.RunWithIO(docker.RunOptions{
		Image: imageName,
		Args: []string{
			"python", "-m", "cog.command.type_signature",
		},
		GPUs: gpus,
	}, nil, &stdout, &stderr)

	if enableGPU && err == docker.ErrMissingDeviceDriver {
		console.Debug(stdout.String())
		console.Debug(stderr.String())
		console.Debug("Missing device driver, re-trying without GPU")
		return GetTypeSignature(imageName, false)
	}

	if err != nil {
		console.Info(stdout.String())
		console.Info(stderr.String())
		return nil, err
	}
	var signature *TypeSignature
	if err := json.Unmarshal(stdout.Bytes(), &signature); err != nil {
		// Exit code was 0, but JSON was not returned.
		// This is verbose, but print so anything that gets printed in Python bubbles up here.
		console.Info(stdout.String())
		console.Info(stderr.String())
		return nil, err
	}
	return signature, nil
}
