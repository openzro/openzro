package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	flowExports "github.com/openzro/openzro/management/server/flow_exports"
)

// ErrNotFound is returned when no row matches the lookup. Callers
// translate it to HTTP 404 at the API layer.
var ErrNotFound = errors.New("providers: authentication provider not found")

// Store provides CRUD for AuthenticationProvider rows with
// credentials encrypted at rest. Encryption uses the same envelope
// (AES-256-GCM keyed by HttpConfig.DataStoreEncryptionKey) as
// flow_exports and mdm — same key, same threat model.
type Store struct {
	db      *gorm.DB
	encrypt *flowExports.FieldEncrypt
}

// NewStore wires a Store and runs AutoMigrate.
func NewStore(db *gorm.DB, key string) (*Store, error) {
	if db == nil {
		return nil, errors.New("providers: db is required")
	}
	enc, err := flowExports.NewFieldEncrypt(key)
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&AuthenticationProvider{}); err != nil {
		return nil, err
	}
	return &Store{db: db, encrypt: enc}, nil
}

// SaveInput is the public payload for Save. ID == 0 creates;
// ID > 0 updates the matching row.
type SaveInput struct {
	ID              uint64
	Name            string
	Type            ProviderType
	Enabled         bool
	Config          Config
	BrandLabel      string
	BrandLogoURL    string
	EmailDomainHint string
}

// Validate enforces the schema-level invariants. The dashboard
// pre-fills IssuerURL per type, so by the time a request reaches
// the Store all four fields below are populated.
func (in *SaveInput) Validate() error {
	if in.Name == "" {
		return errors.New("providers: name is required")
	}
	if in.Type == "" {
		return errors.New("providers: type is required")
	}
	if in.Config.IssuerURL == "" {
		return errors.New("providers: config.issuer_url is required")
	}
	if in.Config.ClientID == "" {
		return errors.New("providers: config.client_id is required")
	}
	if in.Config.ClientSecret == "" {
		return errors.New("providers: config.client_secret is required")
	}
	return nil
}

// Save creates or updates a row. ConfigCipher holds the encrypted
// Config JSON; PublicConfig holds the redacted projection.
func (s *Store) Save(ctx context.Context, in SaveInput) (*AuthenticationProvider, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	cfgBlob, err := json.Marshal(in.Config)
	if err != nil {
		return nil, fmt.Errorf("providers: marshal config: %w", err)
	}
	cipherBytes, err := s.encrypt.Encrypt(cfgBlob)
	if err != nil {
		return nil, fmt.Errorf("providers: encrypt: %w", err)
	}
	publicBlob, err := json.Marshal(in.Config.PublicView())
	if err != nil {
		return nil, fmt.Errorf("providers: marshal public config: %w", err)
	}
	row := AuthenticationProvider{
		ID:              in.ID,
		Name:            in.Name,
		Type:            in.Type,
		Enabled:         in.Enabled,
		PublicConfig:    publicBlob,
		ConfigCipher:    cipherBytes,
		BrandLabel:      in.BrandLabel,
		BrandLogoURL:    in.BrandLogoURL,
		EmailDomainHint: in.EmailDomainHint,
		UpdatedAt:       time.Now().UTC(),
	}
	if in.ID == 0 {
		row.CreatedAt = row.UpdatedAt
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, err
		}
	} else {
		var existing AuthenticationProvider
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

// List returns rows ordered by ID ascending. The admin UI uses this.
func (s *Store) List(ctx context.Context) ([]AuthenticationProvider, error) {
	var rows []AuthenticationProvider
	if err := s.db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListEnabled returns only rows with Enabled = true. The /login
// page consults this; disabled providers are admin-only.
func (s *Store) ListEnabled(ctx context.Context) ([]AuthenticationProvider, error) {
	var rows []AuthenticationProvider
	if err := s.db.WithContext(ctx).
		Where("enabled = ?", true).
		Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Get returns one row by ID.
func (s *Store) Get(ctx context.Context, id uint64) (*AuthenticationProvider, error) {
	var row AuthenticationProvider
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

// Delete removes the row with the given ID. Returns ErrNotFound
// when no row matched.
func (s *Store) Delete(ctx context.Context, id uint64) error {
	res := s.db.WithContext(ctx).Delete(&AuthenticationProvider{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Decrypt reads ConfigCipher into a Config struct. The OIDC
// manager calls this to instantiate live oidc.Provider objects.
func (s *Store) Decrypt(row *AuthenticationProvider) (*Config, error) {
	if row == nil {
		return nil, errors.New("providers: nil row")
	}
	plain, err := s.encrypt.Decrypt(row.ConfigCipher)
	if err != nil {
		return nil, fmt.Errorf("providers: decrypt: %w", err)
	}
	var c Config
	if err := json.Unmarshal(plain, &c); err != nil {
		return nil, fmt.Errorf("providers: unmarshal config: %w", err)
	}
	return &c, nil
}
