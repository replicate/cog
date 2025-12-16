package webhook

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/replicate/go/httpclient"

	"github.com/replicate/cog/coglet/internal/logging"
)

// Event represents a webhook event - using string to be compatible with any type
type Event string

const (
	EventStart     Event = "start"
	EventOutput    Event = "output"
	EventLogs      Event = "logs"
	EventCompleted Event = "completed"
)

// Sender handles webhook delivery
type Sender interface {
	Send(url string, payload io.Reader) error
	SendConditional(url string, payload io.Reader, event Event, allowedEvents []Event, lastUpdated *time.Time) error
}

// Build time assertion that DefaultSender implements the Sender interface
var _ Sender = (*DefaultSender)(nil)

// DefaultSender handles webhook delivery
type DefaultSender struct {
	logger *logging.Logger
	client *http.Client
}

// NewSender creates a new webhook sender
func NewSender(logger *logging.Logger) *DefaultSender {
	return &DefaultSender{
		logger: logger.Named("webhook"),
		client: httpclient.ApplyRetryPolicy(http.DefaultClient),
	}
}

// Send delivers a webhook with the given payload
func (s *DefaultSender) Send(url string, payload io.Reader) error {
	req, err := http.NewRequest(http.MethodPost, url, payload)
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req) // TODO[md]: add SSRF protection
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// SendConditional sends webhook if conditions are met
func (s *DefaultSender) SendConditional(url string, payload io.Reader, event Event, allowedEvents []Event, lastUpdated *time.Time) error {
	log := s.logger.Sugar()
	log.Debugw("sending webhook", "url", url, "event", string(event), "allowed_events", allowedEvents, "last_updated", lastUpdated)
	if url == "" {
		return nil
	}

	// Check event filter
	if len(allowedEvents) > 0 && !slices.Contains(allowedEvents, event) {
		log.Tracew("skipping webhook due to event filter", "url", url, "event", string(event), "allowed_events", allowedEvents)
		return nil
	}

	// Rate limiting for logs and output events
	if event == EventLogs || event == EventOutput {
		if lastUpdated != nil && time.Since(*lastUpdated) < 500*time.Millisecond {
			log.Tracew("skipping webhook due to rate limiting", "url", url, "event", string(event), "last_updated", lastUpdated)
			return nil
		}
		if lastUpdated != nil {
			*lastUpdated = time.Now()
		}
	}

	if err := s.Send(url, payload); err != nil {
		log.Errorw("failed to send webhook",
			"url", url,
			"event", string(event),
			"error", err,
		)
		return err
	}

	return nil
}
