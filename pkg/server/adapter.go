package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/replicate/cog/pkg/files"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/shell"
)

type Adapter struct {
	Name       string `yaml:"name"`
	Script     string `yaml:"script"`
	scriptPath string
}

func loadAdapter(adapterDir string) (*Adapter, error) {
	adapterDir, err := filepath.Abs(adapterDir)
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(adapterDir, "cog-adapter.yaml")
	exists, err := files.FileExists(configPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("Adapter directory is missing cog-adapter.yaml: %s", adapterDir)
	}
	configYaml, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to read %s: %w", configPath, err)
	}

	adapter := new(Adapter)
	if err := yaml.Unmarshal(configYaml, adapter); err != nil {
		return nil, fmt.Errorf("Failed to parse %s: %w", configPath, err)
	}
	if adapter.Script == "" {
		return nil, fmt.Errorf("%s is missing a 'script:' field", configPath)
	}
	if adapter.Name == "" {
		return nil, fmt.Errorf("%s is missing a 'name:' field", configPath)
	}
	adapter.scriptPath = filepath.Join(adapterDir, adapter.Script)
	exists, err = files.FileExists(adapter.scriptPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("Adapter script is missing in %s", adapter.scriptPath)
	}

	return adapter, nil
}

func (s *Server) buildAdapterTargets(user string, name string, mod *model.Model, dir string, logWriter logger.Logger) error {
	for _, adapterDir := range s.adapters {
		artifact, err := s.buildAdapterTarget(user, name, adapterDir, mod, dir, logWriter)
		if err != nil {
			return err
		}
		mod.Artifacts = append(mod.Artifacts, artifact)
	}
	return nil
}

func (s *Server) buildAdapterTarget(user string, name string, adapter *Adapter, mod *model.Model, dir string, logWriter logger.Logger) (*model.Artifact, error) {
	module, class := mod.Config.ModelParts()
	cpuImage, ok := mod.ArtifactFor(model.TargetDockerCPU)
	if !ok {
		return nil, fmt.Errorf("No CPU image built")
	}
	hasDockerRegistry := "false"
	if s.dockerRegistry != "" {
		hasDockerRegistry = "true"
	}
	env := []string{
		"COG_MODEL_PYTHON_MODULE=" + module,
		"COG_MODEL_CLASS=" + class,
		"COG_CPU_IMAGE=" + cpuImage.URI,
		"COG_DOCKER_REGISTRY=" + s.dockerRegistry,
		"COG_HAS_DOCKER_REGISTRY=" + hasDockerRegistry,
		"COG_REPO_USER=" + user,
		"COG_REPO_NAME=" + name,
		"COG_MODEL_ID=" + mod.ID,
	}
	logWriter.WriteLogLine("Building adapter target: %s", adapter.Name)

	cmd := exec.Command(adapter.scriptPath)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env[k] = v
	}
	stderrDone, err := shell.PipeTo(cmd.StderrPipe, func(args ...interface{}) {
		logWriter.WriteLogLine(args[0].(string))
	})
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.Output()
	<-stderrDone
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	if len(lines) != 1 {
		return nil, fmt.Errorf("Adapter returned %d lines on stdout, expected 1", len(lines))
	}
	return &model.Artifact{
		Target: adapter.Name,
		URI:    lines[0],
	}, nil
}
