package mdm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/cluster"
)

// statusTopicPrefix is the cluster pub/sub topic prefix for cross-
// replica MDM cache fills. Each provider gets its own topic
// (statusTopicPrefix + providerID) so subscribers know which cache
// to drop the result into without parsing payloads.
//
// The flow: replica A pays the vendor latency on a cache miss,
// then publishes the fresh status. Other replicas subscribed to the
// same topic populate their local caches without making their own
// API call. In an N-replica cluster this turns one wall-clock
// minute of churn into ~1/N of the API budget we'd otherwise burn.
//
// Best-effort delivery (cluster.Coordinator is at-most-once): a
// missed broadcast just means the second replica pays its own
// vendor call on next miss, same as before this feature existed.
// We don't try to make this strongly consistent.
const statusTopicPrefix = "mdm-status:"

// statusTopic returns the topic for a given provider row ID.
func statusTopic(providerID uint64) string {
	return statusTopicPrefix + strconv.FormatUint(providerID, 10)
}

// statusBroadcast is the JSON shape that travels through the broker.
// Decoded only by other replicas of the same management process —
// not exposed to external systems — so we can evolve the wire format
// in lockstep with the producer.
type statusBroadcast struct {
	Lookup DeviceLookup `json:"lookup"`
	Status DeviceStatus `json:"status"`
}

func encodeStatusBroadcast(lookup DeviceLookup, status DeviceStatus) ([]byte, error) {
	return json.Marshal(statusBroadcast{Lookup: lookup, Status: status})
}

func decodeStatusBroadcast(payload []byte) (DeviceLookup, DeviceStatus, error) {
	var b statusBroadcast
	if err := json.Unmarshal(payload, &b); err != nil {
		return DeviceLookup{}, DeviceStatus{}, fmt.Errorf("mdm: decode status broadcast: %w", err)
	}
	return b.Lookup, b.Status, nil
}

// publishStatus is the helper the CachedProvider calls after a fresh
// inner.GetDeviceStatus succeeds. Errors are logged at Warn and
// swallowed — the cache fill on this replica already succeeded, and
// a failed broadcast just means the other replicas pay their own
// vendor call next time.
func publishStatus(
	ctx context.Context, coord cluster.Coordinator,
	providerID uint64, lookup DeviceLookup, status DeviceStatus,
) {
	if coord == nil {
		return
	}
	payload, err := encodeStatusBroadcast(lookup, status)
	if err != nil {
		log.WithContext(ctx).Warnf("mdm: encode status broadcast for provider %d: %v", providerID, err)
		return
	}
	if err := coord.Publish(ctx, statusTopic(providerID), payload); err != nil {
		log.WithContext(ctx).Warnf("mdm: publish status broadcast for provider %d: %v", providerID, err)
	}
}

// subscribeStatus wires a goroutine that consumes status broadcasts
// for one provider and pours them into the local CachedProvider's
// cache via putFromBroker — bypassing the publish hook so the
// inbound event doesn't echo back to the broker.
//
// Returns the cancel func the caller must invoke at teardown. The
// goroutine exits when ctx is canceled OR the subscription channel
// is closed by the coordinator (broker outage, Close, etc.).
func subscribeStatus(
	parent context.Context, coord cluster.Coordinator,
	providerID uint64, provider *CachedProvider,
) (context.CancelFunc, error) {
	if coord == nil {
		// Single-instance mode — no subscription needed.
		return func() {}, nil
	}
	ctx, cancel := context.WithCancel(parent)
	events, err := coord.Subscribe(ctx, statusTopic(providerID))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("mdm: subscribe provider %d: %w", providerID, err)
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
				lookup, status, err := decodeStatusBroadcast(ev.Payload)
				if err != nil {
					// Malformed payload on the topic — probably from a
					// future version of the producer. Skip and keep
					// consuming so one bad event doesn't poison the
					// whole subscription.
					log.WithContext(ctx).Warnf("mdm: drop malformed status broadcast for provider %d: %v", providerID, err)
					continue
				}
				provider.putFromBroker(lookup, status)
			}
		}
	}()
	return cancel, nil
}
