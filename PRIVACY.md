# Privacy policy

openZro is self-hosted software. The openZro project does not run a hosted
service, and it does not collect, receive or store data from the people or
machines that use openZro.

The openZro programs do not send information to any system other than the
ones the person installing or operating them configures, unless a user
explicitly asks them to. The rest of this page lists every network
connection the programs make, and why.

## Client (`openzro`, `openzro-ui`, the Windows installer)

The client connects only to the openZro servers named in its configuration:
the management server, and the signal, relay, STUN and TURN servers that the
management server assigns. Those servers belong to whoever runs the openZro
deployment (for example, your employer). That operator, not the openZro
project, controls the data, under its own privacy terms.

What the client sends to that deployment:

- **To sign in and register the device:** the login token from the
  operator's identity provider, and the device's details: hostname,
  operating system and version, client version, network interfaces and
  addresses, and the posture checks the operator enables.
- **To build the tunnels:** WireGuard public keys and connection
  candidates (IP addresses and ports), exchanged through the signal and
  relay servers. Traffic between peers is end-to-end encrypted by
  WireGuard; the relays cannot read it.
- **Traffic flow events**, only if the operator turns that feature on.

The client makes other connections only when a user asks it to:

- **Opening a web page** in the browser: the sign-in page, the 2FA
  approval page, or the download page of a new version.
- **Uploading a debug bundle** (`openzro debug ... --upload-bundle`, or the
  *Upload* option in the desktop app) to the URL shown before the upload.
  The bundle can be anonymized with `--anonymize`.
- **Installing an update** on macOS, when the operator has enabled updates
  and the user starts one.

## Server components (management, signal, relay, dashboard)

These run on the operator's own infrastructure. Besides talking to the
clients and to the identity provider the operator configures, the
management server:

- **checks for new versions** by reading the public list of releases from
  the GitHub API (`api.github.com`) every 30 minutes. Set
  `OPENZRO_UPDATE_CHECK_URL=` (empty) to turn the check off;
- **can send aggregate, anonymous usage counts**: a random installation
  ID, the server version, the type of identity provider and store, and
  totals such as numbers of accounts, peers, users, groups and policies,
  and peers per operating system and client version. No names, e-mail
  addresses, IP addresses, keys or traffic. The endpoint for these counts is not
  operated by the openZro project today, so no data is received. Start the
  management server with `--disable-anonymous-metrics` to turn it off.

## Questions

Open an issue at <https://github.com/openzro/openzro/issues>, or for a
sensitive matter use the private channel in [SECURITY.md](SECURITY.md).
