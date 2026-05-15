package posture

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/cluster"
)

// dedupTopic is the cluster pub/sub topic the BufferedRecorder uses
// to share its dedup decisions across replicas. Single global topic
// (no per-account namespace) because the dedupKey already carries the
// AccountID — every subscriber filters by key on receipt.
//
// Why we need this: the dedup cache lives in-process per replica. In
// an N-replica cluster, the first eval after a state change can hit
// each replica once before any of them has populated its local cache,
// producing up to N duplicate rows in the dashboard timeline. The
// broadcast pre-fills the cache on every replica the moment one of
// them persists a row, so subsequent evals on sibling replicas dedup
// against that shared state.
//
// Best-effort: cluster.Coordinator is at-most-once and may drop on
// slow consumers. A dropped event just means the duplicate falls
// through on the affected replica — same as the pre-feature
// behavior. No correctness regression on broker failure.
const dedupTopic = "posture-eval-dedup"

// dedupBroadcast is the JSON envelope on the wire. Decoded only by
// sibling management replicas, so we can evolve the schema in
// lockstep with the producer.
type dedupBroadcast struct {
	Key   dedupKey   `json:"key"`
	Value dedupValue `json:"value"`
}

func encodeDedupBroadcast(key dedupKey, value dedupValue) ([]byte, error) {
	return json.Marshal(dedupBroadcast{Key: key, Value: value})
}

func decodeDedupBroadcast(payload []byte) (dedupKey, dedupValue, error) {
	var b dedupBroadcast
	if err := json.Unmarshal(payload, &b); err != nil {
		return dedupKey{}, dedupValue{}, fmt.Errorf("posture: decode dedup broadcast: %w", err)
	}
	return b.Key, b.Value, nil
}

// publishDedup is fired by Record() right after it commits a new
// dedup-cache entry. Runs in a goroutine — the broker round-trip
// must not stall validatePostureChecksOnPeer when the broker is
// slow or hung. State changes are rare (only on compliance flips
// or refresh-TTL bypass) so the unbounded goroutine spawn here is
// bounded in practice.
//
// Errors are logged at Warn and swallowed — the local write already
// succeeded; a failed broadcast only means peers may write a
// duplicate, not a correctness break.
func publishDedup(coord cluster.Coordinator, key dedupKey, value dedupValue) {
	if coord == nil {
		return
	}
	// Encode eagerly (cheap, deterministic) so a malformed key/value
	// surfaces in the caller's goroutine instead of being swallowed
	// silently in the detached one.
	payload, err := encodeDedupBroadcast(key, value)
	if err != nil {
		log.Warnf("posture: encode dedup broadcast: %v", err)
		return
	}
	go func() {
		// Detached short ctx — caps the worst case if the broker
		// hangs. The caller is no longer blocked on this, but we
		// still don't want a stuck goroutine pile-up under a long
		// outage.
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if err := coord.Publish(ctx, dedupTopic, payload); err != nil {
			log.Warnf("posture: publish dedup broadcast: %v", err)
		}
	}()
}

// subscribeDedup wires a goroutine that consumes peer dedup
// broadcasts and applies them to the local cache via
// applyDedupBroadcast — bypassing publishDedup so the inbound
// event doesn't echo back to the broker (which would cause an N²
// amplification in an N-replica cluster).
//
// Returns the cancel func the caller must invoke at teardown. The
// goroutine exits when ctx is cancelled OR the subscription channel
// is closed by the coordinator (broker outage, Close, etc.).
func subscribeDedup(parent context.Context, coord cluster.Coordinator, r *BufferedRecorder) (context.CancelFunc, error) {
	if coord == nil {
		return func() {}, nil
	}
	ctx, cancel := context.WithCancel(parent)
	events, err := coord.Subscribe(ctx, dedupTopic)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("posture: subscribe dedup: %w", err)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				key, value, err := decodeDedupBroadcast(ev.Payload)
				if err != nil {
					// Malformed payload from a future producer version
					// or a broker hiccup — skip and keep consuming.
					log.Warnf("posture: drop malformed dedup broadcast: %v", err)
					continue
				}
				r.applyDedupBroadcast(key, value)
			}
		}
	}()
	return cancel, nil
}
