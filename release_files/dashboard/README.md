# openzro-dashboard package

Static SPA build of the openZro dashboard. The package ships the
Next.js export bundle plus example reverse-proxy configs and an
env-injection script — it does NOT install or configure a web
server. You front it with whatever you already run (nginx, Caddy,
Apache, Cloudflare Pages, etc.).

## Layout after install

```
/usr/share/openzro-dashboard/
  ├── index.html, _next/, ...        (Next.js static export)
  ├── OidcTrustedDomains.js.tmpl     (env-substituted at install time)
  └── render-env.sh                  (substitutes env values into the bundle)

/usr/share/doc/openzro-dashboard/
  ├── nginx.conf.example
  ├── Caddyfile.example
  ├── openzro-dashboard.env.example
  └── README.md   (this file)

/etc/openzro-dashboard.env           (NOT shipped — operator copies the .example)
```

## First-time setup

```sh
# 1. Copy the env template + edit
sudo cp /usr/share/doc/openzro-dashboard/openzro-dashboard.env.example \
        /etc/openzro-dashboard.env
sudo $EDITOR /etc/openzro-dashboard.env

# 2. Render the env values into the static bundle
sudo /usr/share/openzro-dashboard/render-env.sh

# 3. Configure your web server. nginx example:
sudo cp /usr/share/doc/openzro-dashboard/nginx.conf.example \
        /etc/nginx/sites-available/openzro-dashboard
sudo $EDITOR /etc/nginx/sites-available/openzro-dashboard
sudo ln -sf /etc/nginx/sites-available/openzro-dashboard \
            /etc/nginx/sites-enabled/openzro-dashboard
sudo nginx -t && sudo nginx -s reload
```

## What your reverse proxy needs to do

The dashboard is a SPA, the management server is at gRPC + REST,
and the signal server is gRPC. Your reverse proxy needs to handle
four patterns on the same hostname:

| Path | Backend | Notes |
|---|---|---|
| `/api` | `http://management-host:33071` | management REST |
| `/management.ManagementService` | `grpc://management-host:33073` | management gRPC, HTTP/2 mandatory |
| `/signalexchange.SignalExchange` | `grpc://signal-host:10000` | signal gRPC, HTTP/2 mandatory |
| `/` (everything else) | static `/usr/share/openzro-dashboard/` | SPA, fallback to `/index.html` |

The management host's relay is a **separate concern** — it speaks
WireGuard and a custom UDP/TCP protocol on port 33080, and is NOT
proxied through the web server. Expose it directly at the firewall
(or cloud LB at L4).

## Updating env after first install

Re-edit `/etc/openzro-dashboard.env` and re-run `render-env.sh`.
The script keeps a pristine `.orig` snapshot of the static files
on first run so re-rendering with new values doesn't double-
substitute.

## Upgrading

`apt upgrade openzro-dashboard` (or `dnf`/`pacman` equivalent)
replaces the static files in place. The first re-run of
`render-env.sh` after the upgrade re-creates the `.orig`
snapshot from the new files. Re-render to apply your env values
to the new bundle.
