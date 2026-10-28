# Orca Remote CLI

Agents that run inside the GoClaw Docker sandbox can drive an Orca runtime on another
machine (a developer workstation, a VM, a build box) through the `orca` CLI. Worktrees,
terminals, and coding agents (Claude Code, Codex, ...) launched this way run on the Orca
machine; the GoClaw agent stays isolated in its container and only holds a revocable
pairing credential.

```
GoClaw sandbox container ── orca <cmd> (ORCA_PAIRING_CODE) ── ws://<orca-host>:6768 ── orca serve
                                                                                     └─ worktrees, terminals, agents
```

This reuses the Secure CLI credential path ([19-credentialed-exec.md](./19-credentialed-exec.md)):
the `orca` preset uses the passthrough adapter, so the pairing code is injected per call with
`docker exec -e` and scrubbed from output.

## 1. Orca machine

Run the Orca runtime where the work should happen and make the port reachable only over a
private network (Tailscale, WireGuard, LAN). Do not expose it to the public internet.

```bash
orca serve --port 6768 --pairing-address <tailscale-or-lan-ip>
```

On a desktop that already runs the Orca app, create the access link from
Settings → Remote Orca Servers instead. `serve` prints a `Pairing URL: orca://pair?code=...`.
Each client gets its own token; revoke it from Settings → Remote Orca Servers →
Shared Server Access. Do not pass `--grant-desktop-control`.

A paired client has full control of that runtime, including running commands in its
terminals. Prefer a dedicated machine or VM for agent work over a personal workstation, and
use one pairing code per agent or user so access can be revoked independently.

## 2. Images

Both images need the CLI at `/usr/local/bin/orca`: the gateway resolves the binary path
locally before running it inside the sandbox.

```bash
docker build --build-arg ENABLE_ORCA_CLI=true -t goclaw-sandbox:bookworm-slim -f Dockerfile.sandbox .
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.sandbox.yml -f docker-compose.orca.yml up -d --build
```

`ENABLE_ORCA_CLI` extracts only the CLI bundle (`out/cli/index.js`) from the pinned,
SHA-256-verified `orca-ide` `.deb` release and runs it on Node.js. Override
`ORCA_CLI_VERSION` and `ORCA_CLI_SHA256` together to upgrade; keep the CLI version in step
with the Orca server.

`docker-compose.orca.yml` sets `GOCLAW_SANDBOX_NETWORK=true`. To limit network access to
specific agents, leave the global default off and enable `network_enabled` in each agent's
sandbox settings instead. The sandbox container must be able to route to the Orca host
(for Tailscale, run `tailscaled` on the Docker host; the in-process `tsnet` overlay only
covers the gateway process).

## 3. Credential

1. Web UI → CLI Credentials → add from the `orca` preset.
2. Store the pairing URL as the per-user `ORCA_PAIRING_CODE` credential.
3. Grant the binary to the agents that should use it.

The preset blocks commands that would retarget or leak the connection or touch the Orca
machine's desktop: leading flags (put the subcommand first), `--pairing-code`,
`--environment`, `serve`, `open`, `environment`, `account`, `computer`, `diagnostics`,
`skills install|update|share`, and `orchestration reset`.

## 4. Verify

Ask the agent to run `orca status --json`. `runtime.state` should be `ready` and
`target.environment` should be `pairing-code`. The bundled `orca-cli` skill covers the
remaining commands.
