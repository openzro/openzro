# openZro dashboard · engineering rules

This file scopes the AI assistant's behavior to the `dashboard/`
subtree (Next.js / React / Tailwind). The brand rules in the
[root `CLAUDE.md`](../CLAUDE.md) still apply on top of this file —
specifically the `openZro` spelling, the violet token palette, the
icon location, and the wordmark/lockup HTML pattern.

## Stack

- **Next.js 14 (App Router)**, stable. Not on canary releases — and not
  on Next 15+ until the security implications of CVE-2025-66478 are
  re-evaluated for our usage. See [`docs/security/advisories.md`](../docs/security/advisories.md).
- **React 18.3** with TypeScript in strict mode.
- **Tailwind CSS** with the palette and font tokens defined in
  [`tailwind.config.ts`](tailwind.config.ts) and CSS variables in
  [`src/app/globals.css`](src/app/globals.css).
- **Cypress** for E2E tests.

## Test-driven development is the default

UI work that's just visual (color tweak, copy change) does not need a
test. **Behavior changes do** — a new form field, a new request, a new
state transition, a permission check.

- E2E tests live in `cypress/e2e/`. Add one for every user-visible flow
  before changing the underlying component.
- Component tests with `@cypress/react` are acceptable for components
  with non-trivial internal state. Don't write a test that just snapshots
  the rendered output — those are noise.
- Run with `npm run cypress:open` (interactive) or
  `npm run cypress:run` (CI / `make test.dashboard`).

## TypeScript style

| Rule | Why |
|---|---|
| `strict: true` in `tsconfig.json` — no relaxing | catches the obvious bugs |
| **No `any`.** If you genuinely don't know the type, use `unknown` and narrow with a type guard | `any` defeats the type system |
| **No `as` casts** unless you're at the boundary with a third-party library that has worse typing than reality | catches pretending |
| `interface` for object shapes that may be extended; `type` for unions and computed types | matches the React community convention |
| Imports follow the order: third-party → `@/...` aliases → relative `./` → CSS / asset | enforced by ESLint config |
| Discriminated unions over optional fields when modeling state (`{ status: 'idle' } \| { status: 'loading' } \| { status: 'error', err: Error }`) | makes impossible states unrepresentable |
| Date / time on the wire is ISO-8601 strings; use `dayjs` for parsing/formatting | already a dependency, don't bring `date-fns` |

## React style

| Rule | Why |
|---|---|
| Function components only. No class components | no reason to use them, plus hooks |
| **Component file ≤ ~250 lines.** Split when longer (extract subcomponents, pull state into a hook) | reviewability |
| **Component function body ≤ ~80 lines.** Same | reviewability |
| Hooks named `use<Thing>`; one custom hook per file when non-trivial | discoverability |
| Effects: `useEffect` only for synchronizing with non-React state (timers, subscriptions, gRPC streams). NOT for "do this when prop changes" — that's just JSX | prevents re-render loops |
| Server components where the page does not need interactivity. `"use client"` only at the leaf that actually needs hooks/event handlers | smaller JS bundles |
| Forms: `react-hook-form` + `zod` for validation. Don't roll your own | consistency |
| Lists: stable `key` based on the item's identity, never the index | avoids reconciliation bugs |

## Styling

| Rule | Why |
|---|---|
| **Tailwind utilities first.** No `style={{}}` props except for dynamic values that can't be expressed as classes (e.g. computed positions, animation deltas) | every dev sees the same tokens |
| **Use the `openzro-*` palette** for brand color, never raw hex values. The class names are unchanged from the original NetBird palette but the colors are now the brand violet — see [`tailwind.config.ts`](tailwind.config.ts) | tokens stay swappable |
| For `--oz-*` CSS variables in custom CSS, prefer the **semantic aliases** (`var(--oz-primary)`, `var(--oz-bg)`, `var(--oz-border)`) over raw scale values (`var(--oz-violet-600)`) | theming friendly |
| Dark mode: Tailwind class strategy (`<html class="dark">`). Both light and dark token sets are in [`globals.css`](src/app/globals.css). When adding a new component, verify it looks right in both | half our users prefer dark |
| **No `!important`** unless you're overriding a third-party widget that does not expose a customization API | escape valve only |

## Component naming

- Reusable components carry the `Oz` prefix (`OzButton`, `OzCard`,
  `OzTerminal`) — searchable, keeps brand surface obvious.
- Inherited components from upstream that haven't been touched yet
  keep their original names. Rename when you're touching them
  meaningfully anyway, not as a separate refactor pass.
- The wordmark always uses the markup pattern from the root
  `CLAUDE.md`: `<span class="oz-wordmark">open<span class="oz-z">Z</span>ro</span>`.
  Never hardcode the spelling as a single text node, because that loses
  the heavy middle Z.

## Icon and assets

- The official icon lives at [`/brand/openzro-icon.svg`](../brand/openzro-icon.svg)
  in the repo root and is mirrored at
  [`src/assets/openzro.svg`](src/assets/openzro.svg) for components that
  import via the `@/assets` alias. Keep them in sync.
- `src/assets/openzro-full.svg` currently points at the same icon.
  When a real wordmark+icon SVG is produced, replace it there; do
  *not* introduce a third asset file.
- New images go under `src/assets/`. SVG preferred; raster only for
  bitmap photography.

## Performance

- **Server components by default.** Reach for `"use client"` only when
  you need state, refs, or browser APIs.
- **Dynamic imports** for heavy components used on a single route
  (`next/dynamic` with `ssr: false` for browser-only widgets).
- **No CSS-in-JS runtime libraries** (styled-components, emotion).
  Tailwind covers it.
- **Image optimization** via `next/image` for any image larger than an
  icon.

## What not to introduce

- **No new charting / table / dropdown libraries.** The dashboard
  already uses Radix primitives + Tailwind for these. Adding another
  library balloons the bundle and fragments the look.
- **No analytics / telemetry SDKs** without an ADR. The current
  `AnalyticsProvider` is opt-in via env vars; keep it that way.
- **No dependencies with non-OSI licenses.** No SSPL, BUSL, or
  Commons Clause.
- **Don't import server-only utilities into client components** (the
  TypeScript boundary catches most of this; verify in code review too).

## Commit hygiene

Same conventions as the Go side (see [root CLAUDE.md](../CLAUDE.md)):

- One logical change per commit, scoped subject.
- For dashboard work the prefix is `feat(dashboard): …`,
  `fix(dashboard): …`, `style(dashboard): …`, etc.
- Co-authorship trailer for AI-assisted work.
