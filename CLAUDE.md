# Openzro · Brand instructions for Claude Code

You are working on **Openzro**, an open-source zero-trust mesh networking
project (fork of NetBird). Follow the brand and design rules below for
every UI you build — landing pages, dashboards, CLI output, READMEs, etc.

---

## Brand at a glance

- **Name (always):** `openZro` — capital Z in the middle, the rest lowercase.
  Never `Openzro`, `OpenZro`, `OPENZRO`, or `openzro` unless inside a URL,
  package name, or shell command.
- **Icon:** `brand/openzro-icon.svg` — solid violet disc with a fused O+Z
  glyph inside (the disc IS the O; two white sails define the Z). Always
  use this file; do not redraw it inline.
- **Tone:** technical, modern, friendly. Not enterprise-stuffy, not
  cyberpunk-edgy. Think Vercel × Tailscale.

---

## Wordmark rules

- Sans: **Geist** (`font-weight: 600`).
- The middle **Z** is set in `font-weight: 700` and `color: var(--oz-violet-600)`
  on light backgrounds, `var(--oz-violet-400)` on dark. This visually
  echoes the icon's accent. Always render the wordmark as:
  ```html
  <span class="oz-wordmark">open<span class="oz-z">Z</span>ro</span>
  ```
- Letter-spacing: `-0.025em`. Line-height: `1`.
- Minimum size: 14px. Below that, use the icon alone.

---

## Color tokens

Drop this `:root` block into your global stylesheet. Use the semantic
aliases (e.g. `--oz-primary`) wherever possible — they keep theming
swappable.

```css
:root {
  /* Violet scale */
  --oz-violet-50:  #f5f3ff;
  --oz-violet-100: #ede9fe;
  --oz-violet-200: #ddd6fe;
  --oz-violet-300: #c4b5fd;
  --oz-violet-400: #a78bfa;
  --oz-violet-500: #8b5cf6;
  --oz-violet-600: #7c3aed;  /* primary */
  --oz-violet-700: #6d28d9;
  --oz-violet-800: #5b21b6;
  --oz-violet-900: #4c1d95;

  /* Ink (neutrals) */
  --oz-ink:    #0f0a1f;
  --oz-ink-2:  #1a1330;
  --oz-paper:  #faf9fc;

  /* Semantic aliases — prefer these */
  --oz-primary:       var(--oz-violet-600);
  --oz-primary-hover: var(--oz-violet-700);
  --oz-primary-soft:  var(--oz-violet-100);
  --oz-text:          var(--oz-ink);
  --oz-text-muted:    var(--oz-violet-700);
  --oz-bg:            #ffffff;
  --oz-bg-soft:       var(--oz-violet-50);
  --oz-bg-dark:       var(--oz-ink);
  --oz-border:        var(--oz-violet-200);

  /* Type */
  --oz-sans: 'Geist', -apple-system, system-ui, sans-serif;
  --oz-mono: 'JetBrains Mono', ui-monospace, monospace;
}
```

### Combinations that are known-good

| Surface           | Background        | Text                 | Accent              |
| ----------------- | ----------------- | -------------------- | ------------------- |
| Page (light)      | `#fff`            | `--oz-ink`           | `--oz-violet-600`   |
| Page (soft)       | `--oz-violet-50`  | `--oz-ink`           | `--oz-violet-600`   |
| Page (dark)       | `--oz-ink`        | `#fff`               | `--oz-violet-400`   |
| Card              | `#fff`            | `--oz-ink`           | border `--oz-violet-200` |
| Primary button    | `--oz-violet-600` | `#fff`               | hover `--oz-violet-700` |
| Code / terminal   | `--oz-ink`        | `--oz-violet-200`    | prompt `--oz-violet-400` |
| Link              | inherit           | `--oz-violet-600`    | hover underline      |

### Things to avoid

- ❌ Inventing new violets outside this scale.
- ❌ Mixing in other accent colors (no orange CTAs, no green checkmarks).
  Success/error states use violet variants + neutral grays.
- ❌ Glassmorphism, heavy gradients on body backgrounds, or neon glows.
  Subtle gradients are OK on the icon and on hero surfaces only.
- ❌ Drop shadows with non-violet tints. Use `rgba(76, 29, 149, X)`.

---

## Typography

```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600;700;800&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
```

- **Geist** for everything UI: headings, body, buttons, labels.
- **JetBrains Mono** for code, CLI output, technical labels (uppercase
  with `letter-spacing: 0.08em` for caps treatment).
- Body size: 16px. Line-height: 1.55.
- Headings: 600 weight, `letter-spacing: -0.02em`, line-height `1.1`.

---

## Components to reuse

If you create reusable components, name them with the `Oz` prefix
(`OzButton`, `OzCard`, `OzTerminal`, etc.) so they're searchable.

### Button (primary)
```css
.oz-btn {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 10px 18px; border-radius: 8px;
  background: var(--oz-primary); color: #fff;
  font-family: var(--oz-sans); font-weight: 600; font-size: 14px;
  border: 0; cursor: pointer;
  transition: background 0.15s;
}
.oz-btn:hover { background: var(--oz-primary-hover); }
.oz-btn:focus-visible { outline: 2px solid var(--oz-violet-500); outline-offset: 2px; }
```

### Wordmark
```css
.oz-wordmark {
  font-family: var(--oz-sans); font-weight: 600;
  letter-spacing: -0.025em; line-height: 1;
  color: var(--oz-text);
}
.oz-wordmark .oz-z { color: var(--oz-primary); font-weight: 700; }
.oz-wordmark--dark { color: #fff; }
.oz-wordmark--dark .oz-z { color: var(--oz-violet-400); }
```

### Lockup (icon + wordmark)
```html
<a class="oz-lockup" href="/">
  <img src="/brand/openzro-icon.svg" width="32" height="32" alt="">
  <span class="oz-wordmark">open<span class="oz-z">Z</span>ro</span>
</a>
```
```css
.oz-lockup { display: inline-flex; align-items: center; gap: 10px; text-decoration: none; }
.oz-lockup .oz-wordmark { font-size: 20px; }
```

---

## When in doubt (visual)

- Match the visual style of the assets shipped under `brand/`.
- Don't add extra ornamentation (icons, gradients, deacmetive SVGs)
  unless the user asks. Less is more.
- All UI copy uses the exact name `openZro` with capital Z.

---

# Engineering rules — Go core

These rules govern every Go file under `client/`, `management/`, `signal/`,
`relay/`, `cluster/`, `dns/`, `encryption/`, etc. Dashboard rules are in
[`dashboard/CLAUDE.md`](dashboard/CLAUDE.md).

## License posture (non-negotiable)

The whole tree is **BSD-3-Clause** and stays that way forever.

- The upstream `netbirdio/netbird` repo is **mixed-licensed today**.
  Only `management/`, `signal/`, `relay/` and `combined/` were
  relicensed to **AGPLv3** (each dir carries its own `LICENSE`).
  Everything else — `client/`, `iface/`, `dns/`, `formatter/`,
  shared libraries, etc. — **stays BSD-3-Clause** even on
  upstream `main`.
- That means:
  - **AGPL territory** — never copy/translate/port from upstream
    `management/`, `signal/`, `relay/`, `combined/`. For security
    backports there read CVE / GHSA / CWE prose, not the upstream
    patch. Reimplement clean-room. The commit message MUST cite
    the public sources used and confirm no AGPL diff was consulted.
  - **BSD-3 territory** — upstream `client/` and the rest stay
    license-compatible with our fork. You may consult and adapt
    code from there, attributing the change in the commit message
    when appropriate. The same care still applies to AGPL-licensed
    third-party Go modules: do not pull them in.
- Importing GPL or AGPL into openZro contaminates the binary and
  forces a relicense — see [ADR-0001 §3.1](docs/adr/0001-openzro-foundation.md).
- Examples of clean-room security backports against AGPL upstream
  paths: [0f956e72](https://github.com/openzro/openzro/commit/0f956e72),
  [3196cbbf](https://github.com/openzro/openzro/commit/3196cbbf),
  [c761e80f](https://github.com/openzro/openzro/commit/c761e80f).
- `LICENSE` and `AUTHORS` files (root, `dashboard/`, `signal/dispatcher/`)
  are preserved verbatim. Never edit upstream copyright lines.

## Test-driven development is the default

Every new behavior lands with a failing test that exercises it, written
**before** the implementation. The Makefile target `make test` runs the
whole Go test suite with the race detector and a coverage profile.

- Co-locate tests: `foo_test.go` next to `foo.go`, same package.
- Prefer **table-driven** tests for input/output mappings; one `t.Run`
  per case so failures point at the offending row.
- Touch real dependencies in integration tests, not mocks: see the
  Postgres / MySQL / Redis testcontainers already in `management/server/`.
  Mocks are reserved for external services we cannot stand up locally
  (e.g. Auth0 IdP).
- A bug fix lands as a **regression test that fails on `main`** plus the
  fix that turns it green. The test stays in the tree forever.
- Coverage threshold for `make coverage` is configurable via
  `COVERAGE_MIN`. The current floor is intentionally low; raise it as
  the codebase matures rather than relaxing it.

## Code style

| Rule | Why |
|---|---|
| `gofmt -s -w .` clean — `make fmt.check` blocks unformatted code in CI | one canonical layout |
| `goimports` for import grouping (std → third-party → openzro internal, blank lines between groups) | readable diffs |
| **Functions ≤ ~80 lines** of body. Split when longer | reviewability |
| **Files ≤ ~500 lines**. Split when longer; `foo.go` + `foo_validate.go` is fine | navigation |
| **Lines ≤ 120 chars** soft cap; break long arg lists vertically | side-by-side diffs |
| Package names: short, lowercase, no underscores or camelCase | Go convention |
| Exported identifiers carry doc comments; the comment starts with the identifier name | `go doc` and `pkg.go.dev` need this |
| Errors: `fmt.Errorf("operation: %w", err)` — always wrap, always state context | preserves the chain |
| Sentinel errors are `var ErrFoo = errors.New("...")`; check with `errors.Is`, never string compare | upgrade-safe |
| **No `panic` outside of `init`** (and even there, only for unrecoverable misconfiguration) | a server crashing on a bad input is not OK |
| **Concurrent code uses contexts**, not bare goroutines that leak. Every long-running goroutine takes a `context.Context` and exits when it's done | leak-proof |
| Channel direction in signatures: `<-chan T` for read, `chan<- T` for write, when the function only does one or the other | self-documenting |
| Prefer **`any` over `interface{}`** since Go 1.18 | matches the rest of the std lib |
| **`time.Duration`** for durations, not `int seconds` | type safety |
| **Avoid premature interfaces** — write the concrete type first, extract an interface at the *call site* when a second implementation arrives | YAGNI |

## What not to introduce

- **No new external dependencies** without a license check (Apache 2.0,
  BSD, MIT only). See `go.mod`'s existing pattern.
- **No build tags except `//go:build linux`-style platform gates.**
  Feature flags belong in the runtime config, not in compile-time tags.
- **No reflection in hot paths.** Marshalling and ID generation are fine;
  per-request reflection is not.
- **No global mutable state** beyond what the `var blah = resolveFromEnv()`
  pattern in `cluster/factory` and `management/server/updatechannel.go`
  already does. Anything else goes in a struct that the caller owns.

## Logging

- Use the project's `log` (logrus). One log line per event.
- `log.WithContext(ctx).Errorf` / `Warnf` / `Infof` / `Debugf` —
  pick the right level, deliberately.
- **Sensitive values never go in logs.** Tokens, passwords, full peer
  keys: redact or omit. Peer IDs are OK (they're identifiers, not
  secrets).
- Drop reasons on hot paths log at `Errorf` so they show up by default
  (the existing `dropped update for peer …: channel full` line is the
  template).

## Concurrency

- Locks are **distributed** when state is shared across instances. See
  `cluster.Coordinator.Lock` in [`cluster/coordinator.go`](cluster/coordinator.go).
- Local-only locks (per-process state) use `sync.Mutex` / `sync.RWMutex`.
- Long-lived per-resource locks (per peer, per account) use `sync.Map`
  to avoid an explicit `delete` race. There are precedents in
  `grpcserver.go:peerLocks` and `account.go:accountUpdateLocks`.

## Commit hygiene

- One logical change per commit. The wiring + the rename + the test:
  three commits if they can stand alone.
- Subject ≤ 72 chars, imperative mood, prefixed with the affected
  package or area: `fix(auth): …`, `feat(cluster): …`, `docs(adr): …`,
  `refactor(signal): …`.
- Body explains **why**, not what (the diff already says what). Cite
  ADRs and prior commits by SHA where relevant. Match the style of the
  existing log: every commit since `5de61f30` follows this format.
- Co-authorship trailer for AI-assisted work:
  `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>`.

---

# Engineering rules — general

## What lives where

| Path | Rule |
|---|---|
| `cluster/` | Distributed primitives. Backend-agnostic interfaces in `cluster/`; concrete impls in `cluster/redis/`, `cluster/nats/`. New backends add a sibling subpackage and a switch case in `cluster/factory/factory.go`. |
| `management/server/` | Domain logic. Tests at `_test.go`. New services get their own subpackage. |
| `management/integrations/integrations/` | **In-tree clean-room** stub of the upstream's GPL `management-integrations`. Do not delete; do not vendor the GPL upstream back. |
| `signal/dispatcher/` | Same shape as `cluster/`: interface in the parent, backends in subpackages. |
| `docs/adr/` | Architecture Decision Records. New non-trivial decisions get their own ADR; old ADRs are append-only. |
| `docs/security/` | Advisory triage. Update `advisories.md` for every CVE/GHSA we evaluate. |
| `brand/`, `design-tokens.md`, top-level `CLAUDE.md` | Brand assets. Do not move them deeper into the tree; the dashboard imports `../../brand/openzro-icon.svg`. |
| `LICENSE`, `AUTHORS` (every copy) | Verbatim, attribution-preserving. Read-only. |

## When asking the user

- Surface license-sensitive choices early. "This dep is GPL — should we
  vendor or stub?" is a question for the human, not a unilateral call.
- Surface architectural choices that span >1 commit. "Lock everything
  with `pg_advisory_lock` or with a broker?" is a question for the
  human.
- Do not surface mechanical choices (gofmt vs not, comment phrasing,
  variable names) — make the call and move on.
