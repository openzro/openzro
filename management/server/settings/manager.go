package settings

//go:generate go run github.com/golang/mock/mockgen -package settings -destination=manager_mock.go -source=./manager.go -build_flags=-mod=mod

import (
	"context"
	"fmt"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/integrations/extra_settings"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
	"github.com/openzro/openzro/management/server/users"
)

type Manager interface {
	GetExtraSettingsManager() extra_settings.Manager
	GetSettings(ctx context.Context, accountID string, userID string) (*types.Settings, error)
	GetExtraSettings(ctx context.Context, accountID string) (*types.ExtraSettings, error)
	UpdateExtraSettings(ctx context.Context, accountID, userID string, extraSettings *types.ExtraSettings) (bool, error)
}

type managerImpl struct {
	store                store.Store
	extraSettingsManager extra_settings.Manager
	userManager          users.Manager
	permissionsManager   permissions.Manager
}

func NewManager(store store.Store, userManager users.Manager, extraSettingsManager extra_settings.Manager, permissionsManager permissions.Manager) Manager {
	return &managerImpl{
		store:                store,
		extraSettingsManager: extraSettingsManager,
		userManager:          userManager,
		permissionsManager:   permissionsManager,
	}
}

func (m *managerImpl) GetExtraSettingsManager() extra_settings.Manager {
	return m.extraSettingsManager
}

func (m *managerImpl) GetSettings(ctx context.Context, accountID, userID string) (*types.Settings, error) {
	if userID != activity.SystemInitiator {
		ok, err := m.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Settings, operations.Read)
		if err != nil {
			return nil, status.NewPermissionValidationError(err)
		}
		if !ok {
			return nil, status.NewPermissionDeniedError()
		}
	}

	settings, err := m.store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account settings: %w", err)
	}

	// Trust the GORM-backed columns under accounts.settings_extra_*
	// (see types.Settings.Extra and types.ExtraSettings). Upstream
	// NetBird is mid-refactor moving Flow* into a side `extra_settings.Manager`
	// store, but their open-source build ships only a no-op stub
	// (management/integrations/integrations/extra_settings.go) — a stub
	// that, prior to alpha.25, silently overwrote freshly-loaded GORM
	// values with zeros on every read, breaking the dashboard's flow
	// toggle UX entirely (toast says "enabled", refresh shows disabled).
	// Until upstream actually ships a real backend for this manager
	// (and we choose to consume it under our BSD-3 posture), GORM is
	// the source of truth for ExtraSettings.
	if settings.Extra == nil {
		settings.Extra = &types.ExtraSettings{}
	}
	return settings, nil
}

func (m *managerImpl) GetExtraSettings(ctx context.Context, accountID string) (*types.ExtraSettings, error) {
	settings, err := m.store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account settings: %w", err)
	}
	if settings.Extra == nil {
		return &types.ExtraSettings{}, nil
	}
	return settings.Extra, nil
}

func (m *managerImpl) UpdateExtraSettings(ctx context.Context, accountID, userID string, extraSettings *types.ExtraSettings) (bool, error) {
	return m.extraSettingsManager.UpdateExtraSettings(ctx, accountID, userID, extraSettings)
}
