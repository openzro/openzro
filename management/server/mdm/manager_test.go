package mdm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider is a Provider that returns canned responses for tests.
type fakeProvider struct {
	t        ProviderType
	statuses map[string]DeviceStatus
	err      error
	calls    int
}

func (f *fakeProvider) Type() ProviderType { return f.t }
func (f *fakeProvider) Close() error       { return nil }
func (f *fakeProvider) GetDeviceStatus(_ context.Context, lookup DeviceLookup) (DeviceStatus, error) {
	f.calls++
	if f.err != nil {
		return DeviceStatus{}, f.err
	}
	s, ok := f.statuses[lookup.Hostname]
	if !ok {
		return DeviceStatus{Found: false}, nil
	}
	return s, nil
}

func TestStatusCache_HitMissExpiry(t *testing.T) {
	c := newStatusCache(50 * time.Millisecond)
	_, ok := c.get("dev-1")
	assert.False(t, ok, "miss on cold cache")

	c.put("dev-1", DeviceStatus{Found: true, Compliant: true})
	got, ok := c.get("dev-1")
	require.True(t, ok)
	assert.True(t, got.Compliant)

	time.Sleep(80 * time.Millisecond)
	_, ok = c.get("dev-1")
	assert.False(t, ok, "entry must expire after TTL")
}

func TestCachedProvider_DeduplicatesCalls(t *testing.T) {
	inner := &fakeProvider{
		t: TypeIntune,
		statuses: map[string]DeviceStatus{
			"dev-1": {Found: true, Compliant: true},
		},
	}
	cp := NewCachedProvider(inner, time.Hour)

	for i := 0; i < 5; i++ {
		s, err := cp.GetDeviceStatus(context.Background(), DeviceLookup{Hostname: "dev-1"})
		require.NoError(t, err)
		assert.True(t, s.Compliant)
	}
	assert.Equal(t, 1, inner.calls,
		"5 lookups for the same device must collapse into 1 vendor call")
}

func TestCachedProvider_DifferentDevicesDoNotShareCacheEntry(t *testing.T) {
	inner := &fakeProvider{
		t: TypeIntune,
		statuses: map[string]DeviceStatus{
			"dev-A": {Found: true, Compliant: true},
			"dev-B": {Found: true, Compliant: false, Reason: "out of date"},
		},
	}
	cp := NewCachedProvider(inner, time.Hour)

	a, _ := cp.GetDeviceStatus(context.Background(), DeviceLookup{Hostname: "dev-A"})
	b, _ := cp.GetDeviceStatus(context.Background(), DeviceLookup{Hostname: "dev-B"})
	assert.True(t, a.Compliant)
	assert.False(t, b.Compliant)
	assert.Equal(t, 2, inner.calls)
}

func TestCachedProvider_DoesNotCacheErrors(t *testing.T) {
	inner := &fakeProvider{t: TypeIntune, err: errors.New("boom")}
	cp := NewCachedProvider(inner, time.Hour)

	_, err := cp.GetDeviceStatus(context.Background(), DeviceLookup{Hostname: "dev-1"})
	require.Error(t, err)
	_, err = cp.GetDeviceStatus(context.Background(), DeviceLookup{Hostname: "dev-1"})
	require.Error(t, err)
	assert.Equal(t, 2, inner.calls,
		"errors must NOT be cached — vendor outage shouldn't pin a fail state for the whole TTL")
}
