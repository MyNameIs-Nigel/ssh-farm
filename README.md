# ssh-farm

```bash
ssh ssharcade.dev
```

**Select FARM in the lobby to play.** Your SSH public key identifies your saved
farm. A terminal with 24-bit color and a 100×30 window is recommended.

![ssh-farm running in a terminal](docs/farm.png)

Plant crops, trade, buy land, and rebirth for permanent upgrades. Keyboard and
mouse controls, leaderboards, seasonal themes, and an endgame campaign.

[![CI](https://github.com/MyNameIs-Nigel/ssh-farm/actions/workflows/ci.yml/badge.svg)](https://github.com/MyNameIs-Nigel/ssh-farm/actions/workflows/ci.yml)
[![Release](https://github.com/MyNameIs-Nigel/ssh-farm/actions/workflows/release.yml/badge.svg)](https://github.com/MyNameIs-Nigel/ssh-farm/actions/workflows/release.yml)

## Built and shipped on AWS

I built ssharcade to learn how to take games from source code to a live service.
Farm runs alongside two other games and a shared SSH lobby on one EC2 instance,
which costs me less than $10/month for compute.

I chose SSH because it's an unusual way to play, easy to reach from a terminal,
and already has public-key authentication built in.

## Architecture

```mermaid
flowchart TD
    Player["SSH client"]
    Browser["Web browser"]

    subgraph EC2["AWS EC2"]
        Lobby["Arcade lobby"]
        Farm["ssh-farm"]
        DB[("SQLite volume")]
        Caddy["Caddy"]

        Lobby -->|"Private Docker network · player identity"| Farm
        Farm --> DB
    end

    Player -->|"Host :22 → container :2222"| Lobby
    DB -->|"Litestream"| S3[("S3 replica")]
    Browser -->|"80 / 443"| Caddy
    Caddy -->|"Redirect"| Website["Website on Vercel"]
```

Only the lobby exposes an SSH port. Caddy handles web traffic separately.
[ssh-arcadelobby](https://github.com/MyNameIs-Nigel/ssh-arcadelobby/tree/main/deploy)
owns the fleet's Compose configuration.

## CI/CD

Publishing a GitHub Release deploys the game automatically. **GitHub Actions
uses OIDC to assume an AWS role with temporary credentials**, then sends the
deployment command to EC2 through **AWS SSM**. No stored AWS access keys or SSH
connection from the runner.

- **Verify:** shared CI/release checks, Linux and Windows race tests, coverage
  gates, secret scanning, and vulnerability checks.
- **Publish:** a GHCR image with an SBOM, provenance attestation, and image scan.
- **Deploy:** pull the exact image digest, acquire a fleet-wide lock, recreate
  Farm, and check its SSH version banner. Attempt rollback if startup fails.

The deploy job takes about **28 seconds** in my runs, after tests and the image
build. Players briefly return to the lobby during container replacement and can
rejoin with their saved progress. The data volume stays in place.

[Release workflow](.github/workflows/release.yml) ·
[Deployment script](scripts/deploy/remote-deploy.sh) ·
[Release and recovery runbook](docs/releasing.md)

## Stack and docs

Go · Wish · Bubble Tea · Lip Gloss · SQLite · Litestream · Docker Compose

[Project docs](docs/README.md) · [MIT license](LICENSE) · [Third-party notices](NOTICE)

## Local development

Use the Go version in [go.mod](go.mod):

```bash
go build ./...
go vet ./...
go test ./...
FARM_LISTEN_HOST=127.0.0.1 FARM_LISTEN_PORT=2222 go run ./cmd/ssh-farm
```

Connect from another terminal:

```bash
ssh -p 2222 localhost
```

For a local server with test controls on port 2222:

```bash
go run ./cmd/dev-farm
ssh -p 2222 -o StrictHostKeyChecking=accept-new localhost
```

Press **Ctrl+D** to open or close dev tools. Use ↑/↓ to select a row, type an
exact non-negative integer and press Enter to save it, or use ←/→ to adjust it.
You can edit coins, Starseeds, run earnings, and lifetime rebirths; mature every
planted crop; spawn a parcel; cycle events (including off); or unlock contracts.
Click to select a row, double-click to activate an action, or scroll to select.
Mature auto-harvest plots are collected by the next normal game tick.

Coins and Starseeds change only the balance, so you can test purchases without
also changing earnings or contract progress. Run earnings also raise lifetime
earnings when needed and let you test crop gates and rebirth eligibility.
Lifetime rebirth edits leave contract-local rebirth progress alone. Contract
restrictions still apply to parcels and events; unlocking contracts opens the
campaign without granting seals or cosmetic rewards.

The dev command binds to `127.0.0.1`, disables idle kicks, logs at debug level,
and keeps its database and SSH host key in gitignored `var/dev/`. Changes persist
across reconnects. Every `FARM_*` default can be overridden by setting that
environment variable before launch, including `FARM_LISTEN_PORT`.
`FARM_DEV_MODE` defaults to false on the regular server; never enable it for
production or point a dev server at a real player database.

Saves and the host key live in `var/` by default. Local moderation uses a fixture
and generated farm names; see [moderation configuration](docs/gameplay/03-farm-names-and-moderation.md).
For seasonal previews, add `FARM_DEV_SEASON=halloween` or
`FARM_DEV_SEASON=christmas` to the server command.
