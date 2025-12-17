package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/runner"
)

var errProcedureFailedToStart = errors.New("procedure failed to start")

// runProcedure runs a procedure and returns the prediction id and HTTP status code
func runProcedure(t *testing.T, runtimeServer *httptest.Server, predictionRequest runner.PredictionRequest) (string, int) {
	t.Helper()

	// we only run procedures with webhooks/receivers for testing purposes. It eliminates complexity
	// when we need to wait for the prediction to start avoiding random time.Sleep() calls.
	assert.NotEmpty(t, predictionRequest.Webhook, "procedures must be run with webhook set")

	req := httpPredictionRequest(t, runtimeServer, predictionRequest)
	req.URL.Path = "/procedures"
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	var predictionResponse runner.PredictionResponse
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if resp.StatusCode != http.StatusAccepted {
		return "", resp.StatusCode
	}
	err = json.Unmarshal(body, &predictionResponse)
	require.NoError(t, err)

	assert.NotEmpty(t, predictionResponse.ID)
	defer resp.Body.Close()
	require.NoError(t, err)

	return predictionResponse.ID, resp.StatusCode
}

type procedureRun struct {
	URL            string
	Input          map[string]any
	ExpectedOutput string
	ExpectedLogs   string
	Token          string
	Started        chan struct{}
}

func runAndValidateProcedure(t *testing.T, runtimeServer *httptest.Server, run procedureRun) error {
	t.Helper()
	receiverServer := testHarnessReceiverServer(t)
	procPrediction := runner.PredictionRequest{
		Context: map[string]any{
			"procedure_source_url": run.URL,
			"replicate_api_token":  run.Token,
		},
		Input:   run.Input,
		Webhook: receiverServer.URL + "/webhook",
	}
	_, statusCode := runProcedure(t, runtimeServer, procPrediction)
	if statusCode != http.StatusAccepted {
		return fmt.Errorf("%w: %d", errProcedureFailedToStart, statusCode)
	}
	assert.Equal(t, http.StatusAccepted, statusCode)
	timeout := time.After(10 * time.Second)
	for webhook := range receiverServer.webhookReceiverChan {
		select {
		case <-timeout:
			t.Fatalf("timeout waiting for prediction to complete")
		default:
			switch webhook.Response.Status {
			case runner.PredictionStarting, runner.PredictionProcessing:
				safeCloseChannel(run.Started)
			case runner.PredictionSucceeded:
				ValidateTerminalResponse(t, &webhook.Response)
				assert.Equal(t, run.ExpectedOutput, webhook.Response.Output)
				assert.Equal(t, runner.PredictionSucceeded, webhook.Response.Status)
				assert.Contains(t, webhook.Response.Logs, run.ExpectedLogs)
				return nil
			case runner.PredictionFailed:
				t.Fatalf("unexpected prediction failure: %v", webhook.Response.Error)
				return fmt.Errorf("unexpected prediction failure: %v", webhook.Response.Error)
			default:
				t.Logf("unexpected webhook event: %v", webhook.Response.Status)
				// continue the loop.
			}
		}
	}
	return nil
}

func TestProcedureSlots(t *testing.T) {
	// FIXME: refactor this test. It is doing far too much, but is being left mostly
	// as-is functionality wise for the test-harness refactoring. Some of the phases
	// could be unit tests if respun with direct access to the handler.
	t.Parallel()
	if *legacyCog {
		t.Skipf("procedure endpoint has diverged from legacy Cog")
	}

	runtimeServer, handler, _ := setupCogRuntimeServer(t, cogRuntimeServerConfig{
		procedureMode:    true,
		explicitShutdown: true,
		uploadURL:        "",
		maxRunners:       2,
	})
	hc := waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)
	assert.Equal(t, 2, hc.Concurrency.Max)
	assert.Equal(t, 0, hc.Concurrency.Current)

	wg := sync.WaitGroup{}

	// occupy slot 1
	fooURL := fmt.Sprintf("file://%s/python/tests/procedures/%s", basePath, "foo")
	fooPredictionStarted := make(chan struct{})
	wg.Go(func() {
		err := runAndValidateProcedure(t, runtimeServer, procedureRun{
			URL:            fooURL,
			Input:          map[string]any{"i": 3, "s": "foostr"},
			ExpectedOutput: "i=3, s=foostr, token=footok",
			ExpectedLogs:   "predicting foo",
			Token:          "footok",
			Started:        fooPredictionStarted,
		})
		require.NoError(t, err)
	})

	// Wait for the prediction to start. We can safely block here because we'll timeout in the wg.Go
	// within a short time if the prediction doesn't start.
	<-fooPredictionStarted

	hc = healthCheck(t, runtimeServer)
	assert.Equal(t, runner.StatusReady.String(), hc.Status)
	assert.Equal(t, 2, hc.Concurrency.Max)
	assert.Equal(t, 1, hc.Concurrency.Current)

	activeRunners := handler.ActiveRunners()
	assert.NotNil(t, activeRunners[0])
	assert.Nil(t, activeRunners[1])
	assert.Contains(t, activeRunners[0].String(), fooURL)
	assert.False(t, activeRunners[0].Idle())

	// occupy slot 2
	barURL := fmt.Sprintf(procedureFilePathURITemplate, basePath, "bar")
	barPredictionStarted := make(chan struct{})
	wg.Go(func() {
		err := runAndValidateProcedure(t, runtimeServer, procedureRun{
			URL:            barURL,
			Input:          map[string]any{"i": 2, "s": "barstr"},
			ExpectedOutput: "i=2, s=barstr, token=bartok",
			ExpectedLogs:   "predicting bar",
			Token:          "bartok",
			Started:        barPredictionStarted,
		})
		require.NoError(t, err)
	})

	// Wait for the prediction to start. We can safely block here because we'll timeout in the wg.Go
	// within a short time if the prediction doesn't start.
	<-barPredictionStarted

	// Ensure both slots are occupied with active runners
	hc = healthCheck(t, runtimeServer)
	assert.Equal(t, runner.StatusBusy.String(), hc.Status)
	assert.Equal(t, 2, hc.Concurrency.Max)
	assert.Equal(t, 2, hc.Concurrency.Current)

	activeRunners = handler.ActiveRunners()
	assert.Len(t, activeRunners, 2)
	assert.Contains(t, activeRunners[0].String(), fooURL)
	assert.Contains(t, activeRunners[1].String(), barURL)
	assert.False(t, activeRunners[0].Idle())
	assert.False(t, activeRunners[1].Idle())

	bazURL := fmt.Sprintf(procedureFilePathURITemplate, basePath, "baz")
	// Eviction is not allowed if all slots are occupied
	bazProcedureRun := procedureRun{
		URL:            bazURL,
		Input:          map[string]any{"i": 1, "s": "bazstr"},
		ExpectedOutput: "i=1, s=bazstr, token=baztok",
		ExpectedLogs:   "predicting baz",
		Token:          "baztok",
		Started:        make(chan struct{}),
	}
	err := runAndValidateProcedure(t, runtimeServer, bazProcedureRun)
	require.ErrorIs(t, err, errProcedureFailedToStart)

	// Wait for the predictions to finish
	wg.Wait()

	// Re-attempt the new procedure, now evicting a slot is possible
	err = runAndValidateProcedure(t, runtimeServer, bazProcedureRun)
	require.NoError(t, err)

	activeRunners = handler.ActiveRunners()
	assert.NotNil(t, activeRunners[0])
	assert.NotNil(t, activeRunners[1])
	// find the baz runner and ensure it is in the active runner list
	foundBazRunner := false
	for _, r := range activeRunners {
		// strip off the `NN:` prefix from the runner string/named, e.g. 00:file:///path/to/procedure -> file:///path/to/procedure
		parts := strings.SplitN(r.String(), ":", 2)
		require.Len(t, parts, 2)
		if parts[1] == bazURL {
			foundBazRunner = true
			break
		}
	}
	assert.True(t, foundBazRunner)
}

func TestProcedureSlotBadProcedure(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skipf("procedure endpoint has diverged from legacy Cog")
	}

	runtimeServer, handler, _ := setupCogRuntimeServer(t, cogRuntimeServerConfig{
		procedureMode:    true,
		explicitShutdown: true,
		uploadURL:        "",
		maxRunners:       2,
	})
	hc := waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)
	assert.Equal(t, 2, hc.Concurrency.Max)
	assert.Equal(t, 0, hc.Concurrency.Current)

	// a bad procedure should fail to start and auto vacate the slot
	badProcURL := fmt.Sprintf(procedureFilePathURITemplate, basePath, "bad")
	receiverServer := testHarnessReceiverServer(t)
	procPrediction := runner.PredictionRequest{
		Context: map[string]any{
			"procedure_source_url": badProcURL,
			"replicate_api_token":  "badtok",
		},
		Input:   map[string]any{"i": 3, "s": "foostr"},
		Webhook: receiverServer.URL + "/webhook",
	}
	_, statusCode := runProcedure(t, runtimeServer, procPrediction)
	assert.Equal(t, http.StatusAccepted, statusCode)
	var webhook webhookData
	select {
	case webhook = <-receiverServer.webhookReceiverChan:
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for prediction to complete")
	}
	assert.Equal(t, runner.PredictionFailed, webhook.Response.Status)
	ValidateTerminalResponse(t, &webhook.Response)
	assert.Contains(t, webhook.Response.Logs, "unsupported Cog type")
	assert.Equal(t, "setup failed", webhook.Response.Error)

	// FIXME: this sleep is a small wait to ensure the runner list is cleanedup
	// verified with -count=100 that this is consistent and reliable
	time.Sleep(1 * time.Millisecond)

	hc = healthCheck(t, runtimeServer)
	assert.Equal(t, runner.StatusReady.String(), hc.Status)
	assert.Equal(t, 2, hc.Concurrency.Max)
	assert.Equal(t, 0, hc.Concurrency.Current)

	// The bad procedure should have vacated the slot, so we should have zero runners, all slots == nil
	activeRunners := handler.ActiveRunners()
	assert.Nil(t, activeRunners[0])
	assert.Nil(t, activeRunners[1])
}

func TestProcedureAsyncConcurrency(t *testing.T) {
	// NOTE: concurrent is limited to the maximum number of runners regardless of per-runner concurrency. This
	// is largely due to how everything in replicate is architected as we are not able to "Schedule" a particular
	// prediction to a particular instance that has capacity and already has the runner active. This means that
	// even though we have 4 slots, we can only run 4 total predictions at a time. When we improve routing we
	// can improve this behavior. For now, this note serves as a reminder so that future contributions understand
	// why we max out concurrency at 4 even though technically the slot*per-runner-async-concurrency is 8.
	t.Parallel()
	if *legacyCog {
		t.Skipf("procedure endpoint has diverged from legacy Cog")
	}

	runtimeServer, handler, _ := setupCogRuntimeServer(t, cogRuntimeServerConfig{
		procedureMode:    true,
		explicitShutdown: true,
		uploadURL:        "",
		maxRunners:       4,
	})
	hc := waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)
	assert.Equal(t, 4, hc.Concurrency.Max)
	assert.Equal(t, 0, hc.Concurrency.Current)

	fooURL := fmt.Sprintf(procedureFilePathURITemplate, basePath, "foo")

	wg := sync.WaitGroup{}
	startChan1 := make(chan struct{})
	wg.Go(func() {
		err := runAndValidateProcedure(t, runtimeServer, procedureRun{
			URL:            fooURL,
			Input:          map[string]any{"i": 3, "s": "foostr"},
			ExpectedOutput: "i=3, s=foostr, token=footok",
			ExpectedLogs:   "predicting foo",
			Token:          "footok",
			Started:        startChan1,
		})
		assert.NoError(t, err)
		<-startChan1
	})

	startChan2 := make(chan struct{})
	wg.Go(func() {
		err := runAndValidateProcedure(t, runtimeServer, procedureRun{
			URL:            fooURL,
			Input:          map[string]any{"i": 3, "s": "foostr"},
			ExpectedOutput: "i=3, s=foostr, token=footok",
			ExpectedLogs:   "predicting foo",
			Token:          "footok",
			Started:        startChan2,
		})
		assert.NoError(t, err)
		<-startChan2
	})

	// wait for both predictions to start
	<-startChan1
	<-startChan2

	// foo has max concurrency of 2, so we should have 2 running predictions
	activeRunners := handler.ActiveRunners()
	// The prediction slot cannot be nil, must be occupied  the equality assert will panic
	require.NotNil(t, activeRunners[0])
	assert.Contains(t, activeRunners[0].String(), fooURL)
	assert.Nil(t, activeRunners[1])
	assert.Nil(t, activeRunners[2])
	assert.Nil(t, activeRunners[3])
	assert.False(t, activeRunners[0].Idle())

	hc = healthCheck(t, runtimeServer)
	assert.Equal(t, runner.StatusReady.String(), hc.Status)
	assert.Equal(t, 4, hc.Concurrency.Max)
	assert.Equal(t, 2, hc.Concurrency.Current)

	startChan3 := make(chan struct{})
	wg.Go(func() {
		err := runAndValidateProcedure(t, runtimeServer, procedureRun{
			URL:            fooURL,
			Input:          map[string]any{"i": 3, "s": "foostr"},
			ExpectedOutput: "i=3, s=foostr, token=footok",
			ExpectedLogs:   "predicting foo",
			Token:          "footok",
			Started:        startChan3,
		})
		assert.NoError(t, err)
	})

	startChan4 := make(chan struct{})
	wg.Go(func() {
		err := runAndValidateProcedure(t, runtimeServer, procedureRun{
			URL:            fooURL,
			Input:          map[string]any{"i": 3, "s": "foostr"},
			ExpectedOutput: "i=3, s=foostr, token=footok",
			ExpectedLogs:   "predicting foo",
			Token:          "footok",
			Started:        startChan4,
		})
		assert.NoError(t, err)
	})

	<-startChan3
	<-startChan4

	activeRunners = handler.ActiveRunners()

	// The prediction slot cannot be nil, must be occupied or the equality assert will panic
	require.NotNil(t, activeRunners[0])
	require.NotNil(t, activeRunners[1])
	assert.Contains(t, activeRunners[0].String(), fooURL)
	assert.Contains(t, activeRunners[1].String(), fooURL)

	// Ensure the two runners have different names
	runner0Name := strings.Split(activeRunners[0].String(), ":")[0]
	runner1Name := strings.Split(activeRunners[1].String(), ":")[0]
	assert.NotEqual(t, runner0Name, runner1Name, "Both runners should have different names")

	assert.Nil(t, activeRunners[2])
	assert.Nil(t, activeRunners[3])
	assert.False(t, activeRunners[0].Idle())
	assert.False(t, activeRunners[1].Idle())

	hc = healthCheck(t, runtimeServer)
	assert.Equal(t, runner.StatusBusy.String(), hc.Status)
	assert.Equal(t, 4, hc.Concurrency.Max)
	assert.Equal(t, 4, hc.Concurrency.Current)

	// Wait for all predictions to finish
	wg.Wait()

	// FIXME: Sleep to allow all predictions to clear out of the runners
	time.Sleep(100 * time.Millisecond)

	hc = healthCheck(t, runtimeServer)
	assert.Equal(t, runner.StatusReady.String(), hc.Status)
	assert.Equal(t, 4, hc.Concurrency.Max)
	assert.Equal(t, 0, hc.Concurrency.Current)
	activeRunners = handler.ActiveRunners()
	require.NotNil(t, activeRunners[0])
	require.NotNil(t, activeRunners[1])
	require.Nil(t, activeRunners[2])
	require.Nil(t, activeRunners[3])
	assert.True(t, activeRunners[0].Idle())
	assert.True(t, activeRunners[1].Idle())
}

func TestProcedureNonAsyncConcurrency(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skipf("procedure endpoint has diverged from legacy Cog")
	}

	runtimeServer, handler, _ := setupCogRuntimeServer(t, cogRuntimeServerConfig{
		procedureMode:    true,
		explicitShutdown: true,
		uploadURL:        "",
		maxRunners:       4,
	})
	hc := waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)
	assert.Equal(t, 4, hc.Concurrency.Max)
	assert.Equal(t, 0, hc.Concurrency.Current)

	barURL := fmt.Sprintf(procedureFilePathURITemplate, basePath, "bar")

	wg := sync.WaitGroup{}
	startChan1 := make(chan struct{})
	wg.Go(func() {
		err := runAndValidateProcedure(t, runtimeServer, procedureRun{
			URL:            barURL,
			Input:          map[string]any{"i": 3, "s": "barstr"},
			ExpectedOutput: "i=3, s=barstr, token=bartok",
			ExpectedLogs:   "predicting bar",
			Token:          "bartok",
			Started:        startChan1,
		})
		assert.NoError(t, err)
		<-startChan1
	})

	startChan2 := make(chan struct{})
	wg.Go(func() {
		err := runAndValidateProcedure(t, runtimeServer, procedureRun{
			URL:            barURL,
			Input:          map[string]any{"i": 3, "s": "barstr"},
			ExpectedOutput: "i=3, s=barstr, token=bartok",
			ExpectedLogs:   "predicting bar",
			Token:          "bartok",
			Started:        startChan2,
		})
		assert.NoError(t, err)
		<-startChan2
	})

	// wait for both predictions to start
	<-startChan1
	<-startChan2

	// foo has max concurrency of 2, so we should have 2 running predictions
	activeRunners := handler.ActiveRunners()
	// The prediction slot cannot be nil, must be occupied  the equality assert will panic
	require.NotNil(t, activeRunners[0])
	require.NotNil(t, activeRunners[1])
	// Check that both runners are showing the correct procedure URL format
	// With semaphore-based system, we use random runner IDs instead of fixed slots
	assert.Contains(t, activeRunners[0].String(), barURL)
	assert.Contains(t, activeRunners[1].String(), barURL)

	// Ensure the two runners have different names
	runner0Name := strings.Split(activeRunners[0].String(), ":")[0]
	runner1Name := strings.Split(activeRunners[1].String(), ":")[0]
	assert.NotEqual(t, runner0Name, runner1Name, "Both runners should have different names")

	// We should only have 2 active runners for this test, but we have a capacity of 4
	assert.Len(t, activeRunners, 4)
	assert.False(t, activeRunners[0].Idle())
	assert.False(t, activeRunners[1].Idle())
	assert.Nil(t, activeRunners[2])
	assert.Nil(t, activeRunners[3])

	hc = healthCheck(t, runtimeServer)
	assert.Equal(t, runner.StatusReady.String(), hc.Status)
	assert.Equal(t, 4, hc.Concurrency.Max)
	assert.Equal(t, 2, hc.Concurrency.Current)

	startChan3 := make(chan struct{})
	wg.Go(func() {
		err := runAndValidateProcedure(t, runtimeServer, procedureRun{
			URL:            barURL,
			Input:          map[string]any{"i": 3, "s": "barstr"},
			ExpectedOutput: "i=3, s=barstr, token=bartok",
			ExpectedLogs:   "predicting bar",
			Token:          "bartok",
			Started:        startChan3,
		})
		assert.NoError(t, err)
	})

	startChan4 := make(chan struct{})
	wg.Go(func() {
		err := runAndValidateProcedure(t, runtimeServer, procedureRun{
			URL:            barURL,
			Input:          map[string]any{"i": 3, "s": "barstr"},
			ExpectedOutput: "i=3, s=barstr, token=bartok",
			ExpectedLogs:   "predicting bar",
			Token:          "bartok",
			Started:        startChan4,
		})
		assert.NoError(t, err)
	})

	<-startChan3
	<-startChan4

	activeRunners = handler.ActiveRunners()

	// The prediction slot cannot be nil, must be occupied or the equality assert will panic
	require.NotNil(t, activeRunners[0])
	require.NotNil(t, activeRunners[1])
	require.NotNil(t, activeRunners[2])
	require.NotNil(t, activeRunners[3])
	// Check that all 4 runners show the correct procedure URL format
	assert.Contains(t, activeRunners[0].String(), barURL)
	assert.Contains(t, activeRunners[1].String(), barURL)
	assert.Contains(t, activeRunners[2].String(), barURL)
	assert.Contains(t, activeRunners[3].String(), barURL)

	// Ensure all 4 runners have different names
	runnerNames := make(map[string]bool)
	for i, r := range activeRunners[:4] {
		runnerName := strings.Split(r.String(), ":")[0]
		assert.False(t, runnerNames[runnerName], "Runner %d name %s should be unique", i, runnerName)
		runnerNames[runnerName] = true
	}

	assert.False(t, activeRunners[0].Idle())
	assert.False(t, activeRunners[1].Idle())
	assert.False(t, activeRunners[2].Idle())
	assert.False(t, activeRunners[3].Idle())

	hc = healthCheck(t, runtimeServer)
	assert.Equal(t, runner.StatusBusy.String(), hc.Status)
	assert.Equal(t, 4, hc.Concurrency.Max)
	assert.Equal(t, 4, hc.Concurrency.Current)

	// Wait for all predictions to finish
	wg.Wait()

	hc = healthCheck(t, runtimeServer)
	assert.Equal(t, runner.StatusReady.String(), hc.Status)
	assert.Equal(t, 4, hc.Concurrency.Max)
	assert.Equal(t, 0, hc.Concurrency.Current)
	activeRunners = handler.ActiveRunners()
	require.NotNil(t, activeRunners[0])
	require.NotNil(t, activeRunners[1])
	require.NotNil(t, activeRunners[2])
	require.NotNil(t, activeRunners[3])
	assert.True(t, activeRunners[0].Idle())
	assert.True(t, activeRunners[1].Idle())
	assert.True(t, activeRunners[2].Idle())
	assert.True(t, activeRunners[3].Idle())
}
