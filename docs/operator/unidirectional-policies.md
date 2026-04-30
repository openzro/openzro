# Unidirectional `Protocol=ALL` access policies

> **Experimental** until validated in lab against iptables, uspfilter, and
> native firewalls. The behavior described below is what the data plane
> *should* enforce based on the firewall-rule compiler's design and the
> stateful-conntrack assumptions that already work for protocol-specific
> rules. Lab signoff before relying on it for compliance-grade access
> control.

## What it is

By default, an access policy with `Protocol=ALL` allows traffic in both
directions between the source and destination groups. That's the safest
permissive choice and matches NetBird upstream.

openZro additionally supports **unidirectional** ALL policies: only the
configured `Sources → Destinations` direction is allowed to *initiate*
flows. Reply traffic on established flows still passes (stateful
conntrack), but unsolicited connections from `Destinations` back to
`Sources` are dropped.

This closes the lateral-movement gap NetBird tracks in
[netbirdio/netbird#3547](https://github.com/netbirdio/netbird/issues/3547):
if a peer in the destination group is compromised, the attacker cannot
pivot back into the source network using the same policy.

See [ADR-0010](../adr/0010-unidirectional-all-policies.md) for design
rationale and validation plan.

## When to use it

Good fits:

- **Workstations → servers.** Office laptops should be able to reach
  internal HTTP/SSH/database servers. Servers should *not* be able to
  open new connections back to laptops.
- **Bastion → infrastructure.** Jump host → control planes. Control
  planes should reply to commands but not push outbound to the bastion
  unprompted.
- **Reverse-proxy ingress.** Public proxy → internal services. Services
  should respond to proxied requests but not initiate to the proxy on
  their own.

Avoid for:

- **Bidirectional protocols by design.** Anything that needs the
  destination to push notifications, telemetry, or keep-alives back to
  the source unprompted: SNMP traps, syslog over UDP outbound, server-
  initiated heartbeats, some IoT telemetry shapes. Use bidirectional
  rules or split into two unidirectional rules with explicit groups.
- **ICMP-heavy operations.** Echo request/reply works (conntrack
  handles the identifier session). But unsolicited ICMP from the
  destination — fragmentation needed, destination unreachable, time
  exceeded — will be dropped. If your topology relies on path-MTU
  discovery or detailed ICMP error feedback from the destination side,
  keep ALL bidirectional or use a separate ICMP-bidirectional rule.

## How to enable

### Dashboard

1. **Access Control** → click an existing policy or **Create new**.
2. Set **Protocol** to `All`.
3. Click the direction toggle next to the source/destination groups.
   The default is bidirectional (both arrows green); click once to
   switch to source-only (top arrow blue, bottom gray).
4. Save. The policy persists with `bidirectional=false`.

### REST API

```bash
curl -X POST https://your-management/api/policies \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "workstations to servers",
    "enabled": true,
    "rules": [{
      "name": "ws-to-srv",
      "enabled": true,
      "sources": ["group-workstations"],
      "destinations": ["group-servers"],
      "protocol": "all",
      "bidirectional": false,
      "action": "accept"
    }]
  }'
```

### `openzro-cli`

Not yet wired through the CLI. Use the API or dashboard for now.

## What changes on the peers

Each peer evaluates the access policy locally; the management server
sends only the firewall rules that apply to that peer. With
`bidirectional=false`:

| Peer side | Forward direction | Reverse direction |
|---|---|---|
| Source peer | egress rule installed | no ingress rule (drop) |
| Destination peer | ingress rule installed | no egress rule for this policy |

Reply traffic for an established flow rides the stateful conntrack
table the same way it does for protocol-specific TCP/UDP rules. Drops
on the reverse direction are logged at the client at log level
`debug` (search the client logs for `dropped: rule unidirectional, no
reverse path` once you turn the policy on).

## Auditing

Flow exports (`OPENZRO_FLOW_STORE_ENGINE` ≠ `none`) record the
dropped attempt with `event=drop, direction=reverse, rule=<policy-id>`.
If you stream to a SIEM, filter on these to spot whether your apps
behave as you expect — a flood of reverse drops likely means an
unexpected protocol shape, not an attack, but worth investigating.

## Gotchas

- **Existing bidirectional policies don't auto-flip.** Switching a
  policy from bidirectional to unidirectional is an explicit operator
  action. Past audit-log entries reflect the policy state at the time
  it was applied.
- **Pre-v0.20 imports.** Operators upgrading from filestore (very
  old installs) used to have an automatic coerce that re-flipped
  bidirectional on every server start. That was removed in
  v0.53.1-alpha.16. After the upgrade, the operator's chosen
  `Bidirectional` value is honored.
- **Resource policies.** When the destination is a
  `network resource` (NetBird's "resource" concept) instead of a peer
  group, the dashboard hides the bidirectional toggle and forces
  outgoing-only — that path is unidirectional by design and unaffected
  by this change.
- **Default-account policy stays bidirectional.** Every new account
  still gets a default ALL-to-ALL bidirectional rule, the same as
  upstream. Edit it after creation if you don't want the default.

## Verifying it works

After turning a unidirectional policy on, from the destination peer:

```bash
# Expect: timeout / connection refused
nc -zv <source-peer-ip> 22
```

From the source peer:

```bash
# Expect: connected
nc -zv <destination-peer-ip> 22
```

If either expectation flips, file an issue against
[`openzro/openzro`](https://github.com/openzro/openzro/issues) with the
client and management versions and the policy JSON.
