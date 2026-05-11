package flow_exports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ErrNotFound is returned when no row matches the lookup. Callers
// translate it to HTTP 404 at the API layer.
var ErrNotFound = errors.New("flow_exports: not found")

// Store provides CRUD for FlowExport rows with credential encryption
// applied at the boundary. Callers pass plaintext config; the Store
// encrypts before INSERT/UPDATE and decrypts on Read.
type Store struct {
	db        *gorm.DB
	encrypt   *FieldEncrypt
	publicMap func(t ExportType, plain map[string]any) ([]byte, error)
}

// NewStore wires a Store. AutoMigrate runs here so callers don't have
// to remember.
func NewStore(db *gorm.DB, key string) (*Store, error) {
	if db == nil {
		return nil, errors.New("flow_exports: db is required")
	}
	enc, err := NewFieldEncrypt(key)
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&FlowExport{}); err != nil {
		return nil, err
	}
	return &Store{db: db, encrypt: enc, publicMap: defaultPublicMap}, nil
}

// SaveInput is the public-facing payload for Save/Update. Type
// determines which of the three Config fields is consulted (others
// must be zero).
type SaveInput struct {
	ID      uint64
	Name    string
	Type    ExportType
	Enabled bool
	Elastic *ElasticDestConfig
	S3      *S3DestConfig
	HTTP    *HTTPDestConfig
	Datadog *DatadogDestConfig
	GCS     *GCSDestConfig
}

// Validate ensures Type matches a non-nil Config block and basic
// fields are sane. Type-specific deeper validation lives in the
// API layer, which can return better error messages.
func (in *SaveInput) Validate() error {
	if in.Name == "" {
		return errors.New("flow_exports: name is required")
	}
	switch in.Type {
	case TypeElastic:
		if in.Elastic == nil {
			return errors.New("flow_exports: elastic config required for type=elastic")
		}
		if in.Elastic.URL == "" {
			return errors.New("flow_exports: elastic URL is required")
		}
		if in.Elastic.APIKey == "" && in.Elastic.Username == "" {
			return errors.New("flow_exports: elastic auth required (api_key or username/password)")
		}
	case TypeS3:
		if in.S3 == nil {
			return errors.New("flow_exports: s3 config required for type=s3")
		}
		if in.S3.Bucket == "" {
			return errors.New("flow_exports: s3 bucket is required")
		}
	case TypeHTTP:
		if in.HTTP == nil {
			return errors.New("flow_exports: http config required for type=http")
		}
		if in.HTTP.URL == "" {
			return errors.New("flow_exports: http URL is required")
		}
	case TypeDatadog:
		if in.Datadog == nil || in.Datadog.APIKey == "" {
			return errors.New("flow_exports: datadog api_key is required")
		}
	case TypeGCS:
		if in.GCS == nil || in.GCS.Bucket == "" {
			return errors.New("flow_exports: gcs bucket is required")
		}
	default:
		return fmt.Errorf("flow_exports: unsupported type %q", in.Type)
	}
	return nil
}

// configBlob marshals the type-specific config block to JSON.
func (in *SaveInput) configBlob() ([]byte, error) {
	switch in.Type {
	case TypeElastic:
		return json.Marshal(in.Elastic)
	case TypeS3:
		return json.Marshal(in.S3)
	case TypeHTTP:
		return json.Marshal(in.HTTP)
	case TypeDatadog:
		return json.Marshal(in.Datadog)
	case TypeGCS:
		return json.Marshal(in.GCS)
	}
	return nil, fmt.Errorf("flow_exports: unknown type %q", in.Type)
}

func (in *SaveInput) publicBlob() ([]byte, error) {
	switch in.Type {
	case TypeElastic:
		return json.Marshal(in.Elastic.PublicView())
	case TypeS3:
		return json.Marshal(in.S3.PublicView())
	case TypeHTTP:
		return json.Marshal(in.HTTP.PublicView())
	case TypeDatadog:
		return json.Marshal(in.Datadog.PublicView())
	case TypeGCS:
		return json.Marshal(in.GCS.PublicView())
	}
	return nil, fmt.Errorf("flow_exports: unknown type %q", in.Type)
}

// MergeIncomingSecret preserves the existing credential fields when
// the caller posts an empty value on update. The API never reads
// secrets back, so the dashboard sends "" to mean "leave as is". On
// Elastic the same convention extends to Username because the public
// projection only exposes AuthMode, not the actual identifier.
func (in *SaveInput) MergeIncomingSecret(prev any) {
	switch in.Type {
	case TypeElastic:
		if in.Elastic == nil {
			return
		}
		pc, ok := prev.(*ElasticDestConfig)
		if !ok || pc == nil {
			return
		}
		if in.Elastic.APIKey == "" {
			in.Elastic.APIKey = pc.APIKey
		}
		if in.Elastic.Username == "" {
			in.Elastic.Username = pc.Username
		}
		if in.Elastic.Password == "" {
			in.Elastic.Password = pc.Password
		}
	case TypeS3:
		if in.S3 == nil {
			return
		}
		pc, ok := prev.(*S3DestConfig)
		if !ok || pc == nil {
			return
		}
		if in.S3.AccessKey == "" {
			in.S3.AccessKey = pc.AccessKey
		}
		if in.S3.SecretKey == "" {
			in.S3.SecretKey = pc.SecretKey
		}
	case TypeHTTP:
		// HTTP carries operator-defined header auth; the dashboard
		// does not expose a headers field for flow exports today, so
		// there is nothing to merge.
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
	case TypeGCS:
		if in.GCS == nil {
			return
		}
		pc, ok := prev.(*GCSDestConfig)
		if !ok || pc == nil {
			return
		}
		if in.GCS.CredentialsJSON == "" {
			in.GCS.CredentialsJSON = pc.CredentialsJSON
		}
		if in.GCS.CredentialsFile == "" {
			in.GCS.CredentialsFile = pc.CredentialsFile
		}
	}
}

// Save creates or updates a row. Sensitive fields are encrypted
// before INSERT/UPDATE. Returns the persisted ID and (decrypted)
// FlowExport so the API can return it directly.
func (s *Store) Save(ctx context.Context, in SaveInput) (*FlowExport, error) {
	// Update path: fetch the existing row, decrypt its config, and
	// fall back to the previous secret values for any incoming field
	// the caller left blank. Without this, every Save round-trips as
	// "credentials wiped" and the next ApplyAll fails to authenticate.
	var existing *FlowExport
	if in.ID != 0 {
		row, err := s.Get(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		prev, err := s.Decrypt(row)
		if err != nil {
			return nil, fmt.Errorf("flow_exports: decrypt prev: %w", err)
		}
		in.MergeIncomingSecret(prev)
		existing = row
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
		return nil, fmt.Errorf("flow_exports: encrypt: %w", err)
	}
	publicBlob, err := in.publicBlob()
	if err != nil {
		return nil, err
	}

	row := FlowExport{
		ID:           in.ID,
		Name:         in.Name,
		Type:         in.Type,
		Enabled:      in.Enabled,
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
		// Update path: take the existing CreatedAt so we don't shift it.
		row.CreatedAt = existing.CreatedAt
		if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
			return nil, err
		}
	}
	return &row, nil
}

// List returns all configured exports, sorted by ID.
func (s *Store) List(ctx context.Context) ([]FlowExport, error) {
	var rows []FlowExport
	if err := s.db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Get fetches a single export. Returns ErrNotFound if missing.
func (s *Store) Get(ctx context.Context, id uint64) (*FlowExport, error) {
	var row FlowExport
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

// Delete removes a row. Returns ErrNotFound if it never existed.
func (s *Store) Delete(ctx context.Context, id uint64) error {
	res := s.db.WithContext(ctx).Delete(&FlowExport{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Decrypt reads ConfigCipher into the type-appropriate Go struct.
// Used by the manager to build live Sinks from persisted rows.
func (s *Store) Decrypt(row *FlowExport) (any, error) {
	plain, err := s.encrypt.Decrypt(row.ConfigCipher)
	if err != nil {
		return nil, err
	}
	switch row.Type {
	case TypeElastic:
		var c ElasticDestConfig
		return &c, json.Unmarshal(plain, &c)
	case TypeS3:
		var c S3DestConfig
		return &c, json.Unmarshal(plain, &c)
	case TypeHTTP:
		var c HTTPDestConfig
		return &c, json.Unmarshal(plain, &c)
	case TypeDatadog:
		var c DatadogDestConfig
		return &c, json.Unmarshal(plain, &c)
	case TypeGCS:
		var c GCSDestConfig
		return &c, json.Unmarshal(plain, &c)
	}
	return nil, fmt.Errorf("flow_exports: unknown type %q on decrypt", row.Type)
}

// defaultPublicMap is used when callers do not override publicMap; it
// mirrors what the SaveInput already produced.
func defaultPublicMap(_ ExportType, plain map[string]any) ([]byte, error) {
	return json.Marshal(plain)
}
