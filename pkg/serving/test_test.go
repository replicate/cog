package serving

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
)

func TestTestModel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "myfile.txt"), []byte("filecontents"), 0644))

	logWriter := new(logger.ConsoleLogger)

	run := func(example *Example) *Result {
		setupTime := float64(1.0)
		if setupTimeValue, ok := example.Values["setup_time"]; ok {
			var err error
			setupTime, err = strconv.ParseFloat(*setupTimeValue.String, 64)
			if err != nil {
				panic(err)
			}
		}
		fileContents, err := os.ReadFile(*example.Values["file"].File)
		if err != nil {
			panic(err)
		}
		ret := "hello " + *example.Values["foo"].String + " " + string(fileContents)
		return &Result{
			Values: map[string]ResultValue{
				"output": {
					Buffer:   bytes.NewBuffer([]byte(ret)),
					MimeType: "text/plain",
				},
			},
			SetupTime:       setupTime,
			RunTime:         0.01,
			UsedMemoryBytes: 5000,
			UsedCPUSecs:     0.1,
		}
	}

	helpArgs := map[string]*model.RunArgument{
		"foo": {
			Type: model.ArgumentTypeString,
		},
		"file": {
			Type: model.ArgumentTypePath,
		},
		"setup_time": {
			Type:    model.ArgumentTypeFloat,
			Default: sp("1.0"),
		},
	}

	servingPlatform := NewMockServingPlatform(100*time.Millisecond, run, helpArgs)
	examples := []*model.Example{
		{
			Input: map[string]string{
				"foo":        "bar",
				"file":       "@myfile.txt",
				"setup_time": "2",
			},
			Output: "hello bar filecontents",
		},
		{
			Input: map[string]string{
				"foo":  "qux",
				"file": "@myfile.txt",
			},
			Output: "",
		},
	}

	result, err := TestModel(context.Background(), servingPlatform, "", examples, tmpDir, false, logWriter)
	require.NoError(t, err)

	expectedExamples := []*model.Example{
		{
			Input: map[string]string{
				"foo":        "bar",
				"file":       "@myfile.txt",
				"setup_time": "2",
			},
			Output: "hello bar filecontents",
		},
		{
			Input: map[string]string{
				"foo":  "qux",
				"file": "@myfile.txt",
			},
			Output: "@cog-example-output/output.01.txt",
		},
	}
	expectedNewExampleOutputs := map[string][]byte{
		"cog-example-output/output.01.txt": []byte("hello qux filecontents"),
	}
	require.Equal(t, expectedExamples, result.Examples)
	require.Equal(t, expectedNewExampleOutputs, result.NewExampleOutputs)
	require.Equal(t, helpArgs, result.RunArgs)
	require.Less(t, float64(0.1), result.Stats.BootTime)
	require.Greater(t, float64(1.0), result.Stats.BootTime)
	require.Equal(t, float64(1.5), result.Stats.SetupTime)
	require.Equal(t, float64(0.01), result.Stats.RunTime)
	require.Equal(t, uint64(5000), result.Stats.MemoryUsage)
	require.Equal(t, float64(0.1), result.Stats.CPUUsage)
}

func TestValidateServingExampleInput(t *testing.T) {
	args := map[string]*model.RunArgument{
		"foo": {
			Type: model.ArgumentTypeString,
		},
		"bar": {
			Type:    model.ArgumentTypeInt,
			Default: sp("1"),
		},
	}

	require.NoError(t, validateServingExampleInput(args, map[string]string{
		"foo": "myval",
		"bar": "10",
	}))
	require.NoError(t, validateServingExampleInput(args, map[string]string{
		"foo": "myval",
	}))
	require.Error(t, validateServingExampleInput(args, map[string]string{}))
	require.Error(t, validateServingExampleInput(args, map[string]string{
		"bar": "myval",
	}))
	require.Error(t, validateServingExampleInput(args, map[string]string{
		"qux": "myval",
	}))
	require.Error(t, validateServingExampleInput(args, map[string]string{
		"foo": "myval",
		"bar": "10",
		"qux": "somethingelse",
	}))
}

func TestExtensionByType(t *testing.T) {
	require.Equal(t, ".txt", extensionByType("text/plain"))
	require.Equal(t, ".jpg", extensionByType("image/jpeg"))
	require.Equal(t, ".png", extensionByType("image/png"))
	require.Equal(t, ".json", extensionByType("application/json"))
	require.Equal(t, "", extensionByType("asdfasdf"))
}

func TestOutputBytesFromExample(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	err = os.WriteFile(filepath.Join(tmpDir, "input.txt"), []byte("hello"), 0644)
	require.NoError(t, err)

	outputBytes, outputIsFile, err := outputBytesFromExample("foo", tmpDir)
	require.NoError(t, err)
	require.Equal(t, "foo", string(outputBytes))
	require.False(t, outputIsFile)

	outputBytes, outputIsFile, err = outputBytesFromExample("@input.txt", tmpDir)
	require.NoError(t, err)
	require.Equal(t, "hello", string(outputBytes))
	require.True(t, outputIsFile)

	outputBytes, outputIsFile, err = outputBytesFromExample("", tmpDir)
	require.NoError(t, err)
	require.Nil(t, outputBytes)
	require.False(t, outputIsFile)

	_, _, err = outputBytesFromExample("@doesnotexist.txt", tmpDir)
	require.Error(t, err)
}

func TestSetAggregateStats(t *testing.T) {
	modelStats := new(model.Stats)
	err := setAggregateStats(modelStats, []float64{1.0, 6.0, 2.0}, []float64{3.0, 2.0, 1.0}, []float64{5000, 1000, 2000}, []float64{300, 400, 600})
	require.NoError(t, err)
	require.Equal(t, float64(3.0), modelStats.SetupTime)
	require.Equal(t, float64(2.0), modelStats.RunTime)
	require.Equal(t, uint64(5000), modelStats.MemoryUsage)
	require.Equal(t, float64(600), modelStats.CPUUsage)
}

func TestUpdateExampleOutput(t *testing.T) {
	example := new(model.Example)
	newExampleOutputs := make(map[string][]byte)
	updateExampleOutput(example, newExampleOutputs, []byte("hello"), "text/plain", 2)
	require.Equal(t, example.Output, "@cog-example-output/output.02.txt")
	require.Equal(t, newExampleOutputs, map[string][]byte{
		"cog-example-output/output.02.txt": []byte("hello"),
	})
}

func TestVerifyCorrectOutput(t *testing.T) {
	err := verifyCorrectOutput([]byte("hello"), []byte("hello"), false)
	require.NoError(t, err)
	err = verifyCorrectOutput([]byte("  hello "), []byte(" hello  \n"), false)
	require.NoError(t, err)
	err = verifyCorrectOutput([]byte("hello"), []byte("hi"), false)
	require.Error(t, err)
	err = verifyCorrectOutput([]byte("hello"), []byte("hello"), true)
	require.NoError(t, err)
	err = verifyCorrectOutput([]byte("hello "), []byte("hello"), true)
	require.Error(t, err)
	err = verifyCorrectOutput([]byte("hello"), []byte("hi"), true)
	require.Error(t, err)
}

func sp(s string) *string {
	return &s
}
