# Contributing to Spofly

Contributions, bug reports, and feature ideas are very welcome!

## Local Setup

1. Install **Go** (1.26+), **Bun** (or Node.js 20+), and the **Wails v2 CLI**:
   ```bash
   go install github.com/wailsapp/wails/v2/cmd/wails@latest
   ```
2. Install frontend dependencies:
   ```bash
   cd frontend && bun install
   ```
3. Start live development mode:
   ```bash
   wails dev
   ```

## Before Opening a Pull Request

Run frontend type checks and Go unit tests:

```bash
cd frontend && bun run check && bun run build && cd ..
go test -v ./...
```

