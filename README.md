# ⚡ VIB — Visual Image Builder

> **A minimalist, web-based visual builder for HashiCorp Packer.**
> Design, inspect, and build custom Amazon Machine Images (AMIs) without writing HCL manually.

---

## 🌟 Features

- **Split-Screen Visual Builder:** Visual UI forms on the left, live Monaco code editor on the right with real-time `UI -> HCL` synchronization.
- **Bi-Directional HCL Sync:** Edit Packer HCL directly in the Monaco editor and sync back to the visual form (`HCL -> UI`) using official HashiCorp AST parsers.
- **Live Build Log Streaming:** Real-time log output streamed to a built-in terminal over Server-Sent Events (SSE).
- **Official Base AMI Discovery:** Automatically queries AWS EC2 APIs for the latest official Ubuntu, Amazon Linux 2023, and Debian releases across any region and architecture (`x86_64` / `arm64`).
- **Encrypted Local Credentials:** AWS credentials are encrypted at rest with AES-256-GCM and injected into Packer only in memory. Supports local `~/.aws/credentials` automatically.
- **AMI Lifecycle & Zero-Orphan Cleanup:** Tracks VIB-created AMIs in local SQLite (`~/.vib/vib.db`) and cleanly deregisters AMIs *and* their associated EBS snapshots in one click.
- **Standalone Single Binary:** Backend engine and embedded React UI shipped as a single ~24MB executable. Zero Node.js runtime required.

---

## 🚀 Quick Start

### Option 1: One-Line Install Script
```bash
curl -fsSL https://raw.githubusercontent.com/<your-username>/vib/main/scripts/install.sh | bash
```

### Option 2: Run via Docker
```bash
docker run -d -p 8080:8080 -v ~/.aws:/root/.aws:ro ghcr.io/<your-username>/vib:latest
```

### Option 3: Build from Source
```bash
# Prerequisites: Go 1.23+, Node.js 20+, Packer CLI
git clone https://github.com/<your-username>/vib.git
cd vib

# Build frontend and compile standalone binary
cd web && npm install && npm run build && cd ..
go build -o bin/vib ./cmd/vib

# Launch
./bin/vib --serve --port 8080
```

Open [http://localhost:8080](http://localhost:8080) in your browser.

---

## 🛠️ Architecture

```text
┌────────────────────────────────────────────────────────┐
│               VIB Single Binary (:8080)                │
│                                                        │
│  [React 19 + Monaco Editor] (Embedded via go:embed)    │
│                           │                            │
│                           │ REST + SSE API             │
│                           ▼                            │
│  ┌──────────────────────────────────────────────────┐  │
│  │ - hclwrite Generator & hclparse AST Engine       │  │
│  │ - AWS SDK v2 (EC2 Describe / STS Verify)         │  │
│  │ - Pure-Go SQLite Store (~/.vib/vib.db)           │  │
│  │ - Process-Group Packer Execution Runner          │  │
│  └────────────────────────┬─────────────────────────┘  │
└───────────────────────────┼────────────────────────────┘
                            │
                  [Packer CLI Engine]
                            │
                 [Amazon Web Services]
```

---

## 📜 License

[Apache-2.0](LICENSE)
