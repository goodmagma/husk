# Contributing

The most useful contribution is teaching Husk about a program: which folders and files it creates and how
to tell it is installed. That knowledge lives in the dictionary, one file per system:
`dictionary/windows.toml`, `dictionary/linux.toml`, `dictionary/darwin.toml`.

There are two ways to add a program:

- **Open an issue** with the [Add a program to the dictionary](https://github.com/goodmagma/husk/issues/new?template=new-program.yml)
  form. No TOML needed: a maintainer writes the entry.
- **Open a pull request** that adds the entry yourself, as described below.

For bugs and other changes, open a normal issue or pull request.

## Dictionary entries

### A program: `[[app]]`

```toml
[[app]]
name = "Foo Editor"
category = "development"
detect.names = ["Foo Editor*"]
detect.exe = ["foo"]
detect.files = ['%LOCALAPPDATA%\Programs\Foo\Foo.exe']
paths = [
  { path = '%APPDATA%\Foo', kind = "config" },
  { path = '%LOCALAPPDATA%\Foo', kind = "cache" },
  { path = '%USERPROFILE%\.foo', kind = "data" },
  { path = '%USERPROFILE%\foo_*.log', kind = "logs" },
]
notes = "Projects are saved in .foo\\projects."
clean = "foo cache clean"
```

| Key | Required | Meaning |
|---|---|---|
| `name` | yes | Display name, unique in the file. |
| `category` | yes | `ai`, `browser`, `communication`, `development`, `driver`, `games`, `multimedia`, `utilities`. |
| `detect.names` | at least one `detect` | Patterns (`*`, `?`, case-insensitive) matched against installed program names: registry, Start menu, Store and driver names on Windows; packages and `.desktop` files on Linux; apps, `pkgutil` and Homebrew on macOS. Running processes count too. |
| `detect.exe` | | Commands looked up in PATH, without extension. |
| `detect.files` | | Files or folders that exist only while the program is installed. |
| `paths` | yes | Folders and files the program creates, each with a `kind`. |
| `notes` | | Short, useful facts (English, full sentences). |
| `clean` | | The command that cleans up safely, if the program has one. |

`kind` says what a path holds:

| `kind` | Content |
|---|---|
| `app` | Program files |
| `config` | Settings |
| `cache` | Data that is recreated when missing |
| `models` | Downloaded models (AI tools) |
| `logs` | Logs: always listed as disposable, even when the program is installed |
| `dump` | Crash dumps: as above |
| `data` | User data (documents, projects, saves): the report warns before deleting it |

A single path can be written as `path = '...'` plus `kind = "..."` instead of `paths`.

### A shared folder: `[[shared]]`

Folders used by several programs (a model cache, a library cache, `~/.local/bin`). Husk lists them apart and
never calls them orphans.

```toml
[[shared]]
name = "Hugging Face cache"
path = '%USERPROFILE%\.cache\huggingface'
kind = "models"
used_by = "Python libraries (transformers, diffusers), ComfyUI, WebUI and others"
clean = "huggingface-cli delete-cache"
```

`used_by` is required; `detect` is not allowed.

### Paths

- **Windows:** start from a variable (`%USERPROFILE%`, `%APPDATA%`, `%LOCALAPPDATA%`, `%ProgramData%`,
  `%ProgramFiles%`, ...), use `\` and single quotes, so backslashes need no doubling.
- **Linux and macOS:** start with `~/`, `/` or a `$VARIABLE`, use `/`.
- **Never** a real user name (`C:\Users\mario`, `/home/mario`, `/Users/mario`).
- Wildcards: `*`, `?`, `[...]`. Files matching one pattern are reported as one row (`jcef_*.log`).
- Prefer the first-level folder under the scanned roots (`%APPDATA%\Foo`, not `%APPDATA%\Foo\Cache`):
  deeper folders are reported on their parent.

Scanned roots: Program Files, ProgramData, `AppData\{Roaming,Local,LocalLow}`, `AppData\Local\Programs`, the
user profile, `.cache`, `.config`, `.local\share` (Windows); `/opt`, `~/.local/opt`, the dot folders of the
home folder, `~/.config`, `~/.local/share`, `~/.local/state`, `~/.cache`, `~/.var/app` (Linux); `/Applications`,
`~/Applications`, `/Library/Application Support`, `~/Library/{Application Support,Caches,Logs,Preferences,
Containers,Group Containers}` (macOS).

### Rules

- English only, as in the rest of the program.
- One entry per program. To add paths to an existing program, extend its entry.
- Only paths every installation creates: personal paths (a program unzipped in `D:\Tools`) go in your own
  `apps.user.toml`, not in the dictionary.
- Keep entries in their section (AI, Shared folders, Development, Browser, Communication, Games,
  Multimedia and graphics, Utilities).

## Test an entry locally

1. [Build Husk](README.md#build) or download a [release](https://github.com/goodmagma/husk/releases).
2. Put the entry in `apps.user.toml`, next to the executable or in `%APPDATA%\husk` (Windows),
   `~/.config/husk` (Linux), `~/Library/Application Support/husk` (macOS).
3. Run `husk -v --show all` and check the rows of the program: `associated` while it is installed,
   `orphan` after uninstalling it, `shared` for a `[[shared]]` entry. With `-v` each row shows why.
4. Move the entry to `dictionary/<system>.toml` and run the checks:

```bash
gofmt -l .
go vet ./...
go test ./...
```

`go test ./dictionary` checks every entry: known keys, `category` and `kind` values, `detect` rules,
path style per system, no user names, valid wildcards, unique names. The CI runs it on every pull request.
