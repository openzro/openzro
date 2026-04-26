# openzro — fork point

This repository is a fork of **NetBird** ([netbirdio/netbird](https://github.com/netbirdio/netbird)) at the last BSD-3-Clause licensed state, before the upstream relicensed parts of the project to AGPLv3.

## Source repos and pinned tags

| Component        | Upstream                                  | Tag pinned | Date         | License at this ref |
|------------------|-------------------------------------------|------------|--------------|---------------------|
| repo root (Go)   | https://github.com/netbirdio/netbird      | `v0.52.2`  | 2025-07-30   | BSD-3-Clause (whole tree) |
| `dashboard/`     | https://github.com/netbirdio/dashboard    | `v2.15.0`  | 2025-07-30   | BSD-3-Clause |

Both clones were made with `--single-branch --branch <tag>` so only commits reachable from the pinned tag are present locally — nothing from after the AGPL relicense was fetched.

## Why this exact cut

- Upstream `netbirdio/netbird` `v0.53.0` (2025-08-06) added an AGPLv3 LICENSE inside `management/`, `signal/` and `relay/`. `v0.52.2` is the last release where the whole tree is BSD-3-Clause.
- The dashboard `netbirdio/dashboard` `v2.15.0` was published on the same day as netbird `v0.52.2` (2025-07-30) and is the last dashboard release before the upstream coordinated relicense window.

## License posture for openzro

- All code in this repository, at the fork point, is BSD-3-Clause. The original `LICENSE` files (root and `dashboard/`) are preserved unchanged, including upstream copyright lines — those MUST stay (BSD-3 attribution clause).
- New code added on top of this fork must remain compatible with BSD-3-Clause. Do not pull post-AGPL upstream commits into `management/`, `signal/`, or `relay/` (or any directory) without an explicit license review — those are AGPL upstream and would taint the fork.
- Cherry-picks from upstream are only safe if the specific commit predates the AGPL relicense (≤ `v0.52.2` for core, ≤ `v2.15.0` for dashboard), or is independently re-implemented.

## Repo layout

```
openzro/
├── .git/            (single repo history; initial state imported from netbirdio/netbird@v0.52.2)
├── docs/
│   └── FORK.md      (this file)
├── client/  management/  signal/  relay/  ...   (Go core, ex-netbirdio/netbird)
└── dashboard/       (Next.js web UI, ex-netbirdio/dashboard@v2.15.0 — integrated as a subfolder)
```

This is a single-repo monorepo. The `dashboard/` folder was imported from `netbirdio/dashboard@v2.15.0` as a plain directory snapshot — its independent upstream Git history is **not** preserved here. Refer to https://github.com/netbirdio/dashboard for the original web UI history up to that tag.
