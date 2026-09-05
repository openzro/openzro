package federated

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

// Reuses fakeStore from federated_test.go; these tests only need to
// steer what each side returns.
func hotAnd(hot, arch *fakeStore) *Federated {
	f, err := New(hot, arch, 720*time.Hour)
	if err != nil {
		panic(err)
	}
	return f
}

func at(t time.Time) *store.Event { return &store.Event{ReceivedAt: t, AccountID: "acct-1"} }

// A window that spans both sources, with the archive failing, used to
// return the hot events and a nil error. The caller then rendered a
// short list as the whole answer -- and a gap in flow events reads as
// "nothing happened", which is the most damaging wrong conclusion this
// screen can produce.
//
// Observed in production: a 280s archive timeout returned HTTP 200
// carrying only events newer than the retention boundary, for a window
// that started a week earlier. The only trace was a warning in a log
// nobody reads while looking at a dashboard.
func TestQueryReportsWhenOneSideFailed(t *testing.T) {
	now := time.Now().UTC()
	boundary := now.Add(-720 * time.Hour)

	hotEvent := at(boundary.Add(24 * time.Hour))
	f := hotAnd(
		&fakeStore{name: "hot", queryResult: []*store.Event{hotEvent}},
		&fakeStore{name: "archive", queryErr: context.DeadlineExceeded},
	)

	events, err := f.Query(context.Background(), store.Filter{
		AccountID: "acct-1",
		Since:     boundary.Add(-48 * time.Hour),
		Until:     now,
		Limit:     100,
	})

	require.Len(t, events, 1, "the readable half must still be served")
	require.Equal(t, hotEvent, events[0])

	var incomplete *store.IncompleteError
	require.ErrorAs(t, err, &incomplete,
		"a short answer must say so; nil error here is what made the dashboard lie")
	require.Equal(t, "older", incomplete.Missing,
		"named from the caller's side: what they lost is older events, not 'the archive'")
	require.ErrorIs(t, err, context.DeadlineExceeded, "the cause has to survive for logs")
}

// The mirror case. The hot store failing is rarer but reads the same
// way, and gets the same treatment with the other label.
func TestQueryReportsWhenTheHotSideFailed(t *testing.T) {
	now := time.Now().UTC()
	boundary := now.Add(-720 * time.Hour)
	archEvent := at(boundary.Add(-24 * time.Hour))

	f := hotAnd(
		&fakeStore{name: "hot", queryErr: errors.New("postgres is down")},
		&fakeStore{name: "archive", queryResult: []*store.Event{archEvent}},
	)

	events, err := f.Query(context.Background(), store.Filter{
		AccountID: "acct-1",
		Since:     boundary.Add(-48 * time.Hour),
		Until:     now,
		Limit:     100,
	})

	require.Len(t, events, 1)
	var incomplete *store.IncompleteError
	require.ErrorAs(t, err, &incomplete)
	require.Equal(t, "recent", incomplete.Missing)
}

// Both working must stay exactly as it was: no error, both halves
// merged. If this drifted, every complete answer would start carrying a
// warning and the signal would be worth nothing.
func TestQueryStaysCleanWhenBothSidesWork(t *testing.T) {
	now := time.Now().UTC()
	boundary := now.Add(-720 * time.Hour)

	f := hotAnd(
		&fakeStore{name: "hot", queryResult: []*store.Event{at(boundary.Add(time.Hour))}},
		&fakeStore{name: "archive", queryResult: []*store.Event{at(boundary.Add(-time.Hour))}},
	)

	events, err := f.Query(context.Background(), store.Filter{
		AccountID: "acct-1",
		Since:     boundary.Add(-48 * time.Hour),
		Until:     now,
		Limit:     100,
	})
	require.NoError(t, err, "a complete answer must not be labelled incomplete")
	require.Len(t, events, 2)
}

// Both failing still fails outright. There is nothing to show, so
// pretending otherwise would be the same lie in a different shape.
func TestQueryFailsWhenBothSidesFail(t *testing.T) {
	now := time.Now().UTC()
	boundary := now.Add(-720 * time.Hour)

	f := hotAnd(
		&fakeStore{name: "hot", queryErr: errors.New("postgres is down")},
		&fakeStore{name: "archive", queryErr: context.DeadlineExceeded},
	)

	events, err := f.Query(context.Background(), store.Filter{
		AccountID: "acct-1",
		Since:     boundary.Add(-48 * time.Hour),
		Until:     now,
		Limit:     100,
	})
	require.Error(t, err)
	require.Empty(t, events)

	var incomplete *store.IncompleteError
	require.False(t, errors.As(err, &incomplete),
		"nothing readable is a failure, not an incomplete result")
}
