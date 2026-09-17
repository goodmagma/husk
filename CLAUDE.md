# Husk

Strumento che trova le cartelle residue dei programmi disinstallati (Program Files, ProgramData,
AppData, cartelle "dot" del profilo). **Solo report**: non cancella mai nulla, decide l'utente.

Nome precedente: Ghostdir. **Nome scelto: Husk** (comando `husk`).

## Regole di lavoro

- Nei commit **mai** `Co-Authored-By: Claude` né altre righe di attribuzione.
- Commit e push **solo quando l'utente lo chiede** (primo commit Go: `15cba54`, 17/09/2026).
- Branch: si lavora su `main` (niente `develop`, decisione del 17/09/2026); le versioni sono i tag `v*`.
  Per modifiche corpose: branch brevi con pull request verso `main` (la CI gira sulle PR).
- Il PoC Python (`poc/`) resta solo libreria standard (3.11+) e legge `dictionary/windows.toml`.
- I dizionari TOML sono condivisi da PoC e versione Go: stesso formato.
- Sorgenti Go con fine riga LF (`.gitattributes`); controllare con `gofmt -l .`.
- **Lingua del programma: inglese** (decisione del 17/09/2026) per codice, commenti, CLI, GUI, report,
  log, dizionari TOML e README. Nessuna traduzione per ora (i18n rimandata). Eccezioni: il PoC in
  `poc/` resta in italiano; questo file e la conversazione con l'utente restano in italiano.

## Stato (17/09/2026)

Fase: **riscrittura in Go** (iniziata il 17/09/2026) dopo il PoC Python validato sul PC dell'utente.
Obiettivo: multipiattaforma (Windows, Linux, macOS), riga di comando e interfaccia grafica.

Scelte dell'utente:
- **due eseguibili**: `husk` (CLI, Go puro, niente CGO) e `husk-gui` (Fyne, serve un compilatore C);
- **Fyne** per la grafica; prima release: la GUI esegue la scansione e apre il report HTML nel
  **browser predefinito** (Fyne non mostra HTML). Grafici non prioritari;
- PoC spostato in `poc/` come riferimento finché la versione Go non lo sostituisce.

Ambiente dell'utente: Go 1.27.1 e GCC 16.1 WinLibs (`BrechtSanders.WinLibs.POSIX.UCRT`), entrambi
con winget. Fyne v2.8.1 nel go.mod. Setup e comandi sono nel `README.md` (stile: pochi comandi, poche righe).

Comportamento (richiesto dall'utente il 17/09/2026): la CLI di default stampa il report completo come un
normale comando (sezioni per stato; `--show` sceglie gli stati elencati, default orphan,suspect,portable,shared;
gli altri stati compaiono solo come riga di riepilogo); i file HTML/CSV solo con `--report`, in
una cartella `husk_<data>_<ora>` dentro `%TEMP%` di default. La GUI scrive sempre i file (default uguale, preferenza
`reportDir`) e nel registro mostra lo stesso testo della CLI.

Stato Go: CLI completa per Windows e verificata sul PC (stessa classificazione del PoC su 285 cartelle,
~10 s); Linux e macOS compilano (`GOOS=linux|darwin go vet ./...`) ma non sono ancora provati.
GUI compilata (prima build ~7 minuti, eseguibile ~43 MB), si avvia; non ancora provata dall'utente.
`GOOS=linux|darwin go vet` funziona solo escludendo `cmd/husk-gui` (serve CGO).

Verifica disponibilità del nome Husk (fatta il 17/09/2026):
- liberi: winget, Homebrew (formula e cask), Snap, Flathub;
- Go: nessun conflitto, il modulo sarà `github.com/<account>/husk`;
- AUR occupato da un vecchio front-end iptables → usare `husk-bin` o `husk-scanner`;
- PyPI, crates.io e npm occupati: non importa, la versione finale è Go.

### File

| Percorso | Cosa |
|---|---|
| `cmd/husk/` | CLI: stampa il report su stdout (avanzamento su stderr); `--report` scrive e apre i file; `--show`, `-v`, `--days`, `--min-mb`, `--all`, `--out`, `--no-open`, `--workers`, `--version` |
| `cmd/husk-gui/` | GUI Fyne (`FyneApp.toml`: ID `io.github.goodmagma.husk`) |
| `dictionary/` | `windows.toml` (ex `apps.toml`), `linux.toml`, `darwin.toml` incorporati con `go:embed`; caricamento, unione con le voci utente, rilevamento |
| `internal/model` | tipi comuni: `Evidence`, `Result`, stati, `PathIssue` |
| `internal/pathutil` | `Expand` (`%VAR%`, `$VAR`, `~`), `Key` (minuscole su Windows e macOS), `Unexpand` |
| `internal/match` | confronto per nome (porting di `Matcher`) |
| `internal/platform` | per sistema: radici, fonti dei programmi, PATH, esclusioni, `OpenURL` (`windows.go`, `linux.go`, `darwin.go`, `unix.go`) |
| `internal/scanner` | scansione parallela, classificazione, controllo del PATH (`pathcheck.go`) |
| `internal/version` | versione del programma (0.1.1), da allineare con `cmd/husk-gui/FyneApp.toml`; sovrascrivibile con `-ldflags -X .../internal/version.Version=...` |
| `assets/` | `icon.svg` (logo: cartella vuota con lente su quadrato arancione; icona della finestra via `go:embed`, logo del README), `icon.png` 256 px per `fyne package` (`FyneApp.toml`). Il renderer SVG di Fyne ignora `rx` sui `rect` e non scala bene gli spessori: disegnare con `path` e rigenerare il PNG con `go run ./tools/svg2png assets/icon.svg assets/icon.png 256` |
| `scripts/build.sh` | compilazione di CLI e GUI (vedi Comandi) |
| `tools/svg2png` | converte l'SVG in PNG (disegna a 256 px e ridimensiona) |
| `.github/workflows/` | `ci.yml` (gofmt, vet, test, build sui 3 sistemi), `release.yml` (tag `v*`: CLI per 6 target senza CGO, GUI con `fyne package` su runner nativi, macOS amd64 cross su Apple silicon, pubblicazione con `softprops/action-gh-release`; avvio manuale = solo artefatti) |
| `.github/scripts/version.sh` | versione dal tag (`v1.2.3` → `1.2.3`, manuale → `<versione>-dev.<commit>`), scritta in `internal/version/version.go` solo nel runner |
| `docs/images/` | immagini del README (`husk-gui.png`: screenshot della GUI, nome utente oscurato) |
| `internal/report` | HTML (`template.html` incorporato), CSV, suggerimenti; `text.go`: report testuale (`WriteText`) usato da CLI e registro della GUI |
| `poc/` | PoC Python: `husk.py`, `ignore.txt`, `apps.user.toml`; solo locale, escluso dal repository (`.gitignore`) |
| `dist/` | eseguibili compilati (non va nel repository) |
| `report/` | output delle scansioni (non va nel repository) |

File personali della versione Go (`dictionary.UserDirs()`): `apps.user.toml` e `ignore.txt` accanto
all'eseguibile e in `os.UserConfigDir()/husk` (Windows: `%APPDATA%\husk`). Per le prove c'è una copia
di `poc/apps.user.toml` in `dist/`.

Comandi (istruzioni complete per l'utente nel README, sezione Build):
- compilazione: `bash scripts/build.sh [cli|gui|all]` (Git Bash; da PowerShell usare
  `"C:\Program Files\Git\bin\bash.exe"`, perché `bash` in System32 è WSL). Variabili: `GOOS`, `GOARCH`,
  `VERSION`, `OUT`, `WINRES=1`. Lo usano anche `ci.yml` e `release.yml`;
- la shell Bash di Claude Code può avere un PATH vecchio: aggiungere `/c/Program Files/Go/bin` e la cartella
  `mingw64/bin` di WinLibs (in `%LOCALAPPDATA%\Microsoft\WinGet\Packages\BrechtSanders.WinLibs...`);
- uso: `dist\husk.exe` (testo) o `dist\husk.exe --report`;
- PoC: `py poc/husk.py --out report`.

Risorse Windows (icona nel file .exe e dettagli del file, dal 17/09/2026): `cmd/husk/winres/winres.json` e
`cmd/husk-gui/winres/winres.json` → `rsrc_windows_{amd64,arm64}.syso` (nel repository, generati con
`go run github.com/tc-hib/go-winres@v0.3.3 make ...`). La GUI **non** ha il manifest: MinGW ne aggiunge
uno suo quando collega con CGO e due manifest fanno fallire il link ("multiple non-default manifests").
`build.sh` rigenera i .syso se `VERSION` è diversa dal codice o con `WINRES=1` (release). `go-winres` va
compilato per l'host (`GOOS=$(go env GOHOSTOS)`): il primo tag `v0.1.1` è fallito nel job CLI su Linux
con "exec format error" perché ereditava `GOOS=windows` (corretto il 17/09/2026, verificato in Docker). In release la GUI
Windows si compila con `build.sh` e non più con `fyne package` (avrebbe creato un secondo .syso).
Icone: `assets/icon.png` (256) e `icon-48/32/16.png`.
Versione attuale **0.1.1** (non ancora rilasciata; 0.1.0 è il tag su `f8e4f36`): va aggiornata in
`internal/version/version.go`, `cmd/husk-gui/FyneApp.toml`, i due `winres.json` e il README.

Fonti per sistema:
- Windows: registro, menu Start, Store (`Get-AppxPackage`), processi (Toolhelp32), PATH, portabili;
- Linux: dpkg, rpm, pacman, Flatpak, Snap, file `.desktop`, `/proc/*/exe`, PATH, portabili in `/opt`
  e `~/.local/opt`; nel profilo solo cartelle "dot"; anche `~/.var/app` (dati Flatpak);
- macOS: bundle `.app` (nome e `CFBundleIdentifier`), `pkgutil --pkgs`, Homebrew, `ps`, PATH;
  radici `~/Library/{Application Support,Caches,Logs,Preferences,Containers,Group Containers}`;
  cartelle `com.apple.*` e bundle `.app` esclusi.
- Su Linux e macOS il controllo del PATH usa il PATH del processo (ambito "processo").

### Logica di classificazione

Radici analizzate (`scan_roots()`): Program Files, Program Files (x86), ProgramData, AppData\Roaming,
AppData\Local, AppData\Local\Programs, AppData\LocalLow, profilo, `.cache`, `.config`, `.local\share`.
Si analizzano le sottocartelle di primo livello.

Programmi installati: registro (chiavi Uninstall HKLM/HKCU, 32 e 64 bit), menu Start,
app dello Store (`Get-AppxPackage`), processi in esecuzione (`Get-Process`, esclusa la cartella di
Windows), eseguibili nel PATH. Le cartelle del PATH senza eseguibili non contano (voci rimaste dopo una
disinstallazione, es. Ollama). Anche i nomi dei processi valgono per `detect.names` del dizionario.
Programmi portabili (`read_portable_programs()`): le cartelle delle aree programmi con `.exe` nei primi
2 livelli forniscono nome cartella e nomi degli eseguibili **solo al dizionario** (non al matcher, sennò
ogni cartella corrisponderebbe a se stessa). Se una di queste fa rilevare una voce, la cartella diventa
`associato` a quella voce invece di `portabile`.

Una voce di apps.user.toml con lo stesso `name` di una integrata la **completa**: `detect.*` e `paths`
si sommano, `category`, `notes` e `clean` (se indicati) sostituiscono quelli integrati. In apps.user.toml
vanno solo le informazioni specifiche del PC (percorsi di installazione da zip, esclusioni).

Stati:
- **Cartella del dizionario**: `condivisa` se è una voce `[[shared]]`; `associato` se il programma
  risulta installato; altrimenti `orfano`, con la nota "cartella vuota" o "modificata di recente"
  (modifiche negli ultimi 90 giorni). **Compare sempre, a qualunque dimensione.**
- **Cartella trovata per euristica**:
  - `associato` se c'è una corrispondenza per nome, editore o percorso di installazione;
  - altrimenti `portabile` se è in un'area programmi e ha `.exe` nei primi 2 livelli
    (installer, disinstallatori e aggiornatori esclusi);
  - altrimenti `orfano` se non è stata modificata da 90 giorni o è vuota (nota "cartella vuota"),
    `sospetto` ("usata di recente") se no.
  - `--min-mb` si applica solo qui (default 0).
- `ignorato`: cartelle di sistema in `BUILTIN_IGNORE`, `[ignore]` di apps.user.toml, ignore.txt.

Matcher per nome (`Matcher.find`):
- corrispondenza esatta sul nome normalizzato;
- somiglianza solo su **parole intere** (`tokens()` + `aligned()`); per le chiavi di almeno 8 lettere
  basta l'inizio di una parola;
- eseguibili del PATH e processi valgono solo con corrispondenza esatta;
- i nomi sotto 3 caratteri non vengono confrontati (es. `mc`): servono voci nel dizionario;
- il suffisso `-updater` viene rimosso prima del confronto;
- le parole troppo generiche (`GENERIC_KEYS`: update, setup, desktop, ...) non contano per la somiglianza.

Voci del PATH (`check_path_entries()`): legge `Path` di utente (HKCU\Environment) e sistema
(HKLM\...\Session Manager\Environment) dal registro e segnala le voci duplicate, inesistenti, senza
eseguibili (valgono PATHEXT, `.dll`, `.ps1`) o dentro una cartella `orfano`.
Nessun elenco fisso nel codice (decisione dell'utente, 17/09/2026): una voce mancante o senza eseguibili
che sta dentro le `paths` di una voce del dizionario (`Dictionary.Owner`, confronto per componenti con
caratteri jolly, vince la voce più specifica) **non** viene segnalata se il programma è installato o la voce
è `[[shared]]` (es. `%USERPROFILE%\go` copre `go\bin`, che nasce al primo `go install`); se il programma non è
installato: "belongs to X, which is not installed". `~/.local/bin` (e `%USERPROFILE%\.local\bin`) è una
voce `[[shared]]` "User executables"; .NET SDK aggiunto anche a linux/darwin.toml.
Test: `internal/scanner/pathcheck_test.go` (`CheckPath`, `Dictionary.Owner`). Sezione in fondo al report
HTML, file `husk_path.csv`, elenco in console.

Scansione dei file (0.1.1, `internal/scanner/files.go`, test in `files_test.go`):
- le `paths` del dizionario possono indicare file con caratteri jolly (`Dictionary.ResolveFiles`); i file di
  uno stesso schema sono **una riga** (`Result.Type = "files"`, `Path` = schema, `Files` = quanti);
- nuovo stato `disposable` ("Disposable files (logs, crash dumps)") per `kind = "logs"` o `"dump"` (nuovo tipo
  "Crash dump"): elencati anche se il programma è installato, senza le note della voce;
- euristica sui file sciolti della cartella del profilo: `.log/.dmp/.mdmp/.hprof/.tmp` → disposable,
  altri file ≥ 100 MB (`largeFile`) → orphan se vecchi, suspect se recenti; nomi uguali a meno delle cifre
  raggruppati (`jcef_1234.log` → `jcef_*.log`); esclusi `platform.IsSystemFile` (ntuser.*, desktop.ini, ...);
- voci: JetBrains (`jcef_*.log`, `java_error_in_*.hprof/.log`), `[[shared]]` "Java crash files"
  (`hs_err_pid*.log`, `replay_pid*.log`, `java_pid*.hprof`), Aider (`.aider.conf.yml`);
- sul PC dell'utente (17/09/2026): `java_error_in_phpstorm.hprof` 2,4 GB (PhpStorm, maggio 2025), 26
  `jcef_*.log` vuoti, `.aider.conf.yml` orfano; `global.yml` vuoto non segnalato (origine ignota).

Report HTML: filtri per stato, menu "Minimum size" (default "all", prima 100 MB: nascondeva i piccoli residui del dizionario), colonne ordinabili.
In console: i primi 15 orfani per dimensione.

### Risultati verificati sul PC dell'utente

Veri orfani confermati dall'utente o da controlli:
- VS Code (`Roaming\Code`, `.vscode`), Rust (`.rustup`), Cline (`.cline`);
- Winhance (`ProgramData\Winhance`, era portabile), Adobe (`LocalLow\Adobe`), PhotoSì (`Roaming\PhotoSi`);
- AnythingLLM, Aider (disinstallato il 17/09 con `uv tool uninstall aider-chat`), AiderDesk;
- Pinokio (`pinokio-updater`), Microsoft PC Manager (`PC Manager Store`), MarkText (`marktext-updater`);
- rivisti il 17/09 e aggiunti al dizionario: Ollama, PhotoGenie X (con `Roaming\@iplabs`),
  `Program Files\PhotoSi`, Zed, Mullvad, Bun, OpenWork, S3 Browser, Qodo, Semgrep, browser-use,
  Goose (resta `Local\Goose\bin` nel PATH), Kilo Code (disinstallato il 17/09), Azure CLI (`.azure`, `.ms-ad`);
- cartelle vuote di origine ignota, lasciate all'euristica: `ProgramData\Goodix`, `Roaming\RtSubscribe`,
  `Local\Snowflake`, `.ai` (contiene solo `mcp\mcp.json` vuoto).

Da tenere:
- Liferay Developer Studio (`Program Files\LiferayDevStudio`, da zip): rilevato automaticamente come
  portabile; la voce Eclipse IDE riconosce anche `Liferay*`. apps.user.toml aggiunge solo
  `C:\WORKAREA\Applications\eclipse\eclipse.exe` (fuori dalle radici analizzate);
- Syncthing Tray: portabile in `Roaming\Syncthingtray`, rilevato tramite processo e `detect.files`;
- ArubaSign (installato): crea anche `.ArubaSign`, `.store` (chiave), `.stats`, `Local\RootUpdater` (WebView2);
- MinIO Client (`~\mc`, eseguibile in `C:\WORKAREA\Applications\tools`); rclone in uso
  (`Roaming\rclone` vuota, in `[ignore]` di apps.user.toml);
- `Local\Downloaded Installations`: cache InstallShield con il .msi della Killer Performance Driver
  Suite, che è ancora installata;
- cache condivise: `Local\Pandoc` (scaricato da pypandoc), `Local\datalab` (modelli marker/surya),
  `.paddlex`, `Local\puccinialin`, `.chromium-browser-snapshots` (Puppeteer), librerie Python
  (`pypa`, `pip-audit`, `nltk_data`, `.matplotlib`, `.triton`, `.dspy_cache`, `.streamlit`), PsySH,
  `Local\NEO` (driver Intel), `.swt`, `Local\CEF`; le cache di pacchetti npm (`node-gyp`, `js-v8flags`,
  `Prisma`, `configstore`, `chrome-devtools-mcp`) sono nella voce Node.js; `Roaming\fyne` e
  `Local\fyne` (dati delle app Fyne, compreso husk-gui) sono una voce `[[shared]]`.

Output Go (richiesta dell'utente, 17/09/2026): ogni scansione crea `husk_<data>_<ora>` (suffisso `_2`, `_3`
se esiste già) dentro la cartella scelta (default: cartella temporanea di sistema, `report.DefaultDir()`) con
`husk_report.html/csv` (CSV con virgola), `husk_programs.csv`, `husk_path.csv`, `husk_suggestions.toml`.
La GUI sostituisce la vecchia preferenza `%TEMP%\husk` con il nuovo default. Il PoC usa ancora i nomi italiani (`husk_programmi_*`, `husk_suggerimenti_*`).
Stati nella versione Go: `orphan`, `suspect`, `portable`, `shared` (etichetta "Shared folder", non più
"Shared cache": le voci `[[shared]]` sono anche modelli e programmi, es. `.local\bin`), `associated`, `ignored`
(nel PoC e più sotto in questo file: orfano, sospetto, portabile, condivisa, associato, ignorato).
Ultimo report (17/09/2026, Go e PoC uguali): 59 probabili orfani (4,8 GB) e 19 cache condivise (4,5 GB);
file di suggerimenti con 4 voci (le cartelle vuote di origine ignota).
PATH utente da sistemare: `Local\Goose\bin` (Goose disinstallato), `Local\Programs\Ollama` (vuota);
`%USERPROFILE%\.dotnet\tools` e `%USERPROFILE%\go\bin` mancano ma non sono segnalate (SDK .NET e Go installati).
Dopo che l'utente ha pulito il PATH (17/09/2026): 0 voci da rivedere; `.local\bin` compare tra le cache condivise.

## Prossimi passi

Fatti: rinomina in Husk, repository https://github.com/goodmagma/husk (branch `main`),
revisione degli orfani (17/09/2026), scheletro Go con CLI Windows verificata.

1. Provare `husk-gui` con l'utente.
2. Test automatici (`go test ./...`): ci sono solo `CheckPath` e `Dictionary.Owner`; mancano matcher, dizionario (unione delle voci),
   pathutil, classificazione.
3. Provare Linux e macOS (macchine reali o CI) e ampliare `linux.toml` e `darwin.toml`.
4. Pipeline GitHub Actions funzionanti. **Release 0.1.0 pubblicata il 17/09/2026**
   (https://github.com/goodmagma/husk/releases/tag/v0.1.0, tag su `f8e4f36`): CLI per Windows/Linux/macOS
   amd64+arm64 (~1,3 MB), GUI Windows amd64, Linux amd64/arm64 (`.tar.xz`), macOS amd64/arm64 (`.app` zip,
   ~12 MB), `SHA256SUMS.txt`. Linux richiede anche `libwayland-dev libxkbcommon-dev wayland-protocols`
   (GLFW 3.4); per riprodurre la CI Linux c'è Docker Desktop (`golang:1.27`). `fyne package` (fyne.io/tools
   v1.7.2) non accetta `-ldflags`, crea `cmd/husk-gui/Husk.exe` e incrementa `Build` in `FyneApp.toml`
   (in locale annullare la modifica). Le API GitHub senza token hanno un limite di 60 richieste/ora:
   monitorare con intervalli lunghi. `softprops/action-gh-release` v3 carica in parallelo e al secondo tag
   `v0.1.1` è fallito con "Error creating asset temp dir" lasciando la release in **bozza** senza un file:
   ora `preserve_order: true` (caricamento sequenziale). Le bozze non compaiono nelle API senza token. Da fare: provare i pacchetti su Linux e macOS reali; firma del
   codice (Windows SmartScreen, macOS Gatekeeper) non ancora presente.
5. Residui come file: **fatto per la 0.1.1** (vedi "Scansione dei file"); mancano i `.plist` di macOS
   (`~/Library/Preferences` contiene soprattutto file) e i file sciolti fuori dal profilo.
6. Nuove fonti di programmi installati: winget, Scoop, Chocolatey.
7. Dopo la prima release: tabella dei risultati dentro la GUI, eventuali grafici.
