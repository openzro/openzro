package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/openzro/openzro/management/server/activity"
)

func generateAndStoreEvents(t *testing.T, manager *DefaultAccountManager, typ activity.Activity, initiatorID, targetID,
	accountID string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		_, err := manager.eventStore.Save(context.Background(), &activity.Event{
			Timestamp:   time.Now().UTC(),
			Activity:    typ,
			InitiatorID: initiatorID,
			TargetID:    targetID,
			AccountID:   accountID,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

type ctxProbeKey struct{}

// ctxProbeStore is an activity.Store whose Save blocks until release is
// closed and then reports the state of the context it was handed. It lets
// a test cancel the caller's context while StoreEvent's goroutine is
// still in flight.
type ctxProbeStore struct {
	activity.InMemoryEventStore
	release <-chan struct{}
	result  chan<- ctxProbeResult
}

type ctxProbeResult struct {
	err   error
	value any
}

func (s *ctxProbeStore) Save(ctx context.Context, event *activity.Event) (*activity.Event, error) {
	<-s.release
	s.result <- ctxProbeResult{err: ctx.Err(), value: ctx.Value(ctxProbeKey{})}
	return event, nil
}

// TestStoreEvent_SurvivesCallerCancel guards against StoreEvent's
// background goroutine reusing the request context: once the request
// returns, that context is canceled and persistence plus every
// activity exporter fail with "context canceled".
func TestStoreEvent_SurvivesCallerCancel(t *testing.T) {
	t.Setenv("OZ_EVENT_ACTIVITY_LOG_ENABLED", "true")

	release := make(chan struct{})
	result := make(chan ctxProbeResult, 1)
	am := &DefaultAccountManager{
		eventStore: &ctxProbeStore{release: release, result: result},
	}

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxProbeKey{}, "request-scoped"))
	am.StoreEvent(ctx, "initiator", "target", "account", activity.PeerAddedByUser, nil)
	cancel()
	close(release)

	select {
	case got := <-result:
		assert.NoError(t, got.err, "event context must outlive the caller's request")
		assert.Equal(t, "request-scoped", got.value, "request-scoped values must be preserved")
	case <-time.After(5 * time.Second):
		t.Fatal("StoreEvent never reached the event store")
	}
}

func TestDefaultAccountManager_GetEvents(t *testing.T) {
	manager, err := createManager(t)
	if err != nil {
		return
	}

	accountID := "accountID"

	t.Run("get empty events list", func(t *testing.T) {
		events, err := manager.GetEvents(context.Background(), accountID, userID)
		if err != nil {
			return
		}
		assert.Len(t, events, 0)
		_ = manager.eventStore.Close(context.Background()) //nolint
	})

	t.Run("get events", func(t *testing.T) {
		generateAndStoreEvents(t, manager, activity.PeerAddedByUser, userID, "peer", accountID, 10)
		events, err := manager.GetEvents(context.Background(), accountID, userID)
		if err != nil {
			return
		}

		assert.Len(t, events, 10)
		_ = manager.eventStore.Close(context.Background()) //nolint
	})

	t.Run("get events without duplicates", func(t *testing.T) {
		generateAndStoreEvents(t, manager, activity.UserJoined, userID, "", accountID, 10)
		events, err := manager.GetEvents(context.Background(), accountID, userID)
		if err != nil {
			return
		}
		assert.Len(t, events, 1)
		_ = manager.eventStore.Close(context.Background()) //nolint
	})
}
