# onibi

onibi (鬼火) - _**means will-o'-the-wisp🔥 in Japanese**_ - is a small TUI that
shows the processes haunting your ports — and lets you kill them.

Forgot to stop a `pnpm dev` from a previous session and now `EADDRINUSE` keeps
biting you? `onibi` keeps every listening process visible so a forgotten ghost
never surprises you again.

- Single Go binary. `go build` and go — light, no runtime deps.
- macOS / Linux (uses `lsof`, falls back to `ss` / `netstat`).
- Shows each listening process and its **uptime**.
- Long-lived (likely forgotten) processes are color-coded: yellow after 2h,
  red after 8h.
- Kill the selected process with a confirmation step; `--dry-run` to preview.
- Headless `--list` / `--json` / `--kill` modes for scripting and AI agents.

## Setup

```sh
git clone <this-repo> onibi && cd onibi
go mod tidy      # fetch Bubble Tea / Lip Gloss and write go.sum
go build -o onibi .
./onibi
```

Just trying it out: `go run .` works too.

### Install

```sh
go install github.com/You-saku/onibi@latest   # puts onibi in $GOPATH/bin
```

Building from a local clone still works with `go install .`.

### Cross-compile

```sh
GOOS=darwin GOARCH=arm64 go build -o dist/onibi-darwin-arm64 .
GOOS=linux  GOARCH=amd64 go build -o dist/onibi-linux-amd64  .
```

## Usage

### TUI (default)

```sh
onibi
```

| Key | Action |
|-----|--------|
| up/down or j/k | move selection |
| enter | kill the selected process (asks to confirm) |
| / | filter by port / command |
| r | refresh now |
| a | toggle auto-refresh (every 3s) |
| f | toggle SIGKILL / SIGTERM |
| q / Ctrl-C | quit |

The panel under the list shows the selected process's address, pid, uptime,
cwd, and full command line.

### Headless

```sh
onibi --list                 # print the table once and exit
onibi --json                 # JSON output (pipe into jq)
onibi --kill 3000            # kill whatever listens on port 3000 (or a PID)
onibi --kill 3000 --dry-run  # preview only
onibi --kill 3000 --force    # SIGKILL
```

## Dependencies

- [github.com/charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [github.com/charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) — styling

Both are widely used, trusted libraries.
