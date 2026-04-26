package integrations

import (
	"context"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/integrations/extra_settings"
	"github.com/openzro/openzro/management/server/types"
)

// extraSettingsStub is the no-op extra_settings.Manager. It stores nothing
// and reports an empty ExtraSettings on every read; updates are silently
// dropped (return value indicates "no change applied").
type extraSettingsStub struct{}

// NewManager returns the stub extra_settings.Manager. The eventStore
// argument is accepted (and ignored) so the signature matches what
// richer implementations would need.
func NewManager(_ activity.Store) extra_settings.Manager {
	return &extraSettingsStub{}
}

func (extraSettingsStub) GetExtraSettings(_ context.Context, _ string) (*types.ExtraSettings, error) {
	return &types.ExtraSettings{}, nil
}

func (extraSettingsStub) UpdateExtraSettings(_ context.Context, _, _ string, _ *types.ExtraSettings) (bool, error) {
	return false, nil
}
