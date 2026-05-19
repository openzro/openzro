package mdm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goversion "github.com/hashicorp/go-version"
	"gorm.io/gorm"

	flowExports "github.com/openzro/openzro/management/server/flow_exports"
)

// validateSentinelOneCompliance rejects nonsensical operator input
// at save time so a bad config can never reach the eval hot path
// (where a misparse would fail-closed and silently block peers).
func validateSentinelOneCompliance(c SentinelOneCompliance) error {
	if c.MaxActiveThreats != nil && *c.MaxActiveThreats < 0 {
		return errors.New("mdm: sentinelone max_active_threats must be >= 0")
	}
	if c.SyncWindowMinutes < 0 {
		return errors.New("mdm: sentinelone sync_window_minutes must be >= 0")
	}
	if c.MinAgentVersion != "" {
		if _, err := goversion.NewVersion(c.MinAgentVersion); err != nil {
			return fmt.Errorf("mdm: sentinelone min_agent_version %q is not a valid version", c.MinAgentVersion)
		}
	}
	return nil
}

// ErrNotFound is returned when no row matches the lookup. Callers
// translate it to HTTP 404 at the API layer.
var ErrNotFound = errors.New("mdm: provider not found")

// Store provides CRUD for ProviderRow rows with credentials encrypted
// at rest. The encryption helper is reused from flow_exports — same
// envelope, same key, same threat model.
type Store struct {
	db      *gorm.DB
	encrypt *flowExports.FieldEncrypt
}

// NewStore wires a Store. AutoMigrate runs on construction.
func NewStore(db *gorm.DB, key string) (*Store, error) {
	if db == nil {
		return nil, errors.New("mdm: db is required")
	}
	enc, err := flowExports.NewFieldEncrypt(key)
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&ProviderRow{}); err != nil {
		return nil, err
	}
	return &Store{db: db, encrypt: enc}, nil
}

// SaveInput is the public-facing payload for Save/Update. Type
// determines which of the per-vendor Config fields is consulted.
type SaveInput struct {
	ID      uint64
	Name    string
	Type    ProviderType
	Enabled bool

	// RefreshIntervalMinutes is forwarded onto the row verbatim.
	// 0 = "use default" — Save() rewrites it to
	// defaultRefreshIntervalMinutes before insert so the column
	// never holds zero in steady state. Bounded 1-60 by Validate.
	RefreshIntervalMinutes uint16

	Intune      *IntuneConfig
	SentinelOne *SentinelOneConfig
	Huntress    *HuntressConfig
	CrowdStrike *CrowdStrikeConfig
}

// Bounds for SaveInput.RefreshIntervalMinutes. Validate enforces
// these; the dashboard form mirrors them as input min/max.
const (
	minRefreshIntervalMinutes = 1
	maxRefreshIntervalMinutes = 60
)

func (in *SaveInput) Validate() error {
	if in.Name == "" {
		return errors.New("mdm: name is required")
	}
	if in.RefreshIntervalMinutes != 0 &&
		(in.RefreshIntervalMinutes < minRefreshIntervalMinutes ||
			in.RefreshIntervalMinutes > maxRefreshIntervalMinutes) {
		return fmt.Errorf(
			"mdm: refresh_interval_minutes must be between %d and %d (got %d)",
			minRefreshIntervalMinutes, maxRefreshIntervalMinutes,
			in.RefreshIntervalMinutes,
		)
	}
	switch in.Type {
	case TypeIntune:
		if in.Intune == nil {
			return errors.New("mdm: intune config required for type=intune")
		}
		if in.Intune.TenantID == "" || in.Intune.ClientID == "" {
			return errors.New("mdm: intune tenant_id and client_id are required")
		}
	case TypeSentinelOne:
		if in.SentinelOne == nil || in.SentinelOne.ManagementURL == "" {
			return errors.New("mdm: sentinelone management_url is required")
		}
		if err := validateSentinelOneCompliance(in.SentinelOne.Compliance); err != nil {
			return err
		}
	case TypeHuntress:
		if in.Huntress == nil {
			return errors.New("mdm: huntress config required for type=huntress")
		}
	case TypeCrowdStrike:
		if in.CrowdStrike == nil {
			return errors.New("mdm: crowdstrike config required for type=crowdstrike")
		}
		if in.CrowdStrike.ClientID == "" {
			return errors.New("mdm: crowdstrike client_id is required")
		}
	default:
		return fmt.Errorf("mdm: unsupported type %q", in.Type)
	}
	return nil
}

func (in *SaveInput) configBlob() ([]byte, error) {
	switch in.Type {
	case TypeIntune:
		return json.Marshal(in.Intune)
	case TypeSentinelOne:
		return json.Marshal(in.SentinelOne)
	case TypeHuntress:
		return json.Marshal(in.Huntress)
	case TypeCrowdStrike:
		return json.Marshal(in.CrowdStrike)
	}
	return nil, fmt.Errorf("mdm: unknown type %q", in.Type)
}

func (in *SaveInput) publicBlob() ([]byte, error) {
	switch in.Type {
	case TypeIntune:
		return json.Marshal(in.Intune.PublicView())
	case TypeSentinelOne:
		return json.Marshal(in.SentinelOne.PublicView())
	case TypeHuntress:
		return json.Marshal(in.Huntress.PublicView())
	case TypeCrowdStrike:
		return json.Marshal(in.CrowdStrike.PublicView())
	}
	return nil, fmt.Errorf("mdm: unknown type %q", in.Type)
}

// MergeIncomingSecret preserves the existing sensitive fields when the
// caller posts empty values on update — the API never reads secrets
// back to the dashboard (PublicView strips them), so the form sends
// "" to mean "leave it as is." Without this merge, every Save that
// doesn't re-type the secret would overwrite the stored credential
// with the empty zero-value, leaving the provider unconfigurable on
// the next manager Refresh (NewIntune et al. reject empty secrets).
//
// Mirrors the same-named method in activity_exporters/store.go —
// the canonical "write-only credential" pattern in this codebase.
func (in *SaveInput) MergeIncomingSecret(prev any) {
	switch in.Type {
	case TypeIntune:
		if in.Intune == nil {
			return
		}
		if in.Intune.ClientSecret != "" {
			return
		}
		if pc, ok := prev.(*IntuneConfig); ok && pc != nil {
			in.Intune.ClientSecret = pc.ClientSecret
		}
	case TypeSentinelOne:
		if in.SentinelOne == nil {
			return
		}
		if in.SentinelOne.APIToken != "" {
			return
		}
		if pc, ok := prev.(*SentinelOneConfig); ok && pc != nil {
			in.SentinelOne.APIToken = pc.APIToken
		}
	case TypeHuntress:
		if in.Huntress == nil {
			return
		}
		if pc, ok := prev.(*HuntressConfig); ok && pc != nil {
			if in.Huntress.APIKey == "" {
				in.Huntress.APIKey = pc.APIKey
			}
			if in.Huntress.APISecret == "" {
				in.Huntress.APISecret = pc.APISecret
			}
		}
	case TypeCrowdStrike:
		if in.CrowdStrike == nil {
			return
		}
		if in.CrowdStrike.ClientSecret != "" {
			return
		}
		if pc, ok := prev.(*CrowdStrikeConfig); ok && pc != nil {
			in.CrowdStrike.ClientSecret = pc.ClientSecret
		}
	}
}

// Save creates or updates a row. Sensitive fields are encrypted
// before INSERT/UPDATE.
func (s *Store) Save(ctx context.Context, in SaveInput) (*ProviderRow, error) {
	if in.ID != 0 {
		// Preserve write-only credentials when the caller posts
		// placeholders. See MergeIncomingSecret for the rationale.
		existing, err := s.Get(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		prev, err := s.Decrypt(existing)
		if err != nil {
			return nil, fmt.Errorf("mdm: decrypt prev: %w", err)
		}
		in.MergeIncomingSecret(prev)
	}

	if err := in.Validate(); err != nil {
		return nil, err
	}
	cfgBlob, err := in.configBlob()
	if err != nil {
		return nil, err
	}
	cipherBytes, err := s.encrypt.Encrypt(cfgBlob)
	if err != nil {
		return nil, fmt.Errorf("mdm: encrypt: %w", err)
	}
	publicBlob, err := in.publicBlob()
	if err != nil {
		return nil, err
	}
	refreshMinutes := in.RefreshIntervalMinutes
	if refreshMinutes == 0 {
		// Collapse 0 (operator didn't fill the form, or upgrader hasn't
		// re-saved a pre-knob row) to the default so the column is
		// never persisted as zero. The resolver in
		// ProviderRow.ResolvedRefreshInterval also tolerates zero, but
		// keeping the DB clean makes the form roundtrip readable.
		refreshMinutes = defaultRefreshIntervalMinutes
	}
	row := ProviderRow{
		ID:                     in.ID,
		Name:                   in.Name,
		Type:                   in.Type,
		Enabled:                in.Enabled,
		RefreshIntervalMinutes: refreshMinutes,
		PublicConfig:           publicBlob,
		ConfigCipher:           cipherBytes,
		UpdatedAt:              time.Now().UTC(),
	}
	if in.ID == 0 {
		row.CreatedAt = row.UpdatedAt
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, err
		}
	} else {
		var existing ProviderRow
		if err := s.db.WithContext(ctx).First(&existing, in.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		row.CreatedAt = existing.CreatedAt
		if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
			return nil, err
		}
	}
	return &row, nil
}

func (s *Store) List(ctx context.Context) ([]ProviderRow, error) {
	var rows []ProviderRow
	if err := s.db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) Get(ctx context.Context, id uint64) (*ProviderRow, error) {
	var row ProviderRow
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (s *Store) Delete(ctx context.Context, id uint64) error {
	res := s.db.WithContext(ctx).Delete(&ProviderRow{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Decrypt reads ConfigCipher into the type-appropriate Go struct.
// Used by the manager to instantiate live Provider drivers.
func (s *Store) Decrypt(row *ProviderRow) (any, error) {
	plain, err := s.encrypt.Decrypt(row.ConfigCipher)
	if err != nil {
		return nil, err
	}
	switch row.Type {
	case TypeIntune:
		var c IntuneConfig
		return &c, json.Unmarshal(plain, &c)
	case TypeSentinelOne:
		var c SentinelOneConfig
		return &c, json.Unmarshal(plain, &c)
	case TypeHuntress:
		var c HuntressConfig
		return &c, json.Unmarshal(plain, &c)
	case TypeCrowdStrike:
		var c CrowdStrikeConfig
		return &c, json.Unmarshal(plain, &c)
	}
	return nil, fmt.Errorf("mdm: unknown type %q on decrypt", row.Type)
}
