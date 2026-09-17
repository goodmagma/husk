<img src="assets/icon.svg" alt="Husk logo" width="96" align="right">

# Husk

Finds the folders left behind by uninstalled programs. Report only: nothing is ever deleted.
Windows, Linux, macOS. Two executables: `husk` (CLI) and `husk-gui` (Fyne). Version 0.1.0.

![husk-gui](docs/images/husk-gui.png)

> **Disclaimer:** use at your own risk. Husk only reports what it finds, and its results can be wrong:
> always check a folder before deleting it. The authors accept no responsibility for any loss of data
> or damage resulting from the use of this software or of its reports.

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

Icon: `assets/icon.svg` (window icon and logo); after changing it, regenerate the PNG used for packaging:

```bash
go run ./tools/svg2png assets/icon.svg assets/icon.png 256
```

Version: `internal/version/version.go` (keep `cmd/husk-gui/FyneApp.toml` in sync), or at build time:

```bash
go build -ldflags "-X github.com/goodmagma/husk/internal/version.Version=0.2.0" -o dist/husk ./cmd/husk
```

## Usage

```bash
dist/husk                         # scan, print the report to stdout (progress on stderr)
dist/husk -v --show all           # every status, with match and notes
dist/husk --report                # also write the HTML/CSV files and open the HTML page
dist/husk --report --out report --no-open
dist/husk --help                  # all options: --days, --min-mb, --all, --workers, ...
```

Report files (`--report`): each scan creates a `husk_<date>_<time>` folder inside `--out`
(default: the system temp folder) with `husk_report.html`, `husk_report.csv`, `husk_programs.csv`,
`husk_path.csv` and `husk_suggestions.toml`.
`husk-gui` always writes them and shows the same text as the CLI in its log.

## Local configuration

- `dictionary/{windows,linux,darwin}.toml`: built-in dictionary, embedded in the executables.
- `apps.user.toml`, `ignore.txt`: personal entries and exclusions, read from the executable folder and from
  `%APPDATA%\husk` (Windows), `~/.config/husk` (Linux), `~/Library/Application Support/husk` (macOS).
  Not tracked in the repository.

## Checks

```bash
gofmt -l .
go vet ./...
go test ./...

# other systems: the GUI needs CGO, so it can only be checked on the target system
GOOS=linux go vet ./dictionary ./internal/... ./cmd/husk
GOOS=darwin go vet ./dictionary ./internal/... ./cmd/husk
```

## Release

GitHub Actions (`.github/workflows`):

- `ci.yml`: on every push to `main` and pull request, `gofmt`, `go vet`, `go test` and build on Windows, Linux, macOS.
- `release.yml`: on a `v*` tag, builds and publishes a GitHub release:
  - `husk_<version>_<os>_<arch>`: CLI for Windows, Linux and macOS (amd64, arm64);
  - `husk-gui_<version>_<os>_<arch>`: GUI packaged with `fyne package` (Windows `.exe` with icon,
    Linux `.tar.xz` with desktop entry, macOS `.app`), Windows amd64, Linux amd64/arm64, macOS amd64/arm64;
  - `SHA256SUMS.txt`.

  The version comes from the tag. A manual run (Actions → Release → Run workflow) builds the packages
  without publishing them.

```bash
git tag v0.1.0
git push origin v0.1.0
```

Local GUI package (the output is `cmd/husk-gui/Husk.exe`; `fyne package` also bumps `Build` in `FyneApp.toml`):

```bash
go install fyne.io/tools/cmd/fyne@v1.7.2
fyne package --os windows --src cmd/husk-gui --release
```

## Disclaimer

This software is provided "as is", without warranty of any kind, express or implied. Use it at your own
risk. The authors are not liable for any claim, damage or data loss arising from its use, including
the deletion of folders listed in its reports.
