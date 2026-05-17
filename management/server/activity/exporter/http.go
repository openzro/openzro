package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/safedial"
)

// HTTPWebhookConfig configures an HTTPWebhook exporter.
type HTTPWebhookConfig struct {
	// URL is the destination endpoint. Required. Must be http or https.
	URL string
	// Headers are sent on every request. Optional; commonly used for
	// authorization tokens. The Content-Type header defaults to
	// application/json; supply a different value here to override
	// (e.g. text/plain when paired with a flat-format Template).
	Headers map[string]string
	// Timeout for a single HTTP attempt. Default 5s.
	Timeout time.Duration
	// MaxAttempts including the initial try. Default 3.
	MaxAttempts int
	// InitialBackoff for retry. Default 200ms; doubles each attempt.
	InitialBackoff time.Duration
	// Template, when non-nil, replaces the default openZro JSON
	// payload with the rendered template output. The body is sent
	// as-is (no JSON wrapping). The operator is responsible for
	// matching Content-Type via Headers when their template emits a
	// non-JSON shape. See template.go.
	Template *PayloadTemplate
	// HTTPClient overrides the default net/http client. Test seam.
	HTTPClient *http.Client
}

// HTTPWebhook POSTs each activity event as JSON to a configured URL.
// Retries on 5xx and transport errors with exponential backoff;
// 4xx responses are treated as poison and not retried (a misconfigured
// receiver should not turn a 401 into a retry storm).
type HTTPWebhook struct {
	url            string
	headers        map[string]string
	maxAttempts    int
	initialBackoff time.Duration
	client         *http.Client
	template       *PayloadTemplate
}

// NewHTTPWebhook constructs an HTTPWebhook from cfg, applying defaults
// for unset fields. Returns an error if URL is empty.
func NewHTTPWebhook(cfg HTTPWebhookConfig) (*HTTPWebhook, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("exporter: HTTPWebhook URL is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	initial := cfg.InitialBackoff
	if initial <= 0 {
		initial = 200 * time.Millisecond
	}
	client := cfg.HTTPClient
	if client == nil {
		client = safedial.Client(timeout)
	}
	return &HTTPWebhook{
		url:            cfg.URL,
		headers:        cfg.Headers,
		maxAttempts:    maxAttempts,
		initialBackoff: initial,
		client:         client,
		template:       cfg.Template,
	}, nil
}

// Name returns the stable identifier used in log lines.
func (h *HTTPWebhook) Name() string { return "http-webhook" }

// httpEventPayload is the on-wire shape. Decoupled from activity.Event
// so we can rename internal fields without breaking receivers, and so
// the timestamp uses RFC3339 instead of the JSON default.
type httpEventPayload struct {
	ID             uint64         `json:"id,omitempty"`
	Timestamp      string         `json:"timestamp"`
	Activity       string         `json:"activity"`
	ActivityCode   uint32         `json:"activity_code"`
	InitiatorID    string         `json:"initiator_id,omitempty"`
	InitiatorName  string         `json:"initiator_name,omitempty"`
	InitiatorEmail string         `json:"initiator_email,omitempty"`
	TargetID       string         `json:"target_id,omitempty"`
	AccountID      string         `json:"account_id"`
	Meta           map[string]any `json:"meta,omitempty"`
}

func toPayload(e *activity.Event) httpEventPayload {
	return httpEventPayload{
		ID:             e.ID,
		Timestamp:      e.Timestamp.UTC().Format(time.RFC3339Nano),
		Activity:       e.Activity.StringCode(),
		ActivityCode:   uint32(e.Activity),
		InitiatorID:    e.InitiatorID,
		InitiatorName:  e.InitiatorName,
		InitiatorEmail: e.InitiatorEmail,
		TargetID:       e.TargetID,
		AccountID:      e.AccountID,
		Meta:           e.Meta,
	}
}

// Export POSTs the event as JSON, retrying on 5xx and transport errors
// up to MaxAttempts. Returns the last error if all attempts fail.
//
// When a Template is configured, the rendered output replaces the
// default JSON payload — the body is sent as-is and the operator is
// responsible for matching Content-Type via Headers if their template
// emits a non-JSON shape.
func (h *HTTPWebhook) Export(ctx context.Context, event *activity.Event) error {
	if event == nil {
		return nil
	}

	var body []byte
	var err error
	if h.template != nil {
		body, err = h.template.Render(event)
		if err != nil {
			return fmt.Errorf("render template: %w", err)
		}
	} else {
		body, err = json.Marshal(toPayload(event))
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
	}

	backoff := h.initialBackoff
	var lastErr error
	for attempt := 1; attempt <= h.maxAttempts; attempt++ {
		retry, err := h.attempt(ctx, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry || attempt == h.maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}

	log.WithContext(ctx).Errorf(
		"activity exporter %s: dropped event after %d attempts: %v",
		h.Name(), h.maxAttempts, lastErr)
	return lastErr
}

// attempt issues one HTTP POST. Returns (retryable, error). nil error
// means success; retryable=false means the failure is permanent (4xx
// or marshal-time issue) and the caller should not loop again.
func (h *HTTPWebhook) attempt(ctx context.Context, body []byte) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return true, fmt.Errorf("transport: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false, nil
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		// 4xx is a configuration problem (auth, bad URL): don't retry.
		return false, fmt.Errorf("non-retryable HTTP %d", resp.StatusCode)
	}
	return true, fmt.Errorf("retryable HTTP %d", resp.StatusCode)
}

// Close is a no-op for HTTPWebhook today — net/http connection pooling
// is reclaimed when the *http.Client is GC'd. Implemented for interface
// completeness so future buffered/batched variants can hook here.
func (h *HTTPWebhook) Close() error { return nil }
