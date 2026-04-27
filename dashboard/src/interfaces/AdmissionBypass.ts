// AdmissionBypass mirrors the wire shape returned by
// /api/peers/{id}/admission-bypass and /api/admin/admission-bypasses.
// Bypass grants are time-bounded overrides for the Device Admission
// gate — see ADR-0004 for the rationale + audit-trail contract.
export interface AdmissionBypass {
  id: number;
  peer_id: string;
  initiator_id: string;
  reason: string;
  granted_at: string; // RFC3339
  expires_at: string; // RFC3339
  active: boolean;
}

// AdmissionBypassInput is the create body. Either ExpiresInSeconds
// (relative, recommended — survives client/server clock skew) or
// ExpiresAt (absolute RFC3339) is required. Reason is mandatory.
// Server-side validation rejects no-expiry grants and durations
// over 30 days.
export interface AdmissionBypassInput {
  reason: string;
  expires_in_seconds?: number;
  expires_at?: string;
}
