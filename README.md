# Company Edge

**Company Edge** is an operations and control-plane layer built around [Reproxy](https://github.com/umputun/reproxy), intended to manage reverse-proxy routes, primary/backup origins, health-based failover, and eventually multiple proxy nodes from one central panel.

> [!IMPORTANT]
> **Current maturity: development / staging. Not production-ready yet.** The single-node Reproxy routing and manual/automatic failover path have been exercised in a lab. The central node API and Agent have been added and compile, but a remote Agent has not yet been proven end-to-end in production-like conditions. The administration UI is currently exposed only on `127.0.0.1:8090`; LAN access to the administration UI over HTTPS is not yet configured. Review [Known limitations and production gate](#known-limitations-and-production-gate) before deployment.

## Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Features](#features)
- [Technology stack and languages](#technology-stack-and-languages)
- [Source code and references](#source-code-and-references)
- [Repository layout](#repository-layout)
- [Requirements](#requirements)
- [Ports and network access](#ports-and-network-access)
- [Configuration](#configuration)
- [Run locally or in a lab](#run-locally-or-in-a-lab)
- [Deploy the central server](#deploy-the-central-server)
- [Access the administration panel](#access-the-administration-panel)
- [Register a Proxy Node](#register-a-proxy-node)
- [Deploy the Agent on a Proxy Node](#deploy-the-agent-on-a-proxy-node)
- [Production deployment runbook](#production-deployment-runbook)
- [Validation and testing](#validation-and-testing)
- [Operations, logs, and backups](#operations-logs-and-backups)
- [Security guidance](#security-guidance)
- [Troubleshooting](#troubleshooting)
- [Known limitations and production gate](#known-limitations-and-production-gate)
- [License and attribution](#license-and-attribution)

## Overview

Company Edge separates the system into a **central control plane** and one or more **data-plane Proxy Nodes**:

- The **central control plane** stores domain records and audit events, provides the authenticated administration UI, issues node credentials, and serves active Reproxy configuration to authenticated nodes.
- A **Proxy Node** runs Reproxy for user traffic and a Company Edge Agent for configuration sync and heartbeat reporting.
- Reproxy handles request routing, origin health checks, and primary/backup failover. The control plane does not proxy end-user requests.

The repository contains the upstream Reproxy codebase as well as Company Edge's custom control-panel and Agent code. See [Source code and references](#source-code-and-references) for upstream documentation.

## Architecture

```text
                         Admin browser
                              |
                 SSH tunnel (current admin access)
                              |
                   127.0.0.1:8090 on central host
                              |
                   +------------------------+
                   | Company Edge Control   |
                   | Panel (Go + HTML/CSS/JS)|
                   | JSON store + audit log |
                   +------------------------+
                              |
                   HTTPS + per-node Bearer token
                       10.20.45.252:8444
                              |
                 +------------+------------+
                 |                         |
        +------------------+       +------------------+
        | Proxy Node 1     |       | Proxy Node 2     |
        | Company Edge     |       | Company Edge     |
        | Agent            |       | Agent            |
        | Reproxy          |       | Reproxy          |
        +------------------+       +------------------+
                 |                         |
          Origins / applications     Origins / applications
```

### Configuration flow

1. An administrator signs in to the central panel.
2. The administrator registers a node; the panel returns a random node token once and stores only its SHA-256 hash.
3. The Agent requests `GET /api/node/config` over HTTPS and authenticates with `Authorization: Bearer <token>`.
4. The panel returns YAML for active domains and a SHA-256 response header.
5. The Agent verifies the hash and basic YAML structure, then writes the file atomically to its configured `CONFIG_PATH`.
6. The Agent sends `POST /api/node/heartbeat` with its configuration hash and optional error status.
7. Reproxy reads its local file-provider configuration and performs the request routing and origin health checks.

**Current distribution model:** all active domain records are included in the configuration returned to every enabled node. Per-node domain assignment, rollout groups, and coordinated multi-node rollback are not implemented yet.

## Features

### Implemented or exercised in the current lab

- Reproxy file-provider configuration mounted from `reproxy-config/reproxy.yml`.
- Primary/backup origin routing with Reproxy health checks and `--lb-type=failover`.
- Central panel login using configured administrator credentials and an HTTP-only session cookie.
- Domain draft creation, activation, manual primary/backup preference, and audit records.
- Persistent domain, node, and audit data in a JSON store.
- Per-node random Bearer credentials; the persisted record contains the token hash, not the raw token.
- TLS listener for the node API on port `8444`.
- Node configuration download, heartbeat endpoint, and an Agent that compiles and implements periodic synchronization.
- Docker Compose deployment for Reproxy and the central panel.
- A host firewall rule set intended to limit the node API to the internal network.

### Not yet production-verified

The Agent has compiled successfully, but remote-node enrollment, CA trust, file updates, Reproxy live reload, heartbeat status, reconnection behavior, and failure recovery still require an end-to-end test on a second server. See the release gate below.

## Technology stack and languages

| Area | Technology | Role |
|---|---|---|
| Reverse proxy | [Reproxy](https://github.com/umputun/reproxy) v1.7.1 in the current Compose file | HTTP/HTTPS routing, file-based configuration, origin health checks and failover |
| Backend / control plane | Go (module targets Go 1.26) | Authentication, API handlers, node management, configuration generation, heartbeat processing |
| Agent | Go | Fetches config, verifies its SHA-256 hash, atomically writes the file, reports heartbeat |
| Administration UI | HTML5, CSS3, vanilla JavaScript | Browser UI and authenticated API calls |
| Configuration | YAML | Reproxy route configuration and Docker Compose files |
| Persistent control-plane data | JSON | Domain, node, and audit records in `control-panel/data/store.json` |
| Containers | Docker Engine and Docker Compose v2 | Process packaging and service lifecycle |
| TLS / certificates | TLS, X.509, OpenSSL; `mkcert` for lab certificates only | HTTPS for Reproxy and the node API |
| Host networking | UFW, iptables, systemd | Host firewall and persistent node-API filtering |

## Source code and references

- **Reproxy upstream repository:** <https://github.com/umputun/reproxy>
- **Reproxy documentation:** <https://reproxy.io/>
- **Go documentation:** <https://go.dev/doc/>
- **Docker Engine documentation:** <https://docs.docker.com/engine/>
- **Docker Compose documentation:** <https://docs.docker.com/compose/>
- **YAML v3 Go package:** <https://github.com/go-yaml/yaml>
- **mkcert documentation (development/internal lab certificates):** <https://github.com/FiloSottile/mkcert>
- **Ubuntu Server documentation:** <https://ubuntu.com/server/docs>

Keep the repository's `LICENSE` and relevant upstream attribution when redistributing. Review license compatibility before shipping a packaged product.

## Repository layout

```text
.
├── app/                         # Reproxy application source
├── lib/                         # Reproxy libraries
├── examples/                    # Upstream Reproxy examples
├── control-panel/
│   ├── main.go                  # Panel server, sessions, domain APIs, store
│   ├── publish.go               # Writes active domain records to Reproxy YAML
│   ├── nodes.go                 # Admin APIs for node listing/registration
│   ├── nodes_runtime.go         # Token-authenticated node config/heartbeat API
│   ├── agent/
│   │   └── main.go              # Company Edge Agent
│   ├── static/index.html        # Administration UI (HTML/CSS/JavaScript)
│   ├── Dockerfile               # Central control-panel image
│   ├── Dockerfile.agent         # Agent image build definition
│   ├── go.mod / go.sum          # Control-panel dependencies
│   └── data/                    # Runtime persistent store; do not commit
├── reproxy-config/
│   └── reproxy.yml              # Active Reproxy file-provider configuration
├── certs/                       # TLS files; development files must not be used in production
├── runtime-secrets/             # Runtime TLS key/cert material; do not commit
├── dist/                        # Locally built binaries; do not commit
├── docker-compose.yml           # Reproxy service
├── compose.control-panel.yml    # Central panel and node API
├── Dockerfile                   # Upstream Reproxy build definition
├── go.mod / go.sum              # Upstream Reproxy Go module
├── .dockerignore
└── README.md
```

Runtime data, passwords, node tokens, private keys, and generated binaries must remain outside Git. Do not copy private keys into issue reports or chat messages.

## Requirements

The current lab was built on Ubuntu Server 24.04.5 with Docker Engine 29.1.3 and Docker Compose 2.40.3. These are the environment versions used during development, not a guarantee that every version combination is supported.

### Central server

- Linux server; Ubuntu Server 24.04 LTS is the tested lab platform.
- Docker Engine and Docker Compose v2 plugin.
- Git, `curl`, `openssl`, `iproute2`, `iptables`, and `systemd` for operations and diagnostics.
- A stable internal IP address or DHCP reservation.
- A secure administrator username/password stored outside source control.
- A trusted TLS certificate and private key for the node API. For production, use the company PKI or an appropriately managed certificate authority; the current `mkcert` CA is for development/lab use only.
- Firewall access limited to the intended administrative and node networks.

### Each Proxy Node

- A supported Linux host with Docker Engine / Compose if Reproxy is run in a container.
- Network reachability to the central node API over TCP `8444`.
- The Company Edge Agent binary built for the node's operating system and CPU architecture.
- A unique node token stored in a permission-restricted file.
- The CA certificate that issued the central API certificate installed into the operating system trust store when using the native Agent binary.
- A writable host configuration directory shared with the Reproxy container. Reproxy should mount the same directory read-only.
- Origin URLs and health-check URLs reachable from the Proxy Node.

### Developer workstation (optional)

- Go 1.26 for native builds/tests, or Docker to run the Go toolchain in a disposable container.
- Git and a POSIX shell for the commands in this guide.
- `mkcert` and OpenSSL only for lab certificate creation/inspection.

## Ports and network access

| Port | Service | Current binding / purpose | Production guidance |
|---:|---|---|---|
| `8080/tcp` | Reproxy HTTP | Currently published by the Reproxy Compose file | For a public website, decide whether to map host port 80 to container port 8080 after configuring the intended firewall and redirects. |
| `8443/tcp` | Reproxy HTTPS | Currently published by the Reproxy Compose file | Uses the configured Reproxy certificate; replace the lab certificate before real traffic. A public setup commonly maps host 443 to container 8443. |
| `8090/tcp` | Control-panel UI/API | Published only on `127.0.0.1:8090` | Keep loopback-only until a separate, reviewed LAN HTTPS listener is configured. Current access is through SSH tunneling. |
| `8444/tcp` | Node configuration API | Current central host binds `10.20.45.252:8444` | TLS + per-node Bearer token; firewall is intended to allow only `10.20.45.0/24`. Change the IP, subnet and interface to match the actual environment. |
| `3000/tcp` | Test primary origin | Lab only | Do not use the demo origin in production. |
| `3001/tcp` | Test backup origin | Lab only | Do not use the demo origin in production. |

The current lab host uses `ens34`, central address `10.20.45.252/24`, and subnet `10.20.45.0/24`. These values are **environment-specific examples**, not defaults that should be copied to another network unchanged.

The current node API firewall uses `DOCKER-USER` and the systemd unit `company-edge-node-api-firewall.service`. Docker-published ports can have different packet paths from ordinary host listeners; verify the effective iptables rules rather than assuming a UFW rule alone limits Docker traffic.

## Configuration

### Control-panel environment file

`compose.control-panel.yml` loads `control-panel/.env`. The file is ignored by Git and must have restrictive permissions. It contains at least:

```dotenv
CONTROL_PANEL_ADMIN_USER=replace-with-an-admin-username
CONTROL_PANEL_ADMIN_PASSWORD=replace-with-a-long-unique-password
```

The application rejects an unset administrator username/password and requires the password to be at least 14 characters. Use a password manager to generate a unique password. Do not put real credentials in this README, Git, shell history, screenshots, or support requests.

On a new checkout, create and edit the file locally:

```bash
install -m 600 /dev/null control-panel/.env
nano control-panel/.env
chmod 600 control-panel/.env
```

Do not run the `install` command over an existing `.env` file because it would truncate it.

### Central node API TLS files

The current central Compose configuration expects these paths inside the project:

```text
runtime-secrets/node-api.pem       # server certificate / certificate chain
runtime-secrets/node-api-key.pem   # matching private key
runtime-secrets/rootCA.pem         # public CA certificate for client trust setup
```

The private key should be readable only by the panel process's required group, not world-readable. The root CA certificate is public; the CA's **private signing key** must never be copied to Proxy Nodes. The current files were created using `mkcert` for an internal lab. Replace them with company-managed certificates before production.

The node API certificate must contain a Subject Alternative Name matching the exact URL used by the Agent. The current lab certificate contains IP SAN `10.20.45.252`, so the lab Agent URL is `https://10.20.45.252:8444`.

### Agent environment variables

| Variable | Required | Meaning |
|---|---|---|
| `PANEL_URL` | Yes | Base URL of the central node API, including port; must use HTTPS except for loopback-only testing. Example: `https://10.20.45.252:8444`. |
| `NODE_TOKEN_FILE` | Yes | Absolute path to a file containing the node's raw token. The token is not passed as a command-line argument. |
| `CONFIG_PATH` | No | Absolute path to the Reproxy YAML file. Defaults to `/etc/reproxy-config/reproxy.yml`. |
| `POLL_INTERVAL` | No | Interval between sync attempts; default `15s`, accepted range `5s` to `5m`. |

### Reproxy configuration

The central panel reads/writes `reproxy-config/reproxy.yml`. A simplified route group resembles:

```yaml
cdn.example.com:
  - route: "/"
    dest: "http://10.20.45.20:8080"
    ping: "http://10.20.45.20:8080"
  - route: "/"
    dest: "http://10.20.45.21:8080"
    ping: "http://10.20.45.21:8080"
```

Use real domains and reachable origin URLs. `cdn.example.com` and the IPs above are placeholders. Keep credentials out of URLs. Validate TLS certificates for HTTPS origins where appropriate; do not treat `curl -k` as a production validation method.

Domain draft creation does not by itself activate a route. Review and activate a domain in the panel before expecting it to appear in the active configuration.

## Run locally or in a lab

### 1. Validate Compose files

From the repository root:

```bash
docker compose -f docker-compose.yml config -q
docker compose -f compose.control-panel.yml config -q
```

### 2. Start Reproxy

```bash
docker compose -f docker-compose.yml up -d
```

The current `docker-compose.yml` uses image `umputun/reproxy:v1.7.1`, mounts `reproxy-config/` read-only into Reproxy, mounts `certs/` read-only, enables the file provider and health checks, and selects `--lb-type=failover`.

### 3. Start the control panel

```bash
docker compose -f compose.control-panel.yml up -d --build control-panel
```

This starts the central panel on loopback port `8090` and the TLS node API on the configured internal IP and port `8444`.

### 4. Check service health

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
curl -fsS --max-time 5 http://127.0.0.1:8090/api/health
```

The current health endpoint reports `mode: "authenticated-preview"` and `active: false`. This is a known unfinished status field; a successful HTTP response does **not** mean the platform has passed its production-readiness gate.

### 5. Test the node API TLS without credentials

On the central host:

```bash
curl --cacert runtime-secrets/rootCA.pem \
  --output /dev/null --write-out 'HTTP %{http_code}\n' \
  --max-time 5 https://10.20.45.252:8444/api/node/config
```

Expected for the lab: HTTP `401`. This is intentional and proves that TLS was negotiated but an unauthenticated node request was rejected. A certificate-verification error is a separate problem and must not be worked around in production with `-k`.

## Deploy the central server

This section describes a conservative central-server rollout. Adapt all addresses, certificates, interfaces, and subnets to the target environment.

### 1. Prepare the host

Install Docker Engine and the Compose plugin using the [official Docker instructions](https://docs.docker.com/engine/install/). Install operational utilities if missing:

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl git openssl iproute2 iptables ufw
```

Use a stable IP address or DHCP reservation for the control plane. Restrict SSH to trusted administrative networks. Do not expose the control-panel port directly to the public Internet.

### 2. Obtain the source

Use the repository URL maintained by your organization; do not substitute an invented public URL:

```bash
git clone <YOUR_COMPANY_EDGE_REPOSITORY_URL> company-edge
cd company-edge
```

Review uncommitted changes and build from a known release commit/tag in production. Do not deploy directly from an unreviewed working tree.

### 3. Configure credentials and secrets

- Configure `control-panel/.env` with a unique administrator username and a long unique password.
- Ensure `control-panel/.env`, `control-panel/data/`, `runtime-secrets/`, private keys, and generated binaries are excluded from Git.
- Keep TLS private keys outside the source repository when practical and give only the service account the access it needs.
- Back up secrets using your organization's protected secret-storage process; do not include them in ordinary source backups.

### 4. Configure real certificates

The repository currently refers to development certificates such as `certs/test.company.local.pem`. These must not be used for real users. Install certificate/key pairs issued by the organization's PKI or a certificate authority trusted by the intended clients. Confirm that certificate SANs match the names/IPs used by clients and that renewals are documented.

The current Reproxy Compose file uses one static certificate/key pair. If production domains require different certificates, verify the Reproxy multi-certificate configuration and update the design before launch; do not assume the current single-certificate command automatically handles arbitrary hostnames.

For the internal node API, the CA public certificate must be trusted on each node running the native Go Agent. The existing `mkcert` root is for the lab only. Never copy `rootCA-key.pem` or any CA private key to a node.

### 5. Review routing and exposure

- Replace all `demo.example.com`, `test.company.local`, and test-origin entries.
- Confirm that every primary/backup origin is the real service, is reachable from all intended Proxy Nodes, and has an appropriate health endpoint.
- Decide whether external traffic should arrive on host ports 80/443. The current Compose file publishes 8080/8443; changing this to `80:8080` and `443:8443` requires checking host-port availability, certificates, firewall, and any existing web server first.
- Keep `8090` loopback-only until LAN administration is separately designed and reviewed over HTTPS.
- Keep `8444` restricted to the node network. Verify `DOCKER-USER` rules and the systemd firewall service after any Docker or network changes.

### 6. Validate and start

```bash
docker compose -f docker-compose.yml config -q
docker compose -f compose.control-panel.yml config -q
docker compose -f docker-compose.yml up -d
docker compose -f compose.control-panel.yml up -d --build control-panel
docker ps
docker logs --tail 100 company-control-panel
```

Do not remove or stop origin services until their production replacements have been confirmed healthy. Do not perform a kernel/OS upgrade or reboot as part of a routine application deployment unless it is a separately planned change.

## Access the administration panel

The current panel is exposed on the central host's loopback interface only. From an administrator workstation, establish an SSH tunnel:

```bash
ssh -L 8090:127.0.0.1:8090 <ADMIN_USER>@<CENTRAL_SERVER_IP>
```

Then browse to <http://127.0.0.1:8090> and sign in using the values in `control-panel/.env`.

This is the current safe access method while the dedicated LAN HTTPS listener for the administration UI remains unconfigured. Do **not** open port 8090 in UFW or bind it to `0.0.0.0` as a shortcut. The node API on `8444` is not the administration website.

## Register a Proxy Node

1. Sign in to the central panel.
2. Open **Proxy Nodes**.
3. Enter a unique, descriptive node name such as `edge-baku-02`.
4. Click **Register node**.
5. Copy the returned token and store it in a secret manager or protected transfer workflow. It is shown only once by the registration response.
6. Do not paste the token into Git, a ticket, a chat message, or a shell command that may be saved in history.

The server stores a SHA-256 hash of the random token. If the token is lost, it cannot be recovered from the hash; use the token-rotation/re-enrollment capability provided by the deployed panel version, then update the Agent's token file.

The registration API and panel UI are administrator-authenticated. The runtime node endpoints use their own per-node Bearer token on the TLS listener.

## Deploy the Agent on a Proxy Node

> **Staging procedure — first validate on a non-production second server.** The Agent has compiled, but the full remote enrollment flow still needs end-to-end acceptance testing.

### 1. Build the Agent binary on the central build host

From the repository root:

```bash
mkdir -p dist
docker run --rm \
  -v "$PWD/control-panel:/src" \
  -v "$PWD/dist:/out" \
  -w /src golang:1.26-alpine \
  sh -c 'go build -trimpath -o /out/edge-agent ./agent'
file dist/edge-agent
```

Build for the target node's OS/architecture. This command builds for the current Linux container architecture; it is not automatically a cross-compile for ARM or another architecture.

### 2. Transfer the binary and CA certificate securely

Use your approved internal file-transfer process (for example, `scp` over SSH) to copy:

- `dist/edge-agent` to the second server;
- the public CA certificate which issued `node-api.pem` to the second server.

Do not transfer the CA private key. The current lab CA can be read from `runtime-secrets/rootCA.pem`, but it is generated by `mkcert`; replace it with the enterprise CA for a real deployment.

### 3. Install and trust the CA on the Agent host

For an internal CA on Ubuntu, after verifying its fingerprint through a separate trusted channel:

```bash
sudo install -m 0644 company-edge-root-ca.crt \
  /usr/local/share/ca-certificates/company-edge-root-ca.crt
sudo update-ca-certificates
```

The native Go Agent uses the operating system's trust store. Do not disable certificate validation or add an insecure TLS flag.

### 4. Create the Agent service account and secret file

Create a dedicated local user and protected secret directory on the **Proxy Node**, not on the central server:

```bash
sudo useradd --system --home /nonexistent --shell /usr/sbin/nologin edge-agent
sudo install -d -o root -g edge-agent -m 0750 /etc/company-edge
sudo install -d -o root -g edge-agent -m 0750 /opt/company-edge
sudo install -d -o edge-agent -g edge-agent -m 0775 /opt/company-edge/reproxy-config
sudo install -o root -g edge-agent -m 0755 edge-agent /usr/local/bin/edge-agent
```

Create `/etc/company-edge/node-token` using a protected editor or secret-management workflow. Put only the raw token for this node in that file. Then set its ownership and permissions:

```bash
sudo chown root:edge-agent /etc/company-edge/node-token
sudo chmod 0640 /etc/company-edge/node-token
```

Do not put the token directly in the Agent command line. Ensure the Agent user can read the token file and write the configuration directory, while unrelated users cannot read the token.

### 5. Configure the Reproxy service on the node

The Reproxy container must mount the same host directory that the Agent writes:

```yaml
volumes:
  - ./reproxy-config:/etc/reproxy-config:ro
```

Set the Agent's `CONFIG_PATH` to the **host path** corresponding to that shared directory, for example `/opt/company-edge/reproxy-config/reproxy.yml`. The Reproxy container sees that same file at `/etc/reproxy-config/reproxy.yml`. The Agent needs write access; Reproxy should have read-only access.

Use the intended Reproxy flags in the node's Compose file, including the file-provider name, health checks, and failover strategy. Install a production certificate for the hostnames that node is expected to serve. Do not copy the central server's unrelated test certificates blindly.

### 6. Create the Agent systemd service

Create `/etc/systemd/system/company-edge-agent.service` on the Proxy Node:

```ini
[Unit]
Description=Company Edge Proxy Node Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=edge-agent
Group=edge-agent
Environment=PANEL_URL=https://10.20.45.252:8444
Environment=NODE_TOKEN_FILE=/etc/company-edge/node-token
Environment=CONFIG_PATH=/opt/company-edge/reproxy-config/reproxy.yml
Environment=POLL_INTERVAL=15s
ExecStart=/usr/local/bin/edge-agent
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/company-edge/reproxy-config

[Install]
WantedBy=multi-user.target
```

Change the `PANEL_URL` to the correct central API URL and confirm that the TLS certificate SAN matches it. Then enable/start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now company-edge-agent.service
sudo systemctl status --no-pager company-edge-agent.service
sudo journalctl -u company-edge-agent.service -n 100 --no-pager
```

### 7. Verify enrollment

- In the central panel's **Proxy Nodes** page, confirm that the node becomes `online` and its last-seen time is recent.
- Confirm that `Last Config Hash` is populated.
- On the node, inspect Agent logs for `configuration synchronized`.
- Confirm that Reproxy loads the updated config and that a real request reaches the intended origin.
- Stop/restart only a designated staging origin to test failover, then restore it and verify failback. Never run destructive failover tests against production traffic without an approved maintenance plan.

If a node remains offline, see [Troubleshooting](#troubleshooting).

## Production deployment runbook

Use the following phased rollout rather than deploying all nodes at once.

### Phase A — Central control plane

1. Deploy the panel on a stable internal host.
2. Configure strong administrator credentials and trusted TLS.
3. Keep admin UI access on loopback/SSH until the LAN HTTPS listener is implemented and reviewed.
4. Verify the node API has valid TLS and is network-restricted.
5. Back up the JSON store and secret material according to company policy.

### Phase B — First Proxy Node

1. Register one staging node and store the token securely.
2. Install and trust the correct CA certificate on that node.
3. Install the Agent as a systemd service.
4. Mount the Agent-managed configuration file read-write only to the Agent and read-only to Reproxy.
5. Test config fetch, hash verification, reload, heartbeat, reboot persistence, and network interruption recovery.
6. Verify failover and failback without impacting production users.

### Phase C — Additional nodes

1. Register each node with its own unique name and token.
2. Repeat the secure Agent installation procedure.
3. Confirm that node-level domain assignment requirements are understood. Current behavior sends every active domain to every enabled node.
4. Add external load distribution or DNS/routing for end-user traffic across Proxy Nodes. A central management panel does not itself distribute incoming traffic among multiple Reproxy servers.
5. Perform a staged rollout with one canary node before expanding to the rest.

### Phase D — Go-live acceptance

Do not consider the system production-ready until all items below are checked:

- [ ] Use company-approved certificates; no `mkcert` development certificates for production users.
- [ ] Confirm certificate renewal and expiry monitoring.
- [ ] Decide/implement secure LAN access to the admin UI; keep port 8090 off public interfaces.
- [ ] Validate firewall behavior on the actual network, including Docker's `DOCKER-USER` path.
- [ ] Complete a second-node end-to-end test and a reboot/reconnect test.
- [ ] Prove that bad or unavailable config does not overwrite the last known-good config.
- [ ] Prove that Reproxy reliably reloads updated config and that rollback works.
- [ ] Establish node revocation/token rotation and credential recovery procedures.
- [ ] Establish backups and a tested restore of `control-panel/data/store.json`.
- [ ] Add monitoring/alerting for stale heartbeats, invalid config, certificate expiry, disk exhaustion, and control-plane unavailability.
- [ ] Decide how active domains are assigned to nodes and how changes are rolled out.
- [ ] Test HA/recovery of the central control plane; the current JSON store is single-instance local state.
- [ ] Review rate limiting, CSRF/session behavior, audit integrity, and administrator access management.
- [ ] Replace test origins and placeholders; verify real application owners approve the failover behavior.

## Validation and testing

### Control-panel and Agent Go checks

From the repository root:

```bash
docker run --rm -v "$PWD/control-panel:/src" -w /src golang:1.26-alpine \
  sh -c 'gofmt -w main.go nodes.go nodes_runtime.go agent/main.go && go test ./...'
```

The current control-panel and Agent packages report `[no test files]`; this verifies formatting/build compatibility and does not provide behavioral test coverage. Add unit/integration tests before production release.

### Compose validation

```bash
docker compose -f docker-compose.yml config -q
docker compose -f compose.control-panel.yml config -q
```

### Local TLS/API checks

```bash
curl -fsS --max-time 5 http://127.0.0.1:8090/api/health
curl --cacert runtime-secrets/rootCA.pem \
  --output /dev/null --write-out 'HTTP %{http_code}\n' \
  --max-time 5 https://10.20.45.252:8444/api/node/config
```

The second command should return `401` without a token. Do not use `-k` for certificate verification tests outside an isolated lab.

### Reproxy routing and failover (lab only)

The current lab uses `test-origin` on host port 3000 and `test-origin-backup` on port 3001. A lab request has been exercised with:

```bash
curl -skS --resolve demo.example.com:8443:127.0.0.1 \
  https://demo.example.com:8443/
```

Expected bodies during the lab test are `PRIMARY ORIGIN - company-edge` or `BACKUP ORIGIN - company-edge`, depending on the selected mode/health state. The `-k` flag is for this development certificate only. Do not stop production origins as a routine test.

## Operations, logs, and backups

### Service status

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
docker logs --tail 100 company-reproxy
docker logs --tail 100 company-control-panel
sudo systemctl status --no-pager company-edge-node-api-firewall.service
sudo iptables -vnL DOCKER-USER --line-numbers
sudo iptables -vnL COMPANY_EDGE_NODE_API --line-numbers
```

On a node running the native Agent:

```bash
sudo systemctl status --no-pager company-edge-agent.service
sudo journalctl -u company-edge-agent.service -n 100 --no-pager
```

### Restart / rebuild

Rebuild only the central panel when its code or static UI changes:

```bash
docker compose -f compose.control-panel.yml up -d --build control-panel
```

Reproxy configuration files are bind-mounted. Still, verify the running Reproxy's logs and the resulting behavior after every routing change. Avoid unnecessary restarts of the traffic-serving proxy during peak traffic.

### Back up the control-plane store

The persistent store is `control-panel/data/store.json` inside the project directory (mounted as `/app/data/store.json` in the container). Back it up to a protected location; it can contain infrastructure metadata and audit information:

```bash
sudo install -d -m 0700 /var/backups/company-edge
sudo install -m 0600 control-panel/data/store.json \
  "/var/backups/company-edge/store-$(date +%Y%m%d-%H%M%S).json"
```

Back up certificates/secret material separately using an approved encrypted backup process. Do not place private keys or node tokens in a normal source archive.

### Restore the store

1. Schedule maintenance and stop the panel to avoid concurrent writes.
2. Copy the selected verified backup back to `control-panel/data/store.json`.
3. Restore ownership/permissions to match the panel container's mounted data directory (the current container process uses UID 100 and GID 101).
4. Start the panel and verify domain/node/audit records.
5. Verify token and configuration behavior before returning to service.

A restore procedure must be tested on a staging instance before it is relied on for production.

## Security guidance

- Keep `control-panel/.env`, node tokens, `runtime-secrets/`, TLS private keys, CA private keys, and persistent data out of Git and container build context.
- Review `.dockerignore`; it should exclude `control-panel/data/`, `control-panel/.env`, `certs/`, `runtime-secrets/`, and `dist/` from the build context as applicable. Bind mounts still work when a directory is excluded from the Docker build context.
- Never expose `127.0.0.1:8090` by binding the admin panel to a public interface. Current browser access uses an SSH tunnel.
- Limit `8444` to the node network and verify both UFW and Docker-specific iptables rules.
- Use HTTPS and validate the certificate on Agent requests. Never disable verification just to make a connection succeed.
- Use unique high-entropy tokens per node and rotate credentials if a token is exposed. Raw tokens should not be logged.
- Share only the public CA certificate with nodes. Never distribute the CA private key.
- Restrict secret-file permissions and use dedicated service users with only the required filesystem access.
- Keep host OS, Docker Engine, Compose, Go build images and Reproxy updated according to a controlled patch process. Pin and test versions before promotion.
- Use internal source repositories and signed/reviewed releases for production builds; do not deploy from an unreviewed working tree.
- Add a policy for administrator access, security updates, log retention, incident response, and certificate rotation.

## Troubleshooting

| Symptom | Likely cause | Checks / remediation |
|---|---|---|
| Panel login redirects or returns `401` | Missing/incorrect session or credentials | Check `control-panel/.env`, browser session and `docker logs company-control-panel`. Restarting the panel clears in-memory sessions. |
| Panel container does not start | Missing env vars, file permissions, or invalid TLS paths | Review `docker logs company-control-panel`; verify `CONTROL_PANEL_ADMIN_USER`, `CONTROL_PANEL_ADMIN_PASSWORD`, and TLS file paths/permissions. |
| Node API returns `401 Unauthorized node` | Missing, malformed, unknown, disabled, or rotated token | Verify the node token file locally and `Authorization: Bearer` handling. Never print the token in shared logs. |
| Agent reports certificate verification error | CA not trusted by the operating system, certificate SAN mismatch, expired cert, or wrong URL | Verify certificate subject/SAN/expiry; securely install the correct public CA cert; make `PANEL_URL` match the SAN. Never work around by disabling TLS verification. |
| Agent cannot connect to `10.20.45.252:8444` | Routing, host firewall, Docker firewall, or wrong IP | Test TCP connectivity from the node; inspect UFW, `DOCKER-USER`, and `COMPANY_EDGE_NODE_API`; confirm the central IP is reserved. |
| Node remains `offline` | Agent not running, wrong token, bad CA trust, or cannot reach API | Check Agent's `systemctl` status and journal, then central panel logs and node API reachability. |
| Node reports `error` | Config fetch/validation/install or heartbeat error | Inspect the Agent logs. The last error is a status message, not a substitute for host logs. |
| Reproxy serves old or no routes | Domain not active, invalid origins/health URLs, file path mismatch, or reload issue | Check `reproxy-config/reproxy.yml`, activation status, Reproxy logs and Agent `CONFIG_PATH`; compare config hashes. |
| `go build ./agent` says output `agent` already exists | Go treats the default output name as conflicting with the `agent/` directory | Use `go build -o dist/edge-agent ./agent`. |
| Docker build cannot read `control-panel/data` | Runtime data has restrictive permissions and is in build context | Ensure `.dockerignore` excludes `control-panel/data/`; do not broaden database permissions just to build an image. |
| Firewall service is active but node API is unreachable | Wrong interface/subnet, Docker chain not installed, or rule order issue | Inspect `systemctl status company-edge-node-api-firewall` and both iptables chains. Confirm the `RETURN` for the intended subnet precedes `DROP`. |

When diagnosing, share error messages and redacted logs only. Remove passwords, raw tokens, private keys, cookies, and private application data before sharing output.

## Known limitations and production gate

The following are known limitations of the current development state; plan them explicitly before a production launch:

1. **Admin UI LAN HTTPS is not configured yet.** The panel is currently mapped to `127.0.0.1:8090`, accessible by SSH tunnel. The `8444` listener is for the node API, not the admin UI.
2. **Health endpoint remains in preview mode.** It reports `active: false` / `authenticated-preview`; this must be deliberately replaced with a truthful readiness model before a production release.
3. **Remote-node end-to-end testing is pending.** Compile success is not proof of working TLS trust, config updates, Reproxy reload, heartbeat, reconnect, or failure recovery on another machine.
4. **No per-node domain selection.** Every enabled node currently receives all active domains.
5. **Single local JSON store.** This is not a clustered database and does not provide control-plane high availability or multi-writer coordination.
6. **Session storage is in memory.** Restarting the panel invalidates all login sessions. Administrator management is currently based on one configured user/password, not full RBAC.
7. **Test certificates and origins are present in the lab configuration.** Replace them and verify firewall rules before serving real traffic.
8. **Health/failover behavior needs automated tests.** Control-panel and Agent packages currently report no test files. Add tests for token auth, malformed payloads, persistence, config publication, hash checking, and failover/failback.
9. **Traffic distribution across Proxy Nodes is external.** The central panel distributes configuration; it does not itself serve as a global load balancer for incoming traffic.
10. **Secrets in Docker build context need review.** Ensure `.dockerignore` excludes all runtime secrets, private certificates, runtime data, and built artifacts.

Treat the system as a controlled staging prototype until all relevant items in [Production deployment runbook](#production-deployment-runbook) and the go-live acceptance checklist have passed and are reviewed by the service owner.

## License and attribution

See the repository's `LICENSE` file for the applicable license. The repository includes upstream Reproxy code and Company Edge-specific control-plane/Agent changes; preserve upstream notices and confirm licensing/redistribution obligations before packaging or commercial distribution.
