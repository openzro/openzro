package server

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/activity"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// evaluateAdmission runs the account-wide admission posture checks
// against peer and refuses login when any of them rejects it.
//
// Reads settings + the configured posture checks from the supplied
// transaction so the caller stays within a single locking scope. Runs
// the check.Check() call out of band of any DB lock — the only
// in-transaction work is metadata I/O. The evaluation itself can do
// network I/O (e.g. EndpointSecurityCheck → Microsoft Graph), so
// callers must invoke this BEFORE acquiring write locks they intend
// to hold for long.
//
// Returns nil when the peer is admitted (or admission is disabled).
// Returns a status.PermissionDenied error when a check rejects the
// peer; the message is structured as
// "device admission denied: <CheckType>: <Reason>" so client UIs and
// SIEM rules can pattern-match.
//
// Side effect: on denial, an activity.PeerAdmissionDenied event is
// stored, including the failing posture check ID and the reason. This
// is the audit trail required by Bacen 4.893 / Circular 3.909.
func (am *DefaultAccountManager) evaluateAdmission(ctx context.Context, transaction store.Store, accountID string, peer *nbpeer.Peer, initiatorID string) error {
	settings, err := transaction.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return err
	}
	if settings == nil || !settings.AdmissionEnforcementEnabled {
		return nil
	}
	ids := settings.AdmissionPostureChecks
	if len(ids) == 0 {
		return nil
	}

	checks, err := transaction.GetPostureChecksByIDs(ctx, store.LockingStrengthShare, accountID, ids)
	if err != nil {
		return fmt.Errorf("admission: failed to load posture checks: %w", err)
	}

	denial := types.EvaluateAdmission(ctx, peer, ids, checks)
	if denial == nil {
		return nil
	}

	peerID := peer.ID
	if peerID == "" {
		peerID = peer.Key
	}
	log.WithContext(ctx).Warnf("admission denied for peer %s: %s (%s) %s",
		peerID, denial.CheckType, denial.PostureCheckName, denial.Reason)

	if initiatorID == "" {
		initiatorID = peerID
	}
	am.StoreEvent(ctx, initiatorID, peerID, accountID, activity.PeerAdmissionDenied, map[string]any{
		"posture_check_id":   denial.PostureCheckID,
		"posture_check_name": denial.PostureCheckName,
		"check_type":         denial.CheckType,
		"reason":             denial.Reason,
		"peer_hostname":      peer.Meta.Hostname,
	})

	return status.Errorf(status.PermissionDenied,
		"device admission denied: %s: %s", denial.CheckType, denial.Reason)
}
