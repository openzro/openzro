package posture

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// PostureEvaluation is one row of "what happened when posture check X
// ran on peer Y at time T". The dashboard's per-peer Posture Status
// panel reads the last N records for a peer to render the timeline
// that closed the cluster-debugging loop: instead of grepping
// management logs at INFO level to find out "why was this peer
// blocked", an operator opens /peer/<id> and sees the answer.
//
// Records are written by the eval hot path (validatePostureChecksOnPeer
// in management/server/types/account.go) through a buffered
// EvalRecorder so we don't add DB latency to the firewall-rule
// generation step. A retention job trims rows older than the
// configured TTL.
//
// AccountID is denormalised onto every row so the retention worker
// and the per-account API queries don't have to JOIN through
// peers/posture_checks for the (very) common access pattern.
type PostureEvaluation struct {
	// ID is an auto-incrementing surrogate. The natural key
	// (account_id, peer_id, posture_check_id, evaluated_at) is unique
	// enough in practice — same peer + same check at the same UTC ms
	// would only happen on retries; we accept that as duplicates
	// rather than enforce uniqueness with a constraint.
	ID uint64 `gorm:"primaryKey;autoIncrement"`

	AccountID string `gorm:"index:idx_posture_eval_account_peer_time,priority:1;not null"`
	PeerID    string `gorm:"index:idx_posture_eval_account_peer_time,priority:2;not null"`

	// PostureCheckID references posture_checks.id. Not a hard FK to
	// keep the writer hot path independent of referential integrity
	// at write time (a check could be deleted between eval and write
	// flush). The dashboard handles missing parents gracefully.
	PostureCheckID string `gorm:"not null"`

	// CheckType is the value returned by Check.Name() — one of
	// "EndpointSecurityCheck", "NBVersionCheck", "OSVersionCheck",
	// "ProcessCheck", "PeerNetworkRangeCheck", "GeoLocationCheck",
	// "ScheduleCheck". Persisted denormalised so the dashboard can
	// render the row without joining + so the column stays readable
	// when the parent check row is gone.
	CheckType string `gorm:"size:64;not null"`

	// Compliant is the bool return of Check.Check() — true means the
	// peer satisfied this individual check. The eval as a whole only
	// passes when ALL checks return true; the row captures one check
	// at a time.
	Compliant bool `gorm:"not null"`

	// Reason is the error string returned by Check.Check() when
	// Compliant=false, or empty when Compliant=true. For
	// EndpointSecurityCheck this is shaped like
	//
	//   "endpoint-security: device not enrolled in Intune
	//    (hostname=..., user=..., os=...)"
	//
	// — same string the Info log line carries, so dashboard +
	// logs say the same thing.
	Reason string `gorm:"type:text"`

	// EvaluatedAt is the eval clock in UTC. Indexed DESC alongside
	// the (account_id, peer_id) prefix so "last N evaluations for
	// this peer" is a single index range scan.
	EvaluatedAt time.Time `gorm:"index:idx_posture_eval_account_peer_time,priority:3,sort:desc;not null"`
}

// TableName pins the GORM table name so the auto-migration emits the
// expected SQL identifier regardless of struct renames.
func (PostureEvaluation) TableName() string { return "posture_evaluations" }

// EvalRecorder is the write-side hook that validatePostureChecksOnPeer
// calls after each check.Check() invocation. The implementation is
// expected to be non-blocking (channel send or similar) — the eval
// hot path runs O(peers * policies * checks) per Sync and cannot
// afford a synchronous DB round-trip.
//
// Nil-safe by convention: callers should accept that no recorder
// means "don't record" and skip the hook.
type EvalRecorder interface {
	Record(ctx context.Context, e PostureEvaluation)
}

// EvalStore persists PostureEvaluation rows and answers the
// per-peer-timeline query the dashboard makes. Implemented by the
// GORM-backed store in evaluation_store.go; tests can inject an
// in-memory fake.
type EvalStore interface {
	// Insert writes a batch in a single statement. The store is
	// expected to swallow individual row errors at the persistence
	// layer (best-effort) — failing the eval pipeline on a logging
	// hiccup is the wrong trade-off.
	Insert(ctx context.Context, batch []PostureEvaluation) error

	// ListForPeer returns the most recent `limit` evaluations for
	// the given peer in the given account, newest first.
	ListForPeer(ctx context.Context, accountID, peerID string, limit int) ([]PostureEvaluation, error)

	// PurgeOlderThan deletes rows whose EvaluatedAt is older than
	// the cutoff. Returns the number of rows removed for telemetry.
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// MigrateEvaluationTable creates the posture_evaluations table and
// its compound index if they don't exist yet. Called from the same
// init path as the other GORM auto-migrations.
func MigrateEvaluationTable(db *gorm.DB) error {
	return db.AutoMigrate(&PostureEvaluation{})
}
