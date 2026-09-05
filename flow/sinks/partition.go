package sinks

import (
	"sort"

	"github.com/openzro/openzro/flow/store"
)

// partitionKey is everything an object's path asserts about the events
// inside it: the account and the UTC date. Both sinks lay objects out as
// year=/month=/day=/account=, so two events that disagree on either
// cannot honestly share a file.
type partitionKey struct {
	accountID string
	year      int
	month     int
	day       int
}

func keyFor(ev *store.Event) partitionKey {
	t := ev.ReceivedAt.UTC()
	return partitionKey{
		accountID: ev.AccountID,
		year:      t.Year(),
		month:     int(t.Month()),
		day:       t.Day(),
	}
}

// partitionBatch splits a flush batch into groups that each belong under
// exactly one path.
//
// The sinks buffer into a single queue shared by every account and drain
// it on a timer, so a batch routinely mixes accounts and can straddle
// midnight. Both sinks then derived the whole object's path from
// batch[0] alone, which made the path a claim about one event and a
// guess about the rest:
//
//   - Events for other accounts were filed under batch[0]'s account.
//     The archive query carries no account_id predicate — isolation
//     rests entirely on the path — so those events both leaked into the
//     wrong account's results and vanished from their own.
//   - Events on the other side of midnight were filed under batch[0]'s
//     day. Reading by date could not find them without scanning
//     everything, which is what partition pruning stops doing.
//   - Neither followed from ordering, because nothing orders the queue.
//     batch[0] is the first event dequeued, not the earliest.
//
// Grouping makes the path describe the file's contents, which is what a
// partition column means. The groups are returned in a stable order so
// the uploads, and the tests over them, are deterministic.
func partitionBatch(batch []*store.Event) [][]*store.Event {
	if len(batch) == 0 {
		return nil
	}
	byKey := make(map[partitionKey][]*store.Event)
	order := make([]partitionKey, 0, 4)
	for _, ev := range batch {
		k := keyFor(ev)
		if _, seen := byKey[k]; !seen {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], ev)
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.accountID != b.accountID {
			return a.accountID < b.accountID
		}
		if a.year != b.year {
			return a.year < b.year
		}
		if a.month != b.month {
			return a.month < b.month
		}
		return a.day < b.day
	})
	groups := make([][]*store.Event, 0, len(order))
	for _, k := range order {
		groups = append(groups, byKey[k])
	}
	return groups
}
