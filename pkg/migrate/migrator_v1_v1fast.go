package migrate

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"archive/zip"

	"gopkg.in/yaml.v2"

	"github.com/replicate/cog/pkg/coglog"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/requirements"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
)

const CogRequirementsFile = "cog_requirements.txt"
const MigrateV1V1FastPythonFile = "migrate_v1_v1fast.py"

type PredictorType int

const (
	PredictorTypePredict PredictorType = iota
	PredictorTypeTrain
)

var IgnoredRunCommands = map[string]bool{
	"curl -o /usr/local/bin/pget -L \\\"https://github.com/replicate/pget/releases/latest/download/pget_$(uname -s)_$(uname -m)\\\" && chmod +x /usr/local/bin/pget": true,
	"curl -o /usr/local/bin/pget -L \"https://github.com/replicate/pget/releases/latest/download/pget_$(uname -s)_$(uname -m)\" && chmod +x /usr/local/bin/pget":     true,
	"curl -o /usr/local/bin/pget -L \\\"https://github.com/replicate/pget/releases/latest/download/pget_$(uname -s)_$(uname -m)\\\"":                                 true,
	"curl -o /usr/local/bin/pget -L \"https://github.com/replicate/pget/releases/latest/download/pget_$(uname -s)_$(uname -m)\"":                                     true,
	"chmod +x /usr/local/bin/pget": true,
}

type MigratorV1ToV1Fast struct {
	Interactive bool
	logCtx      *coglog.MigrateLogContext
}

func NewMigratorV1ToV1Fast(interactive bool, logCtx *coglog.MigrateLogContext) *MigratorV1ToV1Fast {
	return &MigratorV1ToV1Fast{
		Interactive: interactive,
		logCtx:      logCtx,
	}
}

func (g *MigratorV1ToV1Fast) Migrate(ctx context.Context, configFilename string) error {
	cfg, projectDir, err := config.GetRawConfig(configFilename)
	if err != nil {
		return err
	}
	err = g.checkPythonRequirements(cfg, projectDir)
	if err != nil {
		return err
	}
	err = g.checkRunCommands(cfg)
	if err != nil {
		return err
	}
	err = g.checkPythonCode(ctx, cfg, projectDir)
	if err != nil {
		return err
	}
	err = g.flushConfig(cfg, projectDir, configFilename)
	return err
}

func (g *MigratorV1ToV1Fast) checkPythonRequirements(cfg *config.Config, dir string) error {
	if cfg.Build == nil {
		g.logCtx.PythonPackageStatus = coglog.StatusPassed
		return nil
	}
	if len(cfg.Build.PythonPackages) == 0 {
		g.logCtx.PythonPackageStatus = coglog.StatusPassed
		return nil
	}
	console.Info("You have python_packages in your configuration, this is now deprecated and replaced with python_requirements.")
	accept := true
	if g.Interactive {
		interactive := &console.InteractiveBool{
			Prompt:         "Would you like to move your python_packages to a requirements.txt?",
			Default:        true,
			NonDefaultFlag: "--y",
		}
		iAccept, err := interactive.Read()
		if err != nil {
			return err
		}
		accept = iAccept
	}
	if !accept {
		g.logCtx.PythonPackageStatus = coglog.StatusDeclined
		console.Error("Skipping python_packages to python_requirements migration, this will cause issues on builds for fast boots.")
		return nil
	}
	requirementsFile := filepath.Join(dir, requirements.RequirementsFile)
	exists, err := files.Exists(requirementsFile)
	if err != nil {
		return err
	}
	if exists {
		// If requirements.txt exists, we will write to an alternative requirements file to prevent overloading.
		requirementsFile = filepath.Join(dir, CogRequirementsFile)
	}
	requirementsContent := strings.Join(cfg.Build.PythonPackages, "\n")
	console.Infof("Writing python_packages to %s.", requirementsFile)
	file, err := os.Create(requirementsFile)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(requirementsContent)
	if err != nil {
		return err
	}
	cfg.Build.PythonPackages = []string{}
	cfg.Build.PythonRequirements = filepath.Base(requirementsFile)
	g.logCtx.PythonPackageStatus = coglog.StatusAccepted
	return nil
}

func (g *MigratorV1ToV1Fast) checkRunCommands(cfg *config.Config) error {
	if cfg.Build == nil {
		g.logCtx.RunStatus = coglog.StatusPassed
		return nil
	}
	if len(cfg.Build.Run) == 0 {
		g.logCtx.RunStatus = coglog.StatusPassed
		return nil
	}
	// Filter run commands we can safely remove
	safelyRemove := true
	for _, runCommand := range cfg.Build.Run {
		_, ok := IgnoredRunCommands[runCommand.Command]
		if !ok {
			console.Warnf("Failed to safely remove \"%s\"", runCommand.Command)
			safelyRemove = false
			break
		}
	}
	if safelyRemove {
		console.Info("Safely removing run commands.")
		cfg.Build.Run = []config.RunItem{}
		g.logCtx.RunStatus = coglog.StatusAccepted
		return nil
	}
	accept := true
	if g.Interactive {
		interactive := &console.InteractiveBool{
			Prompt:         "You have run commands we do not recognize in your configuration, do you want us to remove them?",
			Default:        true,
			NonDefaultFlag: "--y",
		}
		iAccept, err := interactive.Read()
		if err != nil {
			return err
		}
		accept = iAccept
	}
	if !accept {
		g.logCtx.RunStatus = coglog.StatusDeclined
		console.Error("Skipping removing run commands, this will cause issues on builds for fast boots.")
	} else {
		console.Info("Removing run commands.")
		cfg.Build.Run = []config.RunItem{}
		g.logCtx.RunStatus = coglog.StatusAccepted
	}
	return nil
}

func (g *MigratorV1ToV1Fast) checkPythonCode(ctx context.Context, cfg *config.Config, dir string) error {
	err := g.checkPredictor(ctx, cfg.Predict, dir, PredictorTypePredict)
	if err != nil {
		return err
	}
	err = g.checkPredictor(ctx, cfg.Train, dir, PredictorTypeTrain)
	if err != nil {
		return err
	}
	return nil
}

func (g *MigratorV1ToV1Fast) flushConfig(cfg *config.Config, dir string, configFilename string) error {
	if cfg.Build == nil {
		cfg.Build = config.DefaultConfig().Build
	}
	cfg.Build.Fast = true
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	configStr := string(data)

	configFilepath := filepath.Join(dir, configFilename)
	file, err := os.Open(configFilepath)
	if err != nil {
		return err
	}
	content, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		return err
	}
	if configStr == string(content) {
		return nil
	}

	console.Infof("New cog.yaml:\n%s\n", configStr)

	accept := true
	if g.Interactive {
		interactive := &console.InteractiveBool{
			Prompt:         "Do you want to apply the above config changes?",
			Default:        true,
			NonDefaultFlag: "--y",
		}
		iAccept, err := interactive.Read()
		if err != nil {
			return err
		}
		accept = iAccept
	}
	if !accept {
		console.Error("Skipping config changes, this may cause issues on builds for fast boots.")
		return nil
	}

	file, err = os.Create(configFilepath)
	if err != nil {
		return err
	}
	defer file.Close()
	console.Infof("Writing config changes to %s.", configFilepath)

	mergedCfgData, err := util.OverwriteYAML(data, content)
	if err != nil {
		return err
	}

	_, err = file.WriteString(string(mergedCfgData))
	if err != nil {
		return util.WrapError(err, "Failed to write config changes")
	}

	return nil
}

func (g *MigratorV1ToV1Fast) checkPredictor(ctx context.Context, predictor string, dir string, predictorType PredictorType) error {
	if predictor == "" {
		return nil
	}
	zippedBytes, _, err := dockerfile.ReadWheelFile()
	if err != nil {
		return err
	}
	reader := bytes.NewReader(zippedBytes)
	zipReader, err := zip.NewReader(reader, int64(len(zippedBytes)))
	if err != nil {
		return err
	}
	for _, file := range zipReader.File {
		if filepath.Base(file.Name) != MigrateV1V1FastPythonFile {
			continue
		}
		return g.runPythonScript(ctx, file, predictor, dir, predictorType)
	}

	return errors.New("Could not find " + MigrateV1V1FastPythonFile)
}

func (g *MigratorV1ToV1Fast) runPythonScript(ctx context.Context, file *zip.File, predictor string, dir string, predictorType PredictorType) error {
	splitPredictor := strings.Split(predictor, ":")
	pythonFilename := splitPredictor[0]
	pythonPredictor := splitPredictor[1]

	fileReader, err := file.Open()
	if err != nil {
		return err
	}
	defer fileReader.Close()
	extractedData, err := io.ReadAll(fileReader)
	if err != nil {
		return err
	}
	pythonCode := string(extractedData)
	cmd := exec.CommandContext(ctx, "python3", "-c", pythonCode, pythonFilename, pythonPredictor)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}
	newContent := out.String()

	pythonFilepath := filepath.Join(dir, pythonFilename)
	pythonFile, err := os.Open(pythonFilepath)
	if err != nil {
		return err
	}
	content, err := io.ReadAll(pythonFile)
	pythonFile.Close()
	if err != nil {
		return err
	}

	if newContent == string(content) {
		if predictorType == PredictorTypePredict {
			g.logCtx.PythonPredictStatus = coglog.StatusPassed
		} else {
			g.logCtx.PythonTrainStatus = coglog.StatusPassed
		}
		return nil
	}

	if strings.TrimSpace(newContent) == "" {
		if predictorType == PredictorTypePredict {
			g.logCtx.PythonPredictStatus = coglog.StatusPassed
		} else {
			g.logCtx.PythonTrainStatus = coglog.StatusPassed
		}
		return nil
	}
	accept := true
	if g.Interactive {
		interactive := &console.InteractiveBool{
			Prompt:         "Do you want to apply the above code changes?",
			Default:        true,
			NonDefaultFlag: "--y",
		}
		iAccept, err := interactive.Read()
		if err != nil {
			return err
		}
		accept = iAccept
	}
	if !accept {
		if predictorType == PredictorTypePredict {
			g.logCtx.PythonPredictStatus = coglog.StatusDeclined
		} else {
			g.logCtx.PythonTrainStatus = coglog.StatusDeclined
		}
		console.Error("Skipping code changes, this will cause issues on builds for fast boots.")
		return nil
	}
	if predictorType == PredictorTypePredict {
		g.logCtx.PythonPredictStatus = coglog.StatusAccepted
	} else {
		g.logCtx.PythonTrainStatus = coglog.StatusAccepted
	}

	pythonFile, err = os.Create(pythonFilepath)
	if err != nil {
		return util.WrapError(err, "Could not open python predictor file")
	}
	defer pythonFile.Close()
	console.Infof("Writing code changes to %s.", pythonFilepath)
	_, err = pythonFile.WriteString(newContent)
	if err != nil {
		return util.WrapError(err, "Failed to write to python predictor file")
	}
	return nil
}
