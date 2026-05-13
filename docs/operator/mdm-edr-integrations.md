# Operator guide — MDM/EDR integrations

openZro can talk to four MDM/EDR vendors out of the box: **Microsoft
Intune**, **SentinelOne**, **Huntress**, and **CrowdStrike Falcon**.
Each integration is the backing for the Posture Check type *Endpoint
Security (MDM/EDR)*, which in turn is what Device Admission uses to
refuse non-compliant devices.

This file is the per-vendor "what credentials do I need and what
permissions do they need" walkthrough. After this you go to
[Device Admission](device-admission.md) to wire the check into the
admission gate.

## How the lookup works (so you can debug it)

1. Peer connects → management calls posture check evaluation.
2. The `EndpointSecurityCheck` reads the peer's hostname (from
   `peer.Meta.Hostname`, which the client agent reports at sync
   time) and asks the configured MDM/EDR provider "is this hostname
   compliant?"
3. Each provider implementation lives in
   [`management/server/mdm/`](../../management/server/mdm/) — one
   file per vendor, all small (~150 lines).
4. Result is cached for **5 minutes** keyed by `(provider_id,
   hostname)`. Errors are NOT cached — a transient vendor outage
   does not poison the cache.
5. On compliant: peer passes. On non-compliant: structured reason
   like `"device not compliant per Microsoft Intune"`. On vendor
   error: depends on `FailOpen` — closed by default, open if the
   operator flipped it on the posture check.

The hostname is the peer's identity for this lookup. It must match
what the vendor sees. Most environments enroll devices with the
machine's hostname, which is what the openZro client reports — so it
"just works" in practice. If your environment uses a different
identifier (asset tag, serial number), open a feature request — the
identifier resolution lives in `deviceIdentifierFromPeer` and is
intentionally a single function for future extensions.

## Microsoft Intune

### What you need

- An Entra ID (Azure AD) tenant.
- An app registration with Microsoft Graph permission
  `DeviceManagementManagedDevices.Read.All` (Application permission,
  not Delegated). Granted admin consent.
- A client secret on that app registration.
- The tenant ID, the client ID, and the client secret value.

### Walkthrough

1. **Entra portal** → App registrations → **New registration**.
   Name it `openzro-posture` or similar. Single tenant. No redirect
   URI — this is a confidential client doing OAuth client_credentials.
2. **Certificates & secrets** → **New client secret**. Copy the
   *Value* immediately (Entra hides it on the next page load). Set
   the expiry per your secret-rotation policy.
3. **API permissions** → **Add a permission** → **Microsoft Graph**
   → **Application permissions** → search for
   `DeviceManagementManagedDevices.Read.All` → Add.
4. Click **Grant admin consent for <tenant>**.
5. **Overview** tab — copy the *Application (client) ID* and the
   *Directory (tenant) ID*.

### Configuration in openZro

Settings → Integrations → MDM/EDR → **Add provider** → type
**Microsoft Intune**:

| Field | What to paste |
|---|---|
| Name | Free-form label, e.g. `Intune Prod` |
| Tenant ID | Directory (tenant) ID from step 5 |
| Client ID | Application (client) ID from step 5 |
| Client Secret | The Value from step 2 |
| Authority (optional) | Defaults to `https://login.microsoftonline.com/<tenant>`. Override only for sovereign clouds (US Gov: `https://login.microsoftonline.us`, China: `https://login.partner.microsoftonline.cn`). |
| Strict compliance (optional) | Default `off`: `inGracePeriod` is treated as compliant. Flip `on` to drop peers off the network the moment Intune flags them, even before the grace window expires. |

Save. The first peer evaluation will trigger the Graph call.

### What openZro queries

The driver issues up to three Graph queries per device lookup, in
this order, stopping at the first hit. The `$select` projection is
pinned on every call to keep responses small.

1. **Combined filter** (only when both the peer's hostname and the
   registering user's email are known):
   ```http
   GET /v1.0/deviceManagement/managedDevices
     ?$filter=deviceName eq '<hostname>' and userPrincipalName eq '<email>'
     &$select=id,deviceName,complianceState,managementState,operatingSystem,osVersion,userPrincipalName
   ```
   Disambiguates the case where the same hostname appears on multiple
   users' enrolled devices (renames, hand-me-down laptops, shared
   hardware).

2. **userPrincipalName-only fallback** (only when the user email is
   known): `?$filter=userPrincipalName eq '<email>'`. Recovers the
   renamed-hostname case — the agent reports a new hostname but
   Intune still has the device under the old one; the user email is
   the stable anchor.

3. **deviceName-only fallback**: `?$filter=deviceName eq '<hostname>'`.
   Used when the peer was registered via a setup key with no user
   attribution, and as a last-resort safety net.

The response's `complianceState` field drives the result:

| Graph value | openZro decision (default) | Strict compliance ON |
|---|---|---|
| `compliant` | ✅ pass | ✅ pass |
| `inGracePeriod` | ✅ pass (Intune's "compliant within grace window") | ❌ fail |
| `noncompliant` | ❌ fail, reason `"device not compliant per Microsoft Intune"` | ❌ fail |
| `unknown`, `error`, `conflict` | ❌ fail, reason includes the state | ❌ fail |
| missing device (404) | ❌ fail, reason `"device not enrolled in Intune"` | ❌ fail |

### Common Intune issues

- **403 forbidden** → permission `DeviceManagementManagedDevices.Read.All`
  is not Application-scoped or admin consent was not granted. The
  error from the server names the missing permission verbatim.
- **No matching device** → hostname mismatch between the openZro
  agent and Intune. Confirm `hostnamectl` (Linux) / `Get-ComputerInfo`
  (Windows) matches what shows up in Intune's *Devices* list.

## SentinelOne

### What you need

- The SentinelOne Management Console URL (e.g.
  `https://yourorg.sentinelone.net`).
- An API token with **Viewer** scope (read-only is sufficient — we
  only call `GET /agents`). Generate under Settings → Users → API
  Tokens → New.

### Configuration in openZro

| Field | What to paste |
|---|---|
| Name | Free-form |
| Management URL | Full URL, no trailing slash |
| API Token | The token. SentinelOne shows it once at creation. |

### What openZro queries

```http
GET /web/api/v2.1/agents?computerName=<hostname>
Authorization: ApiToken <token>
```

The agent record's fields drive the decision:

| Condition | Decision |
|---|---|
| `isActive: true`, `infected: false`, `isDecommissioned: false` | ✅ pass |
| `infected: true` | ❌ fail, reason `"agent reports infection"` |
| `isActive: false` | ❌ fail, reason `"agent not active (last seen <timestamp>)"` |
| `isDecommissioned: true` | ❌ fail, reason `"agent decommissioned"` |
| no agent matches | ❌ fail, reason `"no SentinelOne agent for hostname <h>"` |

### Common SentinelOne issues

- **401 Unauthorized** → token expired (SentinelOne tokens have a
  configurable expiry — default 6 months) or revoked.
- **Hostname format** → SentinelOne stores hostname as the agent
  reports it. Mac agents report `hostname.local`, Linux agents
  report short hostname or FQDN depending on the distro. Verify in
  the SentinelOne console first.

## Huntress

### What you need

- A Huntress account.
- An API key + API secret pair from Settings → API Credentials.

### Configuration in openZro

| Field | What to paste |
|---|---|
| Name | Free-form |
| API Key | From Huntress |
| API Secret | From Huntress |

### What openZro queries

```http
GET /v1/agents?hostname=<hostname>
Authorization: Basic <base64(apikey:apisecret)>
```

Decision:

| Condition | Decision |
|---|---|
| Agent present, no incident reports, agent version current | ✅ pass |
| Open incident report on the device | ❌ fail, reason `"open incident: <count>"` |
| Agent outdated | ❌ fail, reason includes the version delta |
| no agent matches | ❌ fail, reason `"no Huntress agent for hostname <h>"` |

### Common Huntress issues

- **403** → wrong key/secret pair.
- **Hostname format** → Huntress reports the OS-level hostname; if
  your endpoints use FQDN-style hostnames internally, ensure the
  Huntress agent picks the same one.

## CrowdStrike Falcon

### What you need

- A CrowdStrike Falcon tenant. Note the **cloud region** — Falcon
  tenants live in one of `us-1`, `us-2`, `eu-1`, `us-gov-1`, or
  `us-gov-2`, and an OAuth client minted in one cloud only works
  against its home region.
- A Falcon API client (Console → Support → **API Clients and Keys**)
  with the **Hosts: Read** scope. Read-only is sufficient — we only
  call the device query and entities endpoints.
- The client ID + client secret pair from that API client.

### Walkthrough

1. **Falcon console** → Support → **API Clients and Keys** → **Add
   new API client**.
2. Name it `openzro-posture` or similar. Tick the **Hosts: Read**
   scope only.
3. **Save**. The console shows the *Client ID* and *Client Secret*
   exactly once — copy both.
4. Note the cloud region (top-right of the console URL —
   `falcon.crowdstrike.com` is us-1, `falcon.us-2.crowdstrike.com`
   is us-2, `falcon.eu-1.crowdstrike.com` is eu-1).

### Configuration in openZro

Settings → Integrations → MDM/EDR → **Add provider** → type
**CrowdStrike Falcon**:

| Field | What to paste |
|---|---|
| Name | Free-form, e.g. `Falcon Prod` |
| Cloud | Pick your tenant's region |
| API Client ID | From step 3 |
| API Client Secret | From step 3 |

### What openZro queries

Two-step lookup:

```http
GET /devices/queries/devices/v1?filter=hostname:'<hostname>'
Authorization: Bearer <oauth-token>
```

returns a list of Falcon AIDs (agent IDs); the first match feeds
into:

```http
GET /devices/entities/devices/v2?ids=<aid>
Authorization: Bearer <oauth-token>
```

The device record's `status` and `reduced_functionality_mode`
fields drive the decision:

| Condition | Decision |
|---|---|
| `status: "normal"`, `reduced_functionality_mode: "no"` | ✅ pass |
| `status: "contained"` | ❌ fail, reason `"host is contained (network isolation active)"` |
| `status: "containment_pending"` / `"lift_containment_pending"` | ❌ fail, reason names the transition |
| `reduced_functionality_mode != "no"` | ❌ fail, reason `"sensor in reduced_functionality_mode=…"` |
| `status` is anything else | ❌ fail, reason `"Falcon status=… (expected 'normal')"` |
| no AID matches the hostname | ❌ fail, reason `"no Falcon sensor registered"` |

Containment is treated as non-compliant on purpose — Falcon
containment is the security team's "isolate this host now" action,
and an isolated host should not be admitted into the network until
the containment is lifted by the SOC.

### Common CrowdStrike issues

- **401 Unauthorized** → the API client is from a different cloud
  than what you configured. The error names this hint explicitly.
- **403 Forbidden** → API client lacks **Hosts: Read** scope.
- **No matching device** → hostname mismatch. The Falcon sensor
  reports the OS-level hostname; verify in the *Hosts → Host
  Management* view first.

## Verifying end-to-end

After configuring the provider, before flipping the Device Admission
toggle:

```bash
# 1. Confirm the provider row exists.
curl -fsS -H "Authorization: Bearer <PAT>" \
  http://localhost:33071/api/admin/mdm-providers | jq

# 2. Build a posture check pointing at the provider via the dashboard.

# 3. Attach the posture check to a test policy (Source Posture Checks).
#    Connect a test peer and watch the Network Traffic dashboard —
#    a non-compliant peer disappears from the policy's source group
#    silently.

# 4. Look at the management log for the actual lookup:
#    `make dev.management.logs` and grep for the provider type.
```

Once verified, follow the [Device Admission guide](device-admission.md)
to wire the same posture check into the admission list.

## Adding another vendor

Each vendor lives in one ~150-line file under `management/server/mdm/`
implementing the `Provider` interface:

```go
type Provider interface {
    Name() string
    Type() ProviderType
    Lookup(ctx context.Context, deviceID string) (DeviceStatus, error)
    Close() error
}
```

`DeviceStatus` carries `{Compliant, Reason}`. New vendors:

1. Add the type constant + ID to
   [`management/server/mdm/provider.go`](../../management/server/mdm/provider.go).
2. Add a config struct + public projection to
   [`management/server/mdm/model.go`](../../management/server/mdm/model.go).
3. Implement the `Provider` interface in `management/server/mdm/<vendor>.go`
   and register it in the `Manager.build` switch.
4. Update the dashboard's `MDMProviderModal` with the new type
   option and per-type credential fields.
5. Add a test that hits `httptest` mocks of the vendor API — no live
   tenant required.

Tanium, Jamf, and Kandji are reasonable next candidates. Open an
issue first so we can scope the credential surface and the
compliance attribute to use.
