package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
)

// FIXME(bfirsh): why is this a different format to logger.LogWriter?
type LogEntry struct {
	Level logger.Level `json:"level"`
	Line  string       `json:"line"`
}

func (c *Client) GetBuildLogs(mod *model.Model, buildID string, follow bool) (chan *LogEntry, error) {
	url := newURL(mod, "v1/models/%s/%s/builds/%s/logs", mod.User, mod.Name, buildID)
	req, err := c.newRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if follow {
		q := req.URL.Query()
		q.Add("follow", "true")
		req.URL.RawQuery = q.Encode()
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Build logs endpoint returned error %d", resp.StatusCode)
	}
	logChan := make(chan *LogEntry)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			entry := new(LogEntry)
			if err := json.Unmarshal(scanner.Bytes(), entry); err != nil {
				console.Errorf("Failed to parse log entry: %v", err)
				return
			}
			logChan <- entry
		}
		close(logChan)
	}()

	return logChan, nil
}
