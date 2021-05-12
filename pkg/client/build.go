package client

import (
	"bufio"
	"encoding/json"
	"net/http"

	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
)

type LogEntry struct {
	Level logger.Level `json:"level"`
	Line  string       `json:"line"`
}

func (c *Client) GetBuildLogs(repo *model.Repo, buildID string, follow bool) (chan *LogEntry, error) {
	url := newURL(repo, "v1/repos/%s/%s/builds/%s/logs", repo.User, repo.Name, buildID)
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
