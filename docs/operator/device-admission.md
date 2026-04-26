# Operator guide — Device Admission

This guide walks an operator through enabling Device Admission end-
to-end: configure an MDM/EDR provider, build a posture check that
references it, turn on the admission gate, observe a denial, and
hand a CSV to the auditor.

The architecture and rationale live in
[ADR-0003](../adr/0003-device-admission-gate.md). This file is
how-to.

## Prerequisites

- Management running and reachable on `:33071/api`. `make
  dev.management.up` for local development, or your production
  deployment.
- A working IdP (the dashboard you log into for the steps below).
- An MDM/EDR vendor account with API credentials. See the
  [MDM/EDR integration guide](mdm-edr-integrations.md) for the per-
  vendor setup that produces the credentials you'll paste here.

## Step 1 — Configure the MDM/EDR provider

Settings → Integrations → MDM / EDR → **Add provider**.

| Field | Notes |
|---|---|
| Name | Free-form, shows up in the posture-check picker |
| Type | `Microsoft Intune`, `SentinelOne`, or `Huntress` |
| Type-specific credentials | See the [MDM/EDR integration guide](mdm-edr-integrations.md) |

Save. The credentials are encrypted at rest with the management's
`DataStoreEncryptionKey`. The dashboard never reads them back —
edits send `""` to mean "unchanged"; re-paste only if you are
rotating.

A green status means the provider was configured. The provider does
not actively probe at save time; the first real lookup happens when
a peer is evaluated. To smoke-test the credentials right now:

```bash
curl -fsS -H "Authorization: Bearer <PAT>" \
  http://localhost:33071/api/admin/mdm-providers/<id>
```

A 200 with the public projection of the config means the row is
saved. Failures only surface in the management log when the first
peer triggers a lookup.

## Step 2 — Create the Endpoint Security posture check

Posture Checks → **Add posture check**.

In the modal, scroll past the five built-in check types (NB version,
OS version, geo, network range, process) to **Endpoint Security
(MDM/EDR)** and click **Configure**.

| Field | Notes |
|---|---|
| Provider | Pick the one you saved in Step 1 |
| Fail open | Default off. When on, vendor lookup failures (timeout, vendor outage, device not found) are treated as *compliant*. Use only if availability matters more than fail-closed. |

Save the modal, then save the posture check itself with a
recognisable name (e.g. `intune-compliant`).

The check is now usable in two places: as the source posture check
of any policy (traffic-only filtering, current behavior) and as part
of the admission list (control-plane refusal, the new behavior).

## Step 3 — Add the check to the admission list (don't enforce yet)

Settings → **Device Admission**. The page has three controls:

1. The **Enforce admission on Login & Sync** toggle.
2. A list of posture checks the operator has authored.
3. The **Audit CSV** button.

Tick the `intune-compliant` check **but leave the toggle off**. Save.

This is the soft-rollout step. The list is recorded but the gate is
not enforcing. We'll observe how many peers would fail before
flipping the toggle.

## Step 4 — Observe what would fail

There is no built-in dry-run mode. The recommended observability is
to attach the same posture check to one of your dashboard-only
policies (e.g. a "monitoring" policy that does not gate any real
traffic), then look at which peers vanish from the policy's source
group via the existing per-policy posture filtering. Anything that
disappears would have been refused by the admission gate.

Alternatively, run the smoke loop manually:

```bash
# Get the peer's hostname, then ask the manager what its admission
# status would be by curl-ing the MDM lookup directly.
curl -fsS -H "Authorization: Bearer <PAT>" \
  "http://localhost:33071/api/admin/mdm-providers/<id>"
# Then call the vendor API yourself with the peer's hostname.
```

Once the failure set is empty (or matches the operator's "these
devices are known-bad and should be blocked" target list):

## Step 5 — Flip the gate on

Settings → Device Admission → toggle **Enforce admission on Login &
Sync** → Save.

Three audit events fire on save:

```
account.setting.admission.enforcement.enable    initiator=<your-user>
account.setting.admission.checks.update         initiator=<your-user>  (if list changed too)
```

Within ~6 minutes (worker interval 60s + MDM cache TTL 5min + client
backoff ~30s), every connected peer that fails admission is closed
out of its session. New connection attempts are refused at Login.

The operator log now carries lines like:

```
admission denied for peer <peer-id>: EndpointSecurityCheck
  (intune-compliant): device not compliant per Microsoft Intune
admission revalidator: revoking session for peer <peer-id>:
  device admission denied: EndpointSecurityCheck:
  device not compliant per Microsoft Intune
```

The Activity log carries a `peer.admission.deny` row per refusal
with structured meta:

```json
{
  "posture_check_id":   "abc-123",
  "posture_check_name": "intune-compliant",
  "check_type":         "EndpointSecurityCheck",
  "reason":             "device not compliant per Microsoft Intune",
  "peer_hostname":      "alice-laptop"
}
```

## Step 6 — Auditor evidence package

Settings → Device Admission → **Audit CSV**.

The download is a CSV with stable columns:

```
timestamp, activity_code, activity, initiator_id, initiator_name,
initiator_email, target_id, posture_check_id, posture_check_name,
check_type, reason, peer_hostname
```

For a time-bounded export (quarterly review):

```
GET /api/events/admission.csv?from=2026-01-01T00:00:00Z&to=2026-04-01T00:00:00Z
```

with the user's bearer token. Same shape, time-windowed.

## Tuning

### Revocation latency vs vendor API load

Default is 60s revalidate interval + 5min MDM cache TTL ≈ 6min
worst case. The interval is settable via:

```
OPENZRO_ADMISSION_REVALIDATE_INTERVAL_SECONDS=N
```

- `0` disables the worker entirely (Phase 2 off; only the Phase 1
  Login/Sync gate fires — works for compliance audits where
  "compromised device loses access immediately on next sync" is
  acceptable).
- `60` (default) — 1 min check cadence.
- `30` is the practical floor; `10` is the absolute floor enforced
  by the worker code to protect vendor APIs from a stampede.

To shorten the cache window, edit `mdm.statusCache` TTL (currently
hard-coded at 5 min). Only do this if you have a real reason — the
vendor APIs are expensive and a 5min cache is the difference between
"fine" and "rate limited".

### Fail-Open

Per posture-check, not global. The default is fail-closed: vendor
lookup failure → access denied. Flip it on a specific
EndpointSecurityCheck if availability matters more than strict
posture. Practical guidance:

- **Bacen-regulated tenants**: leave fail-closed. The auditor will
  ask why an unreachable vendor lets non-compliant devices through.
- **Enterprise IT (non-regulated)**: fail-open is reasonable. A
  vendor outage that takes the whole user base offline is often
  worse than a brief compliance gap.
- **Critical infrastructure**: leave fail-closed and have a
  fall-back authentication path the operator manually re-enables
  during a vendor incident.

## Common operational scenarios

### "I need to let one specific peer in even though it's failing"

Either fix the device at the vendor side or remove the relevant
posture check from the admission list. There is **no per-peer
override** — that's a deliberate choice (see ADR-0003 §Consequences).

The fastest path: the auditor goes to the vendor (Intune / S1 /
Huntress) and either marks the device compliant or removes the
non-compliance flag. Within ~6 min the peer is back online.

### "I need to roll out admission gradually"

The recommended sequence:

1. Configure MDM provider (Step 1).
2. Build the posture check (Step 2).
3. Attach the posture check to a single test **policy** (not the
   admission list yet). Watch which peers disappear from that
   policy's source group.
4. When the disappearing set is empty (or matches the expected
   "known-bad" set), add the check to the **admission list** but
   leave enforcement off (Step 3).
5. Wait one Sync window — peers that would fail show up in the
   activity log even though they're not yet refused (search for
   debug logs like `admission denied for peer ...`).
6. Flip the toggle when the operator is satisfied (Step 5).

### "The vendor went down and now everyone is locked out"

Two options:

1. **Quick remediation**: flip the **Enforce admission** toggle off
   in the dashboard. Audit event records the toggle flip with the
   operator's identity. Re-enable when the vendor recovers.
2. **Per-check FailOpen**: pre-emptively enable FailOpen on the
   EndpointSecurityCheck so vendor outages don't lock anyone out.
   Trade-off documented above.

### "I need to know which peers were refused last quarter"

```
GET /api/events/admission.csv?from=2026-01-01T00:00:00Z&to=2026-04-01T00:00:00Z
```

Open in Excel / pandas / your tool of choice. Group by
`peer_hostname` to count per-device refusals; group by
`posture_check_name` to see which check fires most.
