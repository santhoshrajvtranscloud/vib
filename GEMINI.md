# Project: VIB (Visual Image Builder)

## Overview
An open-source, localhost web-based tool that makes HashiCorp Packer easy to use through a graphical UI without writing HCL manually. First version targets AWS (`amazon-ebs`).

## Core Tech Stack
- **Backend:** Go (Golang)
- **HCL Generation & Parsing:** `github.com/hashicorp/hcl/v2` (`hclwrite`, `hclparse`, `hclsyntax`) & `go-cty`
- **Image Engine:** Packer CLI (`packer init`, `packer validate`, `packer build -machine-readable`)
- **API Server & Log Streaming:** Go `net/http` standard library + Server-Sent Events (SSE)
- **Single-Binary Embedding:** Go 1.16+ `//go:embed all:web/dist`
- **AWS Integration:** AWS SDK for Go v2 (`DescribeImages`, `DeregisterImage`, `DeleteSnapshot`)
- **Database:** Local SQLite (`vib.db`) for build history, logs, and AMI-to-HCL mapping (`modernc.org/sqlite`)
- **Frontend:** React 19 + TypeScript + Vite + Tailwind CSS v4 + Monaco Editor (`@monaco-editor/react`) + Lucide Icons

## Project Structure
```text
vib/
├── Dockerfile               # Multi-stage production container (Node + Go + Packer + AWS CLI)
├── embed.go                 # Root embed.go embedding web/dist into standalone Go executable
├── scripts/
│   └── install.sh           # One-command installer for VIB, Packer, and AWS CLI (~/.vib/bin/)
├── cmd/vib/main.go          # CLI & HTTP/SSE Server launcher (--serve)
├── internal/
│   ├── api/                 # HTTP REST & SSE API server + SPA static handler (Milestone 1, 4, 5)
│   │   ├── server.go
│   │   ├── server_test.go
│   │   └── static.go        # Embedded SPA filesystem & local disk dev fallback
│   ├── config/build.go      # JSON struct & validation (Milestone 1)
│   ├── generator/           # HCL generation, bi-directional parsing, & package catalogue (Milestones 1 & 5)
│   │   ├── hcl.go
│   │   ├── parser.go        # HCL AST parser extracting BuildConfig from raw Packer HCL
│   │   └── parser_test.go
│   ├── packer/              # Packer CLI execution & log streaming (Milestone 1)
│   │   ├── runner.go
│   │   └── runner_test.go
│   ├── aws/                 # AWS SDK v2 integration, encrypted credentials, AMI discovery & lifecycle (Milestones 3 & 4)
│   │   ├── credentials.go
│   │   ├── credentials_test.go
│   │   ├── images.go
│   │   ├── images_test.go
│   │   ├── lifecycle.go
│   │   └── lifecycle_test.go
│   ├── store/               # Pure-Go SQLite persistence (~/.vib/vib.db) for builds & AMIs (Milestone 4)
│   │   ├── db.go
│   │   └── db_test.go
├── schemas/build-config.schema.json
├── templates/
└── web/                     # React + TypeScript + Monaco Editor UI (Milestones 2, 3, 4, 5)
    ├── index.html
    ├── vite.config.ts       # Configured with Tailwind CSS and /api proxy
    ├── package.json
    └── src/
        ├── App.tsx          # Split-screen UI coordinator, bi-directional sync, & SSE stream subscriber
        ├── main.tsx
        ├── index.css        # Tailwind v4 theme & custom dark scrollbars
        ├── types.ts         # TypeScript interfaces
        └── components/
            ├── Header.tsx         # Health check, tab switcher, AWS status button
            ├── AWSModal.tsx       # AWS credential modal (STS identity verification, AES-256-GCM storage)
            ├── VisualBuilder.tsx  # Dynamic software catalogue, live Base AMI discovery, & AWS input form
            ├── HclEditor.tsx      # Monaco editor with validate, download, build controls, & "Sync to Form"
            ├── TerminalLogs.tsx   # SSE live log streaming terminal & AMI success banner
            └── AmiDashboard.tsx   # VIB AMI management table, HCL config inspector, & clean deletion
```

## Key Architecture Rules
1. Every AMI built by VIB must include AWS tags: `CreatedBy = "VIB"` and `VIB_Build_ID = "<uuid>"`.
2. Every generated HCL must include `post-processor "manifest"` outputting to `manifest.json` to reliably extract the created AMI ID.
3. AWS credentials must never be hashed (one-way); rely on local `~/.aws/credentials` or AES-256-GCM encryption if stored in `~/.vib/credentials.enc` with `0600` permissions.
4. Deleting a VIB AMI must both `DeregisterImage` AND `DeleteSnapshot` for associated EBS snapshots.
5. Process cancellation signals the entire process group with `syscall.SIGINT` and allows up to 30s `WaitDelay` for graceful AWS resource cleanup by Packer.
6. The compiled binary is fully self-contained: `go build -o vib ./cmd/vib` embeds `web/dist` directly into the executable.

## API Endpoints
- `GET /api/health`: Health status & Packer CLI version check.
- `GET /api/catalogue`: List supported packages (e.g. `docker`, `nginx`, `git`) from `generator.PackageCatalogue`.
- `POST /api/hcl/generate`: Validates incoming `BuildConfig` JSON and returns `{ "hcl": "..." }`.
- `POST /api/hcl/validate`: Accepts `{ "hcl": "..." }`, runs `packer init` + `packer validate`, and returns `{ "valid": true/false, "logs": [...] }`.
- `POST /api/hcl/parse`: Accepts `{ "hcl": "..." }`, parses HCL AST via `generator.ParseHCL`, and returns `{ "config": BuildConfig }`.
- `POST /api/builds`: Accepts either `BuildConfig` or raw HCL, initiates async build in `BuildManager`, persists initial record to SQLite, and returns `{ "build_id": "...", "status": "running" }`.
- `GET /api/builds`: Lists all active and past builds.
- `GET /api/builds/{id}`: Returns summary status of a specific build.
- `GET /api/builds/{id}/stream`: Server-Sent Events (`text/event-stream`) streaming past + live logs (`event: log`) and concluding status event (`event: status`).
- `POST /api/builds/{id}/cancel`: Signals graceful cancellation of a running build.
- `GET /api/aws/status`: Current AWS connection status, masked access key, and STS identity (`Account`, `ARN`).
- `POST /api/aws/credentials`: Validates credentials via STS `GetCallerIdentity`, saves them encrypted via AES-256-GCM, and returns identity info.
- `DELETE /api/aws/credentials`: Clears stored encrypted credentials and reverts to default local profile chain.
- `GET /api/aws/base-amis?region=...&arch=...`: Verified official base AMIs (Ubuntu 24.04/22.04, Amazon Linux 2023, Debian 12), cached for 15 mins with fallback presets.
- `GET /api/images`: Lists VIB-created AMIs by joining live AWS `DescribeImages` (`tag:CreatedBy=VIB`) with local SQLite records.
- `GET /api/images/{id}/config`: Returns `config_json` and `hcl_code` for the specified image.
- `DELETE /api/images/{id}`: Calls `DeleteVIBImage` (`DeregisterImage` + `DeleteSnapshot` for all EBS snapshots) and marks record deleted in SQLite.
- `GET /`: Serves the embedded single-binary React SPA (falling back to local `web/dist` on disk during development).

## Installation & Running

### One-Command Installer
Automates installing VIB, HashiCorp Packer, and AWS CLI v2 into `~/.vib/bin/`:
```bash
./scripts/install.sh
```

### Standalone Single-Binary Execution
Build the standalone executable with the web UI embedded:
```bash
cd web && npm run build && cd ..
go build -o bin/vib ./cmd/vib
./bin/vib --serve --port 8080
```
Open `http://localhost:8080` in your browser.

### Docker Container Mode
Run VIB in a container containing Packer and AWS CLI:
```bash
docker build -t vib:latest .
docker run -p 8080:8080 -v ~/.aws:/root/.aws:ro vib:latest
```

### Development Mode (with hot-reloading)
1. Start the API server on an available port (e.g. 8081):
   ```bash
   go run ./cmd/vib/main.go --serve --port 8081
   ```
2. Start the Vite dev server with proxy:
   ```bash
   cd web
   VITE_API_URL=http://localhost:8081 npm run dev
   ```
3. Open `http://localhost:5173` in your browser.

## Milestone Roadmap & Current Status
- **Milestone 1 (Complete):** Backend core, HCL generator, Packer runner with streaming, and REST/SSE API.
- **Milestone 2 (Complete):** Full localhost split-screen Web UI with React, TypeScript, Monaco Editor, Tailwind CSS, live SSE terminal, and single-port SPA serving.
- **Milestone 3 (Complete):** AWS SDK v2 integration, AES-256-GCM encrypted credential management (`~/.vib/credentials.enc`), STS identity verification, official Base AMI discovery (Ubuntu, Amazon Linux, Debian) with 15-minute in-memory caching and curated fallback presets, active credential injection into Packer runner, frontend AWS connection modal, live Base AMI selector with architecture toggle, and minimalist Black & White monochrome UI theme.
- **Milestone 4 (Complete):** Local SQLite persistence (`vib.db`), VIB-created AMI dashboard, configuration/HCL viewer, and clean AWS AMI + EBS snapshot deletion.
- **Milestone 5 (Complete):** Bi-directional HCL AST parser (`generator.ParseHCL`), `POST /api/hcl/parse` API endpoint, "Sync to Form" toolbar action with visual diagnostic banners, standalone single-binary embedding via `//go:embed all:web/dist`, one-command installer script (`scripts/install.sh`), and multi-stage Dockerfile.
- **VIB MVP Complete!** 🎉 All 5 milestones are fully implemented, verified, and operational.
