package sinks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

func ev(account string, t time.Time) *store.Event {
	return &store.Event{AccountID: account, EventID: []byte{1, 2, 3, 4}, ReceivedAt: t}
}

// The invariant the object layout has always claimed and never held:
// every event in a file agrees with the account and date in that file's
// path. Stated over both sinks, because they build the key
// independently and have to stay in step.
func TestPartitionBatchGroupsAgreeWithTheirObjectKey(t *testing.T) {
	s3 := &S3{cfg: S3Config{Bucket: "b"}, format: formatParquet}
	gcs := &GCS{cfg: GCSConfig{Bucket: "b"}, format: formatParquet}

	batch := []*store.Event{
		ev("acct-A", time.Date(2026, 6, 10, 23, 59, 0, 0, time.UTC)),
		ev("acct-B", time.Date(2026, 6, 11, 0, 1, 0, 0, time.UTC)),
		ev("acct-A", time.Date(2026, 6, 11, 0, 2, 0, 0, time.UTC)),
		ev("acct-A", time.Date(2026, 6, 10, 22, 0, 0, 0, time.UTC)),
	}

	groups := partitionBatch(batch)
	require.Len(t, groups, 3,
		"two accounts across two days is three partitions, whatever order they arrived in")

	for _, group := range groups {
		for _, key := range []string{s3.objectKey(group[0]), gcs.objectKey(group[0])} {
			for _, e := range group {
				u := e.ReceivedAt.UTC()
				require.Contains(t, key, "account="+e.AccountID,
					"an event was filed under another account's partition")
				require.Contains(t, key,
					u.Format("year=2006/month=01/day=02"),
					"an event was filed under another day's partition")
			}
		}
	}
}

// The account half, on its own, because it is not a pruning concern.
// The reader now compares account_id as well as the path, so a misfiled
// event is no longer served to the wrong account — but the path is what
// decides which objects are opened, so it is still lost to the right
// one. Only the writer can put it somewhere both halves agree on.
func TestPartitionBatchNeverMixesAccountsInOneObject(t *testing.T) {
	batch := []*store.Event{
		ev("acct-A", time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)),
		ev("acct-B", time.Date(2026, 6, 10, 12, 0, 1, 0, time.UTC)),
	}
	groups := partitionBatch(batch)
	require.Len(t, groups, 2, "one object may not carry two accounts")
	for _, group := range groups {
		for _, e := range group {
			require.Equal(t, group[0].AccountID, e.AccountID)
		}
	}
}

// Nothing orders the queue, so batch[0] is the first event dequeued and
// not the earliest. The old key derived the whole path from it, which
// meant an out-of-order batch filed earlier events under a later day.
func TestPartitionBatchDoesNotDependOnArrivalOrder(t *testing.T) {
	early := time.Date(2026, 6, 10, 1, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 12, 1, 0, 0, 0, time.UTC)

	forward := partitionBatch([]*store.Event{ev("acct-A", early), ev("acct-A", late)})
	backward := partitionBatch([]*store.Event{ev("acct-A", late), ev("acct-A", early)})

	require.Len(t, forward, 2)
	require.Len(t, backward, 2)
	for i := range forward {
		require.Equal(t, keyFor(forward[i][0]), keyFor(backward[i][0]),
			"the same events must land in the same partitions regardless of dequeue order")
	}
}

// A batch that already belongs to one partition must stay one object.
// Splitting it would multiply small files across the archive for no
// gain, and small files are what MaxEventsPerFile exists to avoid.
func TestPartitionBatchKeepsAHomogeneousBatchWhole(t *testing.T) {
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	batch := make([]*store.Event, 0, 50)
	for i := range 50 {
		batch = append(batch, ev("acct-A", day.Add(time.Duration(i)*time.Minute)))
	}
	groups := partitionBatch(batch)
	require.Len(t, groups, 1)
	require.Len(t, groups[0], 50)
}

func TestPartitionBatchEmpty(t *testing.T) {
	require.Nil(t, partitionBatch(nil))
	require.Nil(t, partitionBatch([]*store.Event{}))
}
