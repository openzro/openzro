package exporter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/activity"
)

func sampleEvent() *activity.Event {
	return &activity.Event{
		ID:             42,
		Timestamp:      time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
		Activity:       activity.PeerApproved,
		InitiatorID:    "user-1",
		InitiatorName:  "Alice",
		InitiatorEmail: "alice@example.com",
		TargetID:       "peer-7",
		AccountID:      "acct-1",
		Meta:           map[string]any{"ip": "10.0.0.7"},
	}
}

func TestHTTPWebhook_RequiresURL(t *testing.T) {
	_, err := NewHTTPWebhook(HTTPWebhookConfig{})
	assert.Error(t, err, "missing URL must be rejected")
}

func TestHTTPWebhook_PostsJSONWithCorrectShape(t *testing.T) {
	var got httpEventPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	exp, err := NewHTTPWebhook(HTTPWebhookConfig{
		URL:     srv.URL,
		Headers: map[string]string{"Authorization": "Bearer secret"},
	})
	require.NoError(t, err)

	require.NoError(t, exp.Export(context.Background(), sampleEvent()))

	assert.Equal(t, uint64(42), got.ID)
	assert.Equal(t, "peer.approve", got.Activity)
	assert.Equal(t, uint32(activity.PeerApproved), got.ActivityCode)
	assert.Equal(t, "user-1", got.InitiatorID)
	assert.Equal(t, "alice@example.com", got.InitiatorEmail)
	assert.Equal(t, "peer-7", got.TargetID)
	assert.Equal(t, "acct-1", got.AccountID)
	assert.Equal(t, "10.0.0.7", got.Meta["ip"])
	// Wire format must be RFC3339, not Go's default time format.
	assert.Equal(t, "2026-04-26T12:00:00Z", got.Timestamp)
}

func TestHTTPWebhook_RetriesOn5xxThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exp, err := NewHTTPWebhook(HTTPWebhookConfig{
		URL:            srv.URL,
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
	})
	require.NoError(t, err)

	require.NoError(t, exp.Export(context.Background(), sampleEvent()))
	assert.Equal(t, int32(3), calls.Load(), "should retry until success")
}

func TestHTTPWebhook_DoesNotRetry4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	exp, err := NewHTTPWebhook(HTTPWebhookConfig{
		URL:            srv.URL,
		MaxAttempts:    5,
		InitialBackoff: time.Millisecond,
	})
	require.NoError(t, err)

	err = exp.Export(context.Background(), sampleEvent())
	assert.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(),
		"4xx is a misconfiguration, must not be retried")
}

func TestHTTPWebhook_GivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	exp, err := NewHTTPWebhook(HTTPWebhookConfig{
		URL:            srv.URL,
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
	})
	require.NoError(t, err)

	err = exp.Export(context.Background(), sampleEvent())
	assert.Error(t, err)
	assert.Equal(t, int32(3), calls.Load(),
		"persistent 5xx must drop after MaxAttempts")
}

func TestHTTPWebhook_RespectsContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	exp, err := NewHTTPWebhook(HTTPWebhookConfig{
		URL:            srv.URL,
		MaxAttempts:    10,
		InitialBackoff: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = exp.Export(ctx, sampleEvent())
	// Either the first attempt is canceled by the client, or the
	// backoff sleep returns ctx.Err. Both are correct: the goal is no
	// long-tail of retries after the caller has given up.
	assert.Error(t, err)
}

func TestHTTPWebhook_NilEventIsNoop(t *testing.T) {
	exp, err := NewHTTPWebhook(HTTPWebhookConfig{URL: "http://unused"})
	require.NoError(t, err)
	assert.NoError(t, exp.Export(context.Background(), nil))
}
