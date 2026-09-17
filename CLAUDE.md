# Husk

Strumento che trova le cartelle residue dei programmi disinstallati (Program Files, ProgramData,
AppData, cartelle "dot" del profilo). **Solo report**: non cancella mai nulla, decide l'utente.

Nome precedente: Ghostdir. **Nome scelto: Husk** (comando `husk`).

## Regole di lavoro

- Nei commit **mai** `Co-Authored-By: Claude` né altre righe di attribuzione.
- Lo script resta solo libreria standard Python (3.11+, serve `tomllib`).
- Il dizionario TOML deve restare riusabile così com'è dalla futura versione Go.
- Lingua: italiano per UI, report, commenti e dizionario.

## Stato (17/09/2026)

Fase: **PoC Python solo Windows**, validato sul PC reale dell'utente rivedendo i report insieme.
Poi riscrittura completa in **Go con interfaccia grafica** (core + provider per sistema operativo:
Windows, poi macOS e Linux).

Verifica disponibilità del nome Husk (fatta il 17/09/2026):
- liberi: winget, Homebrew (formula e cask), Snap, Flathub;
- Go: nessun conflitto, il modulo sarà `github.com/<account>/husk`;
- AUR occupato da un vecchio front-end iptables → usare `husk-bin` o `husk-scanner`;
- PyPI, crates.io e npm occupati: non importa, la versione finale è Go.

### File

| File | Cosa |
|---|---|
| `husk.py` | lo scanner |
| `apps.toml` | dizionario integrato: `[[app]]`, `[[shared]]` (cache condivise) |
| `apps.user.toml` | voci personali ed esclusioni (contiene Liferay Developer Studio), solo locale, non va nel repository |
| `ignore.txt` | esclusioni semplici per nome o percorso |
| `report/` | output delle scansioni (non va nel repository) |

Avvio: `py husk.py --out report` (opzioni: `--min-mb`, `--days 90`, `--all`, `--no-open`, `--workers`).
Durata di una scansione sul PC dell'utente: circa 10 s.

### Logica di classificazione

Radici analizzate (`scan_roots()`): Program Files, Program Files (x86), ProgramData, AppData\Roaming,
AppData\Local, AppData\Local\Programs, AppData\LocalLow, profilo, `.cache`, `.config`, `.local\share`.
Si analizzano le sottocartelle di primo livello.

Programmi installati: registro (chiavi Uninstall HKLM/HKCU, 32 e 64 bit), menu Start,
app dello Store (`Get-AppxPackage`), eseguibili nel PATH.

Stati:
- **Cartella del dizionario**: `condivisa` se è una voce `[[shared]]`; `associato` se il programma
  risulta installato; altrimenti `orfano`, con la nota "modificata di recente" se ci sono modifiche
  negli ultimi 90 giorni. **Compare sempre, a qualunque dimensione.**
- **Cartella trovata per euristica**:
  - `associato` se c'è una corrispondenza per nome, editore o percorso di installazione;
  - altrimenti `portabile` se è in un'area programmi e ha `.exe` nei primi 2 livelli
    (installer, disinstallatori e aggiornatori esclusi);
  - altrimenti `orfano` se non è stata modificata da 90 giorni, `sospetto` ("usata di recente") se sì.
  - `--min-mb` si applica solo qui (default 0).
- `ignorato`: cartelle di sistema in `BUILTIN_IGNORE`, `[ignore]` di apps.user.toml, ignore.txt.

Matcher per nome (`Matcher.find`):
- corrispondenza esatta sul nome normalizzato;
- somiglianza solo su **parole intere** (`tokens()` + `aligned()`); per le chiavi di almeno 8 lettere
  basta l'inizio di una parola;
- gli eseguibili del PATH valgono solo con corrispondenza esatta;
- il suffisso `-updater` viene rimosso prima del confronto;
- le parole troppo generiche (`GENERIC_KEYS`: update, setup, desktop, ...) non contano per la somiglianza.

Report HTML: filtri per stato, menu "Dimensione minima" (default 100 MB), colonne ordinabili.
In console: i primi 15 orfani per dimensione.

### Risultati verificati sul PC dell'utente

Veri orfani confermati dall'utente o da controlli:
- VS Code (`Roaming\Code`, `.vscode`), Rust (`.rustup`), Cline (`.cline`);
- Winhance (`ProgramData\Winhance`, era portabile), Adobe (`LocalLow\Adobe`), PhotoSì (`Roaming\PhotoSi`);
- AnythingLLM, Aider (disinstallato il 17/09 con `uv tool uninstall aider-chat`), AiderDesk;
- Pinokio (`pinokio-updater`), Microsoft PC Manager (`PC Manager Store`), MarkText (`marktext-updater`).

Da tenere:
- `Program Files\LiferayDevStudio`: Eclipse per Liferay installato da zip → voce in apps.user.toml;
- `Local\Downloaded Installations`: cache InstallShield con il .msi della Killer Performance Driver
  Suite, che è ancora installata;
- cache condivise: `Local\Pandoc` (scaricato da pypandoc), `Local\datalab` (modelli marker/surya),
  `.paddlex`, `Local\puccinialin`, `.chromium-browser-snapshots` (Puppeteer).

Output: `husk_report_*.html/csv`, `husk_programmi_*.csv`, `husk_suggerimenti_*.toml`.
Ultimo report: 81 probabili orfani (5,0 GB) e 6 cache condivise (4,4 GB);
file di suggerimenti con 68 voci ancora da rivedere.

## Prossimi passi

1. Cartella del progetto già rinominata `husk`; script, report e output già rinominati (17/09/2026).
2. Repository creato: https://github.com/goodmagma/husk (branch `main`, 17/09/2026).
3. Rivedere con l'utente gli altri orfani piccoli e il file dei suggerimenti.
   Candidati visti: `.bun`, `.qodo`, `.kilocode-shell-integrations`, `.openwork`, `PDFgear`, `Syncthing`,
   `.liferay-ide`, `.semgrep`, `.triton`, `.dspy_cache`, `Programs\AnythingLLM` e `Program Files\Upscayl` (vuote).
4. Supporto ai **residui come file singoli** (es. `%USERPROFILE%\.aider.conf.yml`, `.plist` su macOS).
5. Possibili nuove fonti di programmi installati: winget, Scoop, Chocolatey.
6. Poi: progetto Go (core + provider Windows) che riusa `apps.toml` invariato, e interfaccia grafica
   (Wails preferito per grafici e treemap; Fyne più semplice). Se serve una CLI, eseguibile separato
   senza CGO.
