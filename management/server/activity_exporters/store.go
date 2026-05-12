package activity_exporters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/openzro/openzro/management/server/activity/exporter"
	"github.com/openzro/openzro/management/server/flow_exports"
)

// ErrNotFound is returned when no row matches the lookup. Callers
// translate to HTTP 404 at the API boundary.
var ErrNotFound = errors.New("activity_exporters: not found")

// Store provides CRUD on ActivityExporter rows with credential
// encryption applied at the boundary. The encryption envelope is
// shared with flow_exports so a single DataStoreEncryptionKey
// protects both subsystems.
type Store struct {
	db      *gorm.DB
	encrypt *flow_exports.FieldEncrypt
}

// NewStore wires a Store. AutoMigrate runs here so callers don't have
// to remember.
func NewStore(db *gorm.DB, key string) (*Store, error) {
	if db == nil {
		return nil, errors.New("activity_exporters: db is required")
	}
	enc, err := flow_exports.NewFieldEncrypt(key)
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&ActivityExporter{}); err != nil {
		return nil, err
	}
	return &Store{db: db, encrypt: enc}, nil
}

// SaveInput is the public-facing payload for Save. Type drives which
// of the three Config fields is consulted (others must be nil).
type SaveInput struct {
	ID        uint64
	AccountID string
	Name      string
	Type      ExporterType
	Enabled   bool
	Template  string
	HTTP      *HTTPDestConfig
	Datadog   *DatadogDestConfig
	Elastic   *ElasticDestConfig
}

// Validate rejects obviously broken input. Type-specific deeper
// checks (URL parse, etc.) live in the API layer where we can return
// nicer error messages.
func (in *SaveInput) Validate() error {
	if in.AccountID == "" {
		return errors.New("activity_exporters: account_id is required")
	}
	if in.Name == "" {
		return errors.New("activity_exporters: name is required")
	}
	if in.Template != "" {
		if err := exporter.ValidateTemplate(in.Template); err != nil {
			return fmt.Errorf("activity_exporters: %w", err)
		}
	}
	switch in.Type {
	case TypeHTTP:
		if in.HTTP == nil || in.HTTP.URL == "" {
			return errors.New("activity_exporters: http URL is required")
		}
	case TypeDatadog:
		if in.Datadog == nil || in.Datadog.APIKey == "" {
			return errors.New("activity_exporters: datadog api_key is required")
		}
	case TypeElastic:
		if in.Elastic == nil || in.Elastic.URL == "" {
			return errors.New("activity_exporters: elastic URL is required")
		}
		if in.Elastic.APIKey == "" && in.Elastic.Username == "" {
			return errors.New("activity_exporters: elastic auth (api_key or basic) is required")
		}
	default:
		return fmt.Errorf("activity_exporters: unsupported type %q", in.Type)
	}
	return nil
}

func (in *SaveInput) configBlob() ([]byte, error) {
	switch in.Type {
	case TypeHTTP:
		return json.Marshal(in.HTTP)
	case TypeDatadog:
		return json.Marshal(in.Datadog)
	case TypeElastic:
		return json.Marshal(in.Elastic)
	}
	return nil, fmt.Errorf("activity_exporters: unknown type %q", in.Type)
}

func (in *SaveInput) publicBlob() ([]byte, error) {
	switch in.Type {
	case TypeHTTP:
		return json.Marshal(in.HTTP.PublicView())
	case TypeDatadog:
		return json.Marshal(in.Datadog.PublicView())
	case TypeElastic:
		return json.Marshal(in.Elastic.PublicView())
	}
	return nil, fmt.Errorf("activity_exporters: unknown type %q", in.Type)
}

// MergeIncomingSecret preserves the existing secret if the caller
// posts an empty value on update — the API never reads secrets back,
// so the dashboard sends "" to mean "leave it as is". Without this,
// every Save round-trips to the receiver as "auth disabled."
func (in *SaveInput) MergeIncomingSecret(prev any) {
	switch in.Type {
	case TypeHTTP:
		// HTTP carries no exclusive secret field — auth is operator-
		// supplied via Headers. Nothing to merge.
	case TypeDatadog:
		if in.Datadog == nil {
			return
		}
		if in.Datadog.APIKey != "" {
			return
		}
		if pc, ok := prev.(*DatadogDestConfig); ok && pc != nil {
			in.Datadog.APIKey = pc.APIKey
		}
	case TypeElastic:
		if in.Elastic == nil {
			return
		}
		if pc, ok := prev.(*ElasticDestConfig); ok && pc != nil {
			if in.Elastic.APIKey == "" {
				in.Elastic.APIKey = pc.APIKey
			}
			// Username is not strictly a secret but the public projection
			// only exposes AuthMode, so the dashboard cannot pre-fill it
			// either. Treat empty-on-update as "leave as is" to match the
			// Password/APIKey convention.
			if in.Elastic.Username == "" {
				in.Elastic.Username = pc.Username
			}
			if in.Elastic.Password == "" {
				in.Elastic.Password = pc.Password
			}
		}
	}
}

// Save creates or updates a row. Sensitive fields are encrypted
// before INSERT/UPDATE. Returns the persisted (decrypted) row.
func (s *Store) Save(ctx context.Context, in SaveInput) (*ActivityExporter, error) {
	if in.ID != 0 {
		// Preserve secrets when the caller posts placeholders.
		existing, err := s.Get(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if existing.AccountID != in.AccountID {
			return nil, ErrNotFound
		}
		prev, err := s.Decrypt(existing)
		if err != nil {
			return nil, fmt.Errorf("activity_exporters: decrypt prev: %w", err)
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
		return nil, fmt.Errorf("activity_exporters: encrypt: %w", err)
	}
	publicBlob, err := in.publicBlob()
	if err != nil {
		return nil, err
	}

	row := ActivityExporter{
		ID:           in.ID,
		AccountID:    in.AccountID,
		Name:         in.Name,
		Type:         in.Type,
		Enabled:      in.Enabled,
		Template:     in.Template,
		PublicConfig: publicBlob,
		ConfigCipher: cipherBytes,
		UpdatedAt:    time.Now().UTC(),
	}
	if in.ID == 0 {
		row.CreatedAt = row.UpdatedAt
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, err
		}
	} else {
		var existing ActivityExporter
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

// List returns every exporter configured under accountID, sorted
// by ID. Empty accountID returns the global view (across all tenants)
// — used by the manager at startup.
func (s *Store) List(ctx context.Context, accountID string) ([]ActivityExporter, error) {
	var rows []ActivityExporter
	q := s.db.WithContext(ctx).Order("id ASC")
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Get fetches a single row. Returns ErrNotFound if missing.
func (s *Store) Get(ctx context.Context, id uint64) (*ActivityExporter, error) {
	var row ActivityExporter
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

// Delete removes a row scoped to the supplied accountID. The
// account_id check defends against ID-guessing across tenants.
func (s *Store) Delete(ctx context.Context, accountID string, id uint64) error {
	res := s.db.WithContext(ctx).Where("account_id = ?", accountID).Delete(&ActivityExporter{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Decrypt reads ConfigCipher into the type-appropriate Go struct.
// Used by the manager to construct live Exporter instances.
func (s *Store) Decrypt(row *ActivityExporter) (any, error) {
	plain, err := s.encrypt.Decrypt(row.ConfigCipher)
	if err != nil {
		return nil, err
	}
	switch row.Type {
	case TypeHTTP:
		var c HTTPDestConfig
		return &c, json.Unmarshal(plain, &c)
	case TypeDatadog:
		var c DatadogDestConfig
		return &c, json.Unmarshal(plain, &c)
	case TypeElastic:
		var c ElasticDestConfig
		return &c, json.Unmarshal(plain, &c)
	}
	return nil, fmt.Errorf("activity_exporters: unknown type %q on decrypt", row.Type)
}
