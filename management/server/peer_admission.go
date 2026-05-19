package server

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/admission"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// evaluateAdmission runs the account-wide admission posture checks
// against peer and refuses login when any of them rejects it.
//
// Two short-circuits run BEFORE the posture checks. Both are
// declarative gates added by ADR-0004 to keep the admission gate
// usable in real deployments:
//
//  1. Group-scope exemption — if the peer's groups intersect
//     `Settings.AdmissionExemptGroups`, the check is skipped.
//     Motivating case: gateway / routing peers (cloud VMs, K8s
//     pods, on-prem servers) that are part of the mesh but never
//     enroll in MDM/EDR, so a posture check would always fail.
//     Exempting their group is a one-time declarative change
//     audited via AdmissionExemptGroupsUpdated.
//
//  2. Per-peer bypass — if there's an active, non-expired
//     PeerAdmissionBypass row for (accountID, peerID), the
//     check is skipped. Bypass is the break-glass for individual
//     non-compliant devices that need temporary access (CEO
//     laptop with a 24h reason). The grant emitted its own
//     audit event when it was issued; we just honor it here.
//
// candidateGroups is consulted only when peer.ID is empty (the
// AddPeer flow, before the peer is persisted). For Login / Sync
// the function resolves the actual group memberships from the
// supplied transaction.
func (am *DefaultAccountManager) evaluateAdmission(
	ctx context.Context,
	transaction store.Store,
	accountID string,
	peer *nbpeer.Peer,
	candidateGroups []string,
	initiatorID string,
) error {
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

	peerID := peer.ID
	if peerID == "" {
		peerID = peer.Key
	}

	// Short-circuit 1: group-scope exemption.
	if len(settings.AdmissionExemptGroups) > 0 {
		groups, err := am.resolveAdmissionPeerGroups(ctx, transaction, accountID, peer.ID, candidateGroups)
		if err != nil {
			return fmt.Errorf("admission: failed to resolve peer groups: %w", err)
		}
		if admission.HasGroupOverlap(groups, settings.AdmissionExemptGroups) {
			log.WithContext(ctx).Debugf(
				"admission: peer %s exempt by group membership", peerID)
			return nil
		}
	}

	// Short-circuit 2: active per-peer bypass.
	if am.admissionBypasses != nil && peer.ID != "" {
		active, row, err := am.admissionBypasses.IsActive(ctx, accountID, peer.ID)
		if err != nil {
			// Don't fail-open silently on a DB hiccup; log and
			// continue to the posture checks. If the operator
			// expects bypass to take effect and the DB is broken,
			// the right behavior is "deny" (fail-closed) plus a
			// loud log so the on-call sees the actual problem.
			log.WithContext(ctx).Errorf(
				"admission: bypass lookup failed for peer %s: %v",
				peerID, err)
		} else if active && row != nil {
			log.WithContext(ctx).Infof(
				"admission: peer %s bypassed by %s (reason=%q expires=%s)",
				peerID, row.InitiatorID, row.Reason, row.ExpiresAt.Format("2006-01-02T15:04:05Z"))
			return nil
		}
	}

	checks, err := transaction.GetPostureChecksByIDs(ctx, store.LockingStrengthShare, accountID, ids)
	if err != nil {
		return fmt.Errorf("admission: failed to load posture checks: %w", err)
	}

	denial := types.EvaluateAdmission(ctx, peer, ids, checks)
	if denial == nil {
		return nil
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

// resolveAdmissionPeerGroups returns the peer's group memberships as
// the admission gate sees them. For an existing peer (peer.ID
// non-empty) this is a transaction-scoped DB lookup. For a new peer
// (AddPeer flow) it falls back to the candidate groups computed
// from the SetupKey.AutoGroups or User.AutoGroups before the peer
// is persisted.
func (am *DefaultAccountManager) resolveAdmissionPeerGroups(
	ctx context.Context,
	transaction store.Store,
	accountID, peerID string,
	candidateGroups []string,
) ([]string, error) {
	if peerID == "" {
		return candidateGroups, nil
	}
	return getPeerGroupIDs(ctx, transaction, accountID, peerID)
}
