package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

func isUpdateEnabled() bool {
	return os.Getenv("COG_NO_UPDATE_CHECK") == ""
}

// DisplayAndCheckForRelease will display an update message if an update is available and will check for a new update in the background
// The result of that check will then be displayed the next time the user runs Cog
// Returns errors which the caller is assumed to ignore so as not to break the client
func DisplayAndCheckForRelease() error {
	if !isUpdateEnabled() {
		return fmt.Errorf("update check disabled")
	}

	s, err := loadState()
	if err != nil {
		return err
	}

	if s.Version != global.Version {
		console.Debugf("Resetting update message because Cog has been upgraded")
		return writeState(&state{Message: "", LastChecked: time.Now(), Version: global.Version})
	}

	if time.Since(s.LastChecked) > time.Hour {
		startCheckingForRelease()
	}
	if s.Message != "" {
		console.Info(s.Message)
		console.Info("")
	}
	return nil
}

func startCheckingForRelease() {
	go func() {
		console.Debugf("Checking for updates...")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		switch r, err := checkForRelease(ctx); {
		case err == nil:
			if r == nil {
				break
			}
			if err := writeState(&state{Message: r.Message, LastChecked: time.Now(), Version: global.Version}); err != nil {
				console.Debugf("Failed to write state: %s", err)
			}

			console.Debugf("result of update check: %v", r.Message)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			break
		default:
			console.Debugf("failed querying for new release: %v", err)
		}
	}()
}

type updateCheckResponse struct {
	Message string `json:"message"`
}

func checkForRelease(ctx context.Context) (*updateCheckResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://update.cog.run/v1/check", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/json")
	q := req.URL.Query()
	q.Add("version", global.Version)
	q.Add("commit", global.Commit)
	q.Add("os", runtime.GOOS)
	q.Add("arch", runtime.GOARCH)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response updateCheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return &response, err
	}

	return &response, nil
}
