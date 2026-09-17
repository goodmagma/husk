# Husk

Finds the folders left behind by uninstalled programs. Report only: nothing is ever deleted.
Windows, Linux, macOS. Two executables: `husk` (CLI) and `husk-gui` (Fyne).

## Requirements

- Go 1.24+
- `husk-gui` only: a C compiler (CGO) and, on Linux, the OpenGL/X11 development libraries

```bash
# Windows
winget install GoLang.Go
winget install BrechtSanders.WinLibs.POSIX.UCRT

# Debian/Ubuntu
sudo apt install golang gcc libgl1-mesa-dev xorg-dev

# macOS
xcode-select --install
brew install go
```

Open a new terminal after installing, then check:

```bash
go version
gcc --version
```

## Build

```bash
go mod download

# CLI (pure Go, builds for any OS)
go build -o dist/husk ./cmd/husk

# GUI (the first build takes a few minutes)
go build -o dist/husk-gui ./cmd/husk-gui
```

On Windows: add `.exe` to the output names and `-ldflags "-H=windowsgui"` to the GUI build to hide the console.

CLI for other systems:

```bash
GOOS=linux GOARCH=amd64 go build -o dist/husk-linux ./cmd/husk
GOOS=darwin GOARCH=arm64 go build -o dist/husk-macos ./cmd/husk
```

The GUI must be built on the target system.

## Usage

```bash
dist/husk --out report            # scan, write the report to ./report, open the HTML
dist/husk --help                  # options: --days, --min-mb, --all, --no-open, --workers
```

Output: `husk_report_*.html/csv`, `husk_programs_*.csv`, `husk_path_*.csv`, `husk_suggestions_*.toml`.

## Local configuration

- `dictionary/{windows,linux,darwin}.toml`: built-in dictionary, embedded in the executables.
- `apps.user.toml`, `ignore.txt`: personal entries and exclusions, read from the executable folder and from
  `%APPDATA%\husk` (Windows), `~/.config/husk` (Linux), `~/Library/Application Support/husk` (macOS).
  Not tracked in the repository.

## Checks

```bash
gofmt -l .
go vet ./...

# other systems: the GUI needs CGO, so it can only be checked on the target system
GOOS=linux go vet ./dictionary ./internal/... ./cmd/husk
GOOS=darwin go vet ./dictionary ./internal/... ./cmd/husk
```
