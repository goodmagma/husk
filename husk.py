#!/usr/bin/env python3
"""
husk.py - Husk, PoC per Windows: trova cartelle residue di programmi disinstallati.

SOLO LETTURA: non cancella nulla, produce un report HTML + CSV.
Nessuna dipendenza esterna. Python 3.11+ per il dizionario (tomllib);
con Python 3.9/3.10 funziona solo l'euristica, a meno di installare 'tomli'.

Uso:
    python husk.py [--days 90] [--min-mb 10] [--out DIR] [--all] [--no-open]

File letti accanto allo script (o all'eseguibile):
    apps.toml       dizionario integrato dei programmi noti
    apps.user.toml  voci personali (aggiungono o sostituiscono quelle integrate) ed esclusioni
    ignore.txt      esclusioni semplici, un nome o percorso per riga
"""
from __future__ import annotations

import argparse
import csv
import fnmatch
import glob
import html
import os
import re
import subprocess
import sys
import time
import webbrowser
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path

try:
    import winreg  # solo Windows
except ImportError:  # permette di testare il core su altri sistemi
    winreg = None

try:
    import tomllib  # Python 3.11+
except ImportError:
    try:
        import tomli as tomllib  # type: ignore
    except ImportError:
        tomllib = None

FILE_ATTRIBUTE_REPARSE_POINT = 0x400

# --------------------------------------------------------------------------- #
# Configurazione
# --------------------------------------------------------------------------- #

BUILTIN_IGNORE = {n.lower() for n in [
    # Profilo utente
    "AppData", "Desktop", "Documents", "Downloads", "Music", "Pictures", "Videos", "Favorites",
    "Links", "Contacts", "Searches", "Saved Games", "3D Objects", "OneDrive", "MicrosoftEdgeBackups",
    ".cache", ".config", ".local", "Application Data", "Cookies", "Local Settings", "My Documents",
    "NetHood", "PrintHood", "Recent", "SendTo", "Start Menu", "Templates",
    # AppData
    "Microsoft", "Packages", "Temp", "Programs", "CrashDumps", "D3DSCache", "ConnectedDevicesPlatform",
    "Comms", "PeerDistRepub", "Publishers", "VirtualStore", "PlaceholderTileLogoFolder", "History",
    "Microsoft_Corporation", "IsolatedStorage", "Windows Master Store", "speech",
    "SquirrelTemp",  # cartella temporanea degli installer Electron (Squirrel)
    # Program Files / ProgramData
    "Common Files", "Internet Explorer", "Reference Assemblies", "MSBuild", "dotnet",
    "Uninstall Information", "Microsoft.NET", "PackageManagement", "Microsoft Update Health Tools",
    "Package Cache", "USOShared", "USOPrivate", "ssh", "regid.1991-06.com.microsoft",
    "ModifiableWindowsApps", "Documents and Settings", "SoftwareDistribution", "Whesvc",
    "boost_interprocess",  # memoria condivisa della libreria Boost, usata da molti programmi
]}

# nome cartella normalizzato -> chiave alternativa da cercare (solo per l'euristica)
ALIASES = {
    "vscode": "visualstudiocode",
    "code": "visualstudiocode",
    "nuget": "visualstudio",
    "vs": "visualstudio",
    "msplaywright": "playwright",
    "ipython": "python",
    "jupyter": "python",
    "pip": "python",
    "npm": "nodejs",
    "npmcache": "nodejs",
    "gnupg": "gpg",
}

STATUS_ORDER = ["orfano", "sospetto", "portabile", "condivisa", "associato", "ignorato"]
STATUS_LABEL = {
    "orfano": "Probabile orfano",
    "sospetto": "Nessun programma, ma usata di recente",
    "portabile": "Possibile programma portabile",
    "condivisa": "Cache condivisa",
    "associato": "Associata a un programma",
    "ignorato": "Sistema / ignorata",
}
KIND_LABEL = {
    "models": "Modelli",
    "cache": "Cache",
    "config": "Configurazione",
    "data": "Dati utente (!)",
    "app": "Programma",
    "logs": "Log",
}


@dataclass
class Evidence:
    """Un indizio che un programma è presente sul sistema."""
    name: str
    source: str
    publisher: str = ""
    location: str = ""


@dataclass
class Result:
    path: str
    area: str
    size: int
    files: int
    last_write: float
    status: str
    match: str
    source: str = ""      # "dizionario" / "euristica" / ""
    kind: str = ""
    details: str = ""


def expand(path: str) -> str:
    """Espande %VAR% (anche fuori da Windows, utile per i test) e normalizza i separatori."""
    s = re.sub(r"%([^%]+)%", lambda m: os.environ.get(m.group(1), m.group(0)), path)
    s = os.path.expandvars(s)
    return os.path.normpath(s.replace("\\", os.sep).replace("/", os.sep))


def key_of(path: str) -> str:
    return os.path.normcase(os.path.normpath(path))


# --------------------------------------------------------------------------- #
# Raccolta dei programmi installati
# --------------------------------------------------------------------------- #

UNINSTALL_KEY = r"SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall"


def _reg_value(key, name: str) -> str:
    try:
        value, _ = winreg.QueryValueEx(key, name)
        return str(value).strip() if value else ""
    except OSError:
        return ""


def read_registry() -> list[Evidence]:
    out: list[Evidence] = []
    if winreg is None:
        return out
    hives = [(winreg.HKEY_LOCAL_MACHINE, "HKLM"), (winreg.HKEY_CURRENT_USER, "HKCU")]
    views = [(winreg.KEY_WOW64_64KEY, "64"), (winreg.KEY_WOW64_32KEY, "32")]
    for hive, hname in hives:
        for view, vname in views:
            access = winreg.KEY_READ | view
            try:
                root = winreg.OpenKey(hive, UNINSTALL_KEY, 0, access)
            except OSError:
                continue
            with root:
                i = 0
                while True:
                    try:
                        sub = winreg.EnumKey(root, i)
                    except OSError:
                        break
                    i += 1
                    try:
                        with winreg.OpenKey(root, sub, 0, access) as k:
                            name = _reg_value(k, "DisplayName")
                            if not name:
                                if sub.startswith("{"):
                                    continue
                                name = re.sub(r"_is\d+$", "", sub)  # es. "Ollama_is1"
                            out.append(Evidence(
                                name=name,
                                source=f"registro {hname}/{vname}",
                                publisher=_reg_value(k, "Publisher"),
                                location=_reg_value(k, "InstallLocation"),
                            ))
                    except OSError:
                        continue
    return out


def read_start_menu() -> list[Evidence]:
    out: list[Evidence] = []
    dirs = [
        Path(os.environ.get("ProgramData", r"C:\ProgramData")) / "Microsoft/Windows/Start Menu/Programs",
        Path(os.environ.get("APPDATA", "")) / "Microsoft/Windows/Start Menu/Programs",
    ]
    for d in dirs:
        if not d.is_dir():
            continue
        try:
            for p in d.rglob("*"):
                if p.suffix.lower() == ".lnk":
                    out.append(Evidence(p.stem, "menu Start"))
                elif p.is_dir():
                    out.append(Evidence(p.name, "menu Start"))
        except OSError:
            pass
    return out


def read_store_apps() -> list[Evidence]:
    """App del Microsoft Store (non compaiono nella chiave Uninstall)."""
    if os.name != "nt":
        return []
    cmd = ["powershell", "-NoProfile", "-NonInteractive", "-Command",
           "Get-AppxPackage | ForEach-Object { $_.Name + '|' + $_.Publisher }"]
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=120,
                           encoding="utf-8", errors="replace")
    except (OSError, subprocess.TimeoutExpired):
        return []
    out: list[Evidence] = []
    for line in r.stdout.splitlines():
        name, _, pub = line.strip().partition("|")
        if not name:
            continue
        m = re.search(r"CN=([^,]+)", pub)
        publisher = m.group(1) if m else ""
        out.append(Evidence(name, "Store", publisher=publisher))
        if "." in name:  # "SpotifyAB.SpotifyMusic" -> anche "SpotifyMusic"
            out.append(Evidence(name.rsplit(".", 1)[-1], "Store"))
    return out


def read_path_executables() -> tuple[list[Evidence], set[str]]:
    """Eseguibili nel PATH: coprono tool installati a mano (ollama, cargo, ecc.)."""
    out: list[Evidence] = []
    sysroot = os.path.normcase(os.environ.get("SystemRoot", r"C:\Windows"))
    exts = {".exe", ".cmd", ".bat", ""} if os.name != "nt" else {".exe", ".cmd", ".bat"}
    seen: set[str] = set()
    for d in os.environ.get("PATH", "").split(os.pathsep):
        d = d.strip().strip('"')
        if not d or not os.path.isdir(d) or os.path.normcase(d).startswith(sysroot):
            continue
        out.append(Evidence(os.path.basename(d.rstrip("\\/")), "PATH", location=d))
        try:
            with os.scandir(d) as it:
                for e in it:
                    stem, ext = os.path.splitext(e.name)
                    if ext.lower() in exts and stem.lower() not in seen:
                        seen.add(stem.lower())
                        out.append(Evidence(stem, f"PATH ({d})"))
        except OSError:
            continue
    return out, seen


# --------------------------------------------------------------------------- #
# Dizionario dei programmi noti
# --------------------------------------------------------------------------- #

@dataclass
class AppEntry:
    name: str
    origin: str
    category: str = ""
    shared: bool = False
    names: list[str] = field(default_factory=list)
    exe: list[str] = field(default_factory=list)
    files: list[str] = field(default_factory=list)
    paths: list[tuple[str, str]] = field(default_factory=list)  # (pattern, kind)
    notes: str = ""
    clean: str = ""
    installed: bool = False
    detected_by: str = ""


class Dictionary:
    def __init__(self) -> None:
        self.entries: dict[str, AppEntry] = {}
        self.ignore_names: set[str] = set()
        self.ignore_paths: set[str] = set()
        self.loaded: list[str] = []
        self.errors: list[str] = []

    def load(self, path: Path, origin: str) -> None:
        if not path.is_file():
            return
        if tomllib is None:
            self.errors.append(f"{path.name}: serve Python 3.11+ (oppure 'pip install tomli')")
            return
        try:
            with path.open("rb") as f:
                data = tomllib.load(f)
        except (OSError, tomllib.TOMLDecodeError) as ex:
            self.errors.append(f"{path.name}: {ex}")
            return

        for section, shared in (("app", False), ("shared", True)):
            for raw in data.get(section, []):
                if "name" not in raw:
                    self.errors.append(f"{path.name}: voce [[{section}]] senza 'name'")
                    continue
                det = raw.get("detect", {})
                paths: list[tuple[str, str]] = []
                if "path" in raw:
                    paths.append((raw["path"], raw.get("kind", "")))
                for p in raw.get("paths", []):
                    if isinstance(p, str):
                        paths.append((p, raw.get("kind", "")))
                    else:
                        paths.append((p["path"], p.get("kind", raw.get("kind", ""))))
                entry = AppEntry(
                    name=raw["name"], origin=origin, category=raw.get("category", ""),
                    shared=shared, names=det.get("names", []), exe=det.get("exe", []),
                    files=det.get("files", []), paths=paths,
                    notes=" ".join(filter(None, (raw.get("used_by", ""), raw.get("notes", "")))), clean=raw.get("clean", ""),
                )
                self.entries[entry.name.lower()] = entry  # le voci utente sostituiscono quelle integrate

        ign = data.get("ignore", {})
        self.ignore_names |= {n.lower() for n in ign.get("names", [])}
        self.ignore_paths |= {key_of(expand(p)) for p in ign.get("paths", [])}
        self.loaded.append(f"{path.name} ({origin})")

    def detect(self, installed_names: list[str], path_exes: set[str]) -> None:
        lowered = [(n.lower(), n) for n in installed_names]
        for e in self.entries.values():
            if e.shared:
                continue
            how = ""
            for pat in e.names:
                pl = pat.lower()
                hit = next((orig for low, orig in lowered if fnmatch.fnmatchcase(low, pl)), None)
                if hit:
                    how = f"installato: {hit}"
                    break
            if not how:
                how = next((f"eseguibile nel PATH: {x}" for x in e.exe if x.lower() in path_exes), "")
            if not how:
                how = next((f"file presente: {f}" for f in e.files if glob.glob(expand(f))), "")
            e.installed = bool(how)
            e.detected_by = how

    def resolve_paths(self) -> dict[str, tuple[AppEntry, str, str]]:
        """Ritorna {chiave_percorso: (voce, kind, percorso_reale)} per le cartelle esistenti."""
        out: dict[str, tuple[AppEntry, str, str]] = {}
        for e in self.entries.values():
            for pat, kind in e.paths:
                for p in glob.glob(expand(pat)):
                    if os.path.isdir(p):
                        out.setdefault(key_of(p), (e, kind, p))
        return out


def resource_paths(filename: str) -> list[Path]:
    dirs = []
    if hasattr(sys, "_MEIPASS"):  # file incorporato da PyInstaller (--add-data)
        dirs.append(Path(sys._MEIPASS))  # type: ignore[attr-defined]
    dirs.append(app_dir())
    return [d / filename for d in dirs]


def load_dictionary() -> Dictionary:
    d = Dictionary()
    builtin = next((p for p in resource_paths("apps.toml") if p.is_file()), None)
    if builtin:
        d.load(builtin, "integrato")
    else:
        d.errors.append("apps.toml non trovato: uso solo l'euristica")
    d.load(app_dir() / "apps.user.toml", "utente")
    return d


# --------------------------------------------------------------------------- #
# Euristica: confronto cartella <-> programma per nome
# --------------------------------------------------------------------------- #

_NON_ALNUM = re.compile(r"[^a-z0-9]")
_VERSION_TAIL = re.compile(r"\s+v?\d+(\.\d+)+.*$")
_PARENS = re.compile(r"\(.*?\)|\[.*?\]")
_COMPANY = re.compile(
    r"\b(inc|corp|corporation|ltd|llc|gmbh|s\.?r\.?l|s\.?p\.?a|co|company|limited|technologies|b\.?v)\b\.?",
    re.IGNORECASE)


_CAMEL = re.compile(r"(?<=[a-z])(?=[A-Z])")
_UPDATER_TAIL = re.compile(r"[-_. ]?updater$", re.IGNORECASE)

# parole troppo comuni per una corrispondenza per somiglianza
GENERIC_KEYS = {
    "update", "updater", "setup", "install", "installer", "uninstall", "helper", "service",
    "launcher", "desktop", "tools", "common", "shared", "runtime", "client", "server",
    "cache", "config", "local", "data", "files", "program", "programs", "python", "scripts",
}


def norm(s: str) -> str:
    return _NON_ALNUM.sub("", s.lower())


def tokens(s: str) -> list[str]:
    """'MongoDBCompass' -> ['mongo', 'dbcompass'], 'aider-desk' -> ['aider', 'desk']."""
    return [t for t in _NON_ALNUM.split(_CAMEL.sub(" ", s).lower()) if t]


def aligned(needle: str, toks: list[str]) -> bool:
    """True se needle coincide con parole consecutive di toks (o, se lungo, con l'inizio di una parola)."""
    for i in range(len(toks)):
        if len(needle) >= 8 and toks[i].startswith(needle):
            return True
        acc = ""
        for t in toks[i:]:
            acc += t
            if acc == needle:
                return True
            if len(acc) >= len(needle):
                break
    return False


class Matcher:
    def __init__(self, aliases: dict[str, str]):
        self.aliases = aliases
        self.exact: dict[str, str] = {}
        self.keys: list[tuple[str, list[str], str]] = []
        self.locations: list[tuple[str, str]] = []

    def add_key(self, raw: str, label: str, fuzzy: bool = True) -> None:
        k = norm(raw)
        if len(k) >= 3:
            self.exact.setdefault(k, label)
            if fuzzy and k not in GENERIC_KEYS:
                self.keys.append((k, tokens(raw), label))

    def add(self, ev: Evidence) -> None:
        label = f"{ev.name} [{ev.source}]"
        # gli eseguibili nel PATH sono tanti e dai nomi generici: solo corrispondenza esatta
        fuzzy = not ev.source.startswith("PATH (")
        base = _VERSION_TAIL.sub("", ev.name)
        for raw in {ev.name, base, _PARENS.sub("", base)}:
            self.add_key(raw, label, fuzzy)
        if ev.publisher:
            self.add_key(_COMPANY.sub("", ev.publisher), f"{ev.name} [{ev.source}, editore: {ev.publisher}]")
        if ev.location:
            loc = ev.location.strip().strip('"').rstrip("\\/")
            if len(loc) > 3 and os.path.isabs(loc):
                self.locations.append((key_of(loc), f"{ev.name} [{ev.source}, percorso]"))
                self.add_key(os.path.basename(loc), label)

    def find(self, folder: str) -> str | None:
        f = key_of(folder)
        for loc, label in self.locations:
            if loc == f or loc.startswith(f + os.sep):
                return label

        name = _UPDATER_TAIL.sub("", os.path.basename(folder).lstrip("."))
        n = norm(name)
        if len(n) < 3:
            return None
        targets = [n] + ([self.aliases[n]] if n in self.aliases else [])
        for t in targets:
            if t in self.exact:
                return self.exact[t]
        folder_toks = tokens(name)
        for t in targets:
            t_toks = folder_toks if t == n else [t]
            for k, k_toks, label in self.keys:
                if (len(k) >= 5 and aligned(k, t_toks)) or (len(t) >= 5 and aligned(t, k_toks)):
                    return f"{label} (somiglianza: '{k}')"
        return None


# --------------------------------------------------------------------------- #
# Scansione
# --------------------------------------------------------------------------- #

def _is_reparse(entry: os.DirEntry) -> bool:
    try:
        if entry.is_symlink():
            return True
        st = entry.stat(follow_symlinks=False)
        return bool(getattr(st, "st_file_attributes", 0) & FILE_ATTRIBUTE_REPARSE_POINT)
    except OSError:
        return True


def _long(path: str) -> str:
    """Prefisso \\\\?\\ per superare il limite di 260 caratteri su Windows."""
    if os.name == "nt" and not path.startswith("\\\\?\\"):
        return "\\\\?\\" + os.path.abspath(path)
    return path


def scan_roots() -> list[tuple[str, str]]:
    env = os.environ.get
    profile = env("USERPROFILE") or str(Path.home())
    roaming = env("APPDATA") or os.path.join(profile, "AppData", "Roaming")
    local = env("LOCALAPPDATA") or os.path.join(profile, "AppData", "Local")
    roots = [
        (env("ProgramW6432") or env("ProgramFiles"), "Program Files"),
        (env("ProgramFiles(x86)"), "Program Files (x86)"),
        (env("ProgramData"), "ProgramData"),
        (roaming, r"AppData\Roaming"),
        (local, r"AppData\Local"),
        (os.path.join(local, "Programs"), r"AppData\Local\Programs"),
        (os.path.join(profile, "AppData", "LocalLow"), r"AppData\LocalLow"),
        (profile, "Profilo"),
        (os.path.join(profile, ".cache"), r"Profilo\.cache"),
        (os.path.join(profile, ".config"), r"Profilo\.config"),
        (os.path.join(profile, ".local", "share"), r"Profilo\.local\share"),
    ]
    return [(p, a) for p, a in roots if p]


def enumerate_candidates(roots: list[tuple[str, str]]) -> list[tuple[str, str]]:
    out: list[tuple[str, str]] = []
    seen: set[str] = set()
    root_set = {key_of(p) for p, _ in roots}
    for root, area in roots:
        k = key_of(root)
        if k in seen or not os.path.isdir(root):
            continue
        seen.add(k)
        try:
            with os.scandir(root) as it:
                for e in it:
                    if not e.is_dir(follow_symlinks=False) or _is_reparse(e):
                        continue
                    if key_of(e.path) in root_set:
                        continue  # analizzata come radice a sé
                    out.append((e.path, area))
        except OSError as ex:
            print(f"  ! impossibile leggere {root}: {ex}", file=sys.stderr)
    return out


def measure(path: str) -> tuple[int, int, float]:
    size = files = 0
    try:
        last = os.stat(path).st_mtime
    except OSError:
        last = 0.0
    stack = [_long(path)]
    while stack:
        d = stack.pop()
        try:
            it = os.scandir(d)
        except OSError:
            continue
        with it:
            for e in it:
                try:
                    if _is_reparse(e):
                        continue
                    if e.is_dir(follow_symlinks=False):
                        stack.append(e.path)
                    else:
                        st = e.stat(follow_symlinks=False)
                        size += st.st_size
                        files += 1
                        if st.st_mtime > last:
                            last = st.st_mtime
                except OSError:
                    continue
    return size, files, last


PROGRAM_AREAS = {"Program Files", "Program Files (x86)", r"AppData\Local\Programs"}
_NOT_APP_EXE = re.compile(r"^(unins|uninstall|setup|install|update|vc_?redist|dotnet)", re.IGNORECASE)


def find_executables(path: str, depth: int = 2, limit: int = 3) -> list[str]:
    """Eseguibili nei primi livelli: indizio di un programma portabile (installato da zip)."""
    found: list[str] = []
    level = [_long(path)]
    for _ in range(depth):
        nxt: list[str] = []
        for d in level:
            try:
                with os.scandir(d) as it:
                    for e in it:
                        if _is_reparse(e):
                            continue
                        if e.is_dir(follow_symlinks=False):
                            nxt.append(e.path)
                        elif e.name.lower().endswith(".exe") and not _NOT_APP_EXE.match(e.name):
                            found.append(e.name)
                            if len(found) >= limit:
                                return found
            except OSError:
                continue
        level = nxt
    return found


# --------------------------------------------------------------------------- #
# Report
# --------------------------------------------------------------------------- #

def fmt_size(b: float) -> str:
    for unit in ("B", "KB", "MB", "GB", "TB"):
        if b < 1024 or unit == "TB":
            return f"{b:.0f} {unit}" if unit == "B" else f"{b:.1f} {unit}".replace(".", ",")
        b /= 1024
    return ""


def fmt_date(ts: float) -> str:
    return datetime.fromtimestamp(ts).strftime("%d/%m/%Y") if ts else "-"


def unexpand(path: str) -> str:
    """Sostituisce i prefissi noti con le variabili d'ambiente (per i suggerimenti)."""
    pairs = []
    for var in ("LOCALAPPDATA", "APPDATA", "ProgramData", "ProgramFiles(x86)", "ProgramFiles", "USERPROFILE"):
        val = os.environ.get(var)
        if val:
            pairs.append((key_of(val), var))
    pairs.sort(key=lambda p: -len(p[0]))
    k = key_of(path)
    for prefix, var in pairs:
        if k == prefix or k.startswith(prefix + os.sep):
            return f"%{var}%" + os.path.normpath(path)[len(prefix):].replace("/", "\\")
    return path


def write_csv(results: list[Result], path: Path) -> None:
    with path.open("w", newline="", encoding="utf-8-sig") as f:
        w = csv.writer(f, delimiter=";")
        w.writerow(["Stato", "Fonte", "Contenuto", "Area", "Percorso", "Byte", "Dimensione", "File",
                    "UltimaModifica", "Corrispondenza", "Dettagli"])
        for r in results:
            w.writerow([STATUS_LABEL[r.status], r.source, KIND_LABEL.get(r.kind, r.kind), r.area, r.path,
                        r.size, fmt_size(r.size), r.files,
                        datetime.fromtimestamp(r.last_write).strftime("%Y-%m-%d %H:%M") if r.last_write else "",
                        r.match, r.details])


def write_apps_csv(evidence: list[Evidence], dictionary: Dictionary, path: Path) -> None:
    with path.open("w", newline="", encoding="utf-8-sig") as f:
        w = csv.writer(f, delimiter=";")
        w.writerow(["Nome", "Fonte", "Editore", "Percorso"])
        for e in sorted(evidence, key=lambda e: (e.source, e.name.lower())):
            w.writerow([e.name, e.source, e.publisher, e.location])
        w.writerow([])
        w.writerow(["Voce dizionario", "Origine", "Installato", "Rilevato tramite"])
        for e in sorted(dictionary.entries.values(), key=lambda e: e.name.lower()):
            if not e.shared:
                w.writerow([e.name, e.origin, "sì" if e.installed else "no", e.detected_by])


def write_suggestions(results: list[Result], path: Path) -> int:
    rows = [r for r in results if r.source == "euristica" and r.status in ("orfano", "sospetto", "portabile")]
    if not rows:
        return 0
    lines = [
        "# Cartelle trovate solo con l'euristica: candidati per apps.user.toml.",
        "# Verifica a quale programma appartengono, completa i campi e togli i commenti.",
        "",
    ]
    for r in rows:
        name = os.path.basename(r.path).lstrip(".")
        lines += [
            f"# {fmt_size(r.size)} - ultima modifica {fmt_date(r.last_write)} - {STATUS_LABEL[r.status]}",
            "# [[app]]",
            f'# name = "{name}"',
            '# category = ""',
            f'# detect.names = ["{name}*"]',
            f'# detect.exe = ["{name.lower()}"]',
            f"# paths = [ {{ path = '{unexpand(r.path)}', kind = \"cache\" }} ]",
            "",
        ]
    path.write_text("\n".join(lines), encoding="utf-8")
    return len(rows)


HTML_TEMPLATE = """<!DOCTYPE html>
<html lang="it"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Husk – Cartelle residue</title>
<style>
:root { --bg:#f7f7f5; --fg:#1d1d1b; --muted:#6b6b66; --card:#fff; --line:#e3e3de;
        --orfano:#c0392b; --sospetto:#b9770e; --condivisa:#2e6da4; --associato:#1e8449; --ignorato:#7f8c8d; }
@media (prefers-color-scheme: dark) {
  :root { --bg:#161615; --fg:#ecece8; --muted:#9a9a94; --card:#20201f; --line:#33332f;
          --orfano:#ec7063; --sospetto:#f5b041; --condivisa:#6fa8dc; --associato:#58d68d; --ignorato:#aab7b8; } }
body { margin:0; padding:24px; background:var(--bg); color:var(--fg); font:14px/1.45 "Segoe UI", system-ui, sans-serif; }
h1 { margin:0 0 4px; font-size:22px; } .sub { color:var(--muted); margin-bottom:20px; }
.cards { display:flex; gap:12px; flex-wrap:wrap; margin-bottom:16px; }
.card { background:var(--card); border:1px solid var(--line); border-left:4px solid; border-radius:8px; padding:10px 14px; min-width:180px; }
.card .n { font-size:20px; font-weight:600; }
.card.orfano { border-left-color:var(--orfano); } .card.sospetto, .card.portabile { border-left-color:var(--sospetto); }
.card.condivisa { border-left-color:var(--condivisa); }
.card.associato { border-left-color:var(--associato); } .card.ignorato { border-left-color:var(--ignorato); }
.filters { margin:12px 0; display:flex; gap:16px; flex-wrap:wrap; }
.wrap { overflow-x:auto; background:var(--card); border:1px solid var(--line); border-radius:8px; }
table { border-collapse:collapse; width:100%; }
th, td { padding:7px 10px; border-bottom:1px solid var(--line); text-align:left; vertical-align:top; }
th { cursor:pointer; user-select:none; position:sticky; top:0; background:var(--card); }
.num { text-align:right; white-space:nowrap; } .path { word-break:break-all; }
.small { color:var(--muted); font-size:12px; }
a { color:inherit; }
.tag { display:inline-block; padding:1px 8px; border-radius:10px; border:1px solid; font-size:12px; white-space:nowrap; }
.tag.orfano { color:var(--orfano); } .tag.sospetto, .tag.portabile { color:var(--sospetto); } .tag.condivisa { color:var(--condivisa); }
.tag.associato { color:var(--associato); } .tag.ignorato { color:var(--ignorato); }
.src-dizionario { font-weight:600; }
.warn { color:var(--orfano); }
.note { color:var(--muted); margin-top:16px; font-size:13px; }
</style></head><body>
<h1>Husk – Cartelle residue</h1>
<div class="sub">__SUBTITLE__</div>
<div class="cards">__CARDS__</div>
<div class="filters">
  <label><input type="checkbox" class="flt" value="orfano" checked> Probabili orfani</label>
  <label><input type="checkbox" class="flt" value="sospetto" checked> Usate di recente</label>
  <label><input type="checkbox" class="flt" value="portabile" checked> Portabili</label>
  <label><input type="checkbox" class="flt" value="condivisa" checked> Cache condivise</label>
  <label><input type="checkbox" class="flt" value="associato"> Associate</label>
  <label><input type="checkbox" class="flt" value="ignorato"> Sistema</label>
  <label>Dimensione minima
    <select id="minsize">
      <option value="0">tutte</option>
      <option value="1048576">1 MB</option>
      <option value="10485760">10 MB</option>
      <option value="104857600" selected>100 MB</option>
      <option value="1073741824">1 GB</option>
    </select></label>
</div>
<div class="wrap"><table>
<thead><tr><th>Stato</th><th>Fonte</th><th>Contenuto</th><th>Percorso</th><th>Dimensione</th><th>File</th><th>Ultima modifica</th><th>Dettagli</th></tr></thead>
<tbody>__ROWS__</tbody>
</table></div>
<p class="note">Fonte "dizionario" = cartella riconosciuta da apps.toml (affidabile); "euristica" = dedotta dal nome (da verificare).
Verifica sempre prima di cancellare, soprattutto le voci con contenuto "Dati utente".</p>
<script>
const boxes = document.querySelectorAll('.flt');
const minsize = document.getElementById('minsize');
function apply() {
  const on = new Set([...boxes].filter(b => b.checked).map(b => b.value));
  const min = Number(minsize.value);
  document.querySelectorAll('tbody tr').forEach(r =>
    r.style.display = on.has(r.dataset.s) && Number(r.dataset.b) >= min ? '' : 'none');
}
boxes.forEach(b => b.addEventListener('change', apply));
minsize.addEventListener('change', apply); apply();
document.querySelectorAll('th').forEach((th, i) => th.addEventListener('click', () => {
  const tb = document.querySelector('tbody'), rows = [...tb.rows];
  const asc = th.dataset.asc !== '1'; th.dataset.asc = asc ? '1' : '0';
  const val = c => c.dataset.v ?? c.textContent;
  rows.sort((a, b) => {
    const x = val(a.cells[i]), y = val(b.cells[i]), nx = Number(x), ny = Number(y);
    const c = (!isNaN(nx) && !isNaN(ny)) ? nx - ny : x.localeCompare(y, 'it');
    return asc ? c : -c;
  });
  rows.forEach(r => tb.appendChild(r));
}));
</script>
</body></html>
"""


def write_html(results: list[Result], path: Path, args, subtitle: str) -> None:
    e = html.escape
    cards = []
    for s in STATUS_ORDER:
        if s == "ignorato" and not args.all:
            continue
        items = [r for r in results if r.status == s]
        cards.append(f'<div class="card {s}"><div class="n">{fmt_size(sum(r.size for r in items))}</div>'
                     f'<div>{STATUS_LABEL[s]}: {len(items)}</div></div>')
    rows = []
    for r in results:
        uri = Path(r.path).as_uri()
        files_it = f"{r.files:,}".replace(",", ".")
        kind = KIND_LABEL.get(r.kind, r.kind)
        kind_html = f'<span class="warn">{e(kind)}</span>' if r.kind == "data" else e(kind)
        details = e(r.match) + (f'<div class="small">{e(r.details)}</div>' if r.details else "")
        rows.append(
            f'<tr data-s="{r.status}" data-b="{r.size}"><td><span class="tag {r.status}">{e(STATUS_LABEL[r.status])}</span></td>'
            f'<td class="src-{r.source}">{e(r.source)}</td><td>{kind_html}</td>'
            f'<td class="path"><a href="{e(uri)}">{e(r.path)}</a><div class="small">{e(r.area)}</div></td>'
            f'<td class="num" data-v="{r.size}">{fmt_size(r.size)}</td>'
            f'<td class="num" data-v="{r.files}">{files_it}</td>'
            f'<td data-v="{int(r.last_write)}">{fmt_date(r.last_write)}</td>'
            f'<td>{details}</td></tr>')
    page = (HTML_TEMPLATE.replace("__SUBTITLE__", e(subtitle))
            .replace("__CARDS__", "".join(cards))
            .replace("__ROWS__", "\n".join(rows)))
    path.write_text(page, encoding="utf-8")


# --------------------------------------------------------------------------- #
# Main
# --------------------------------------------------------------------------- #

def app_dir() -> Path:
    if getattr(sys, "frozen", False):  # eseguibile PyInstaller
        return Path(sys.executable).parent
    return Path(__file__).resolve().parent


def load_ignore_txt(dictionary: Dictionary) -> None:
    f = app_dir() / "ignore.txt"
    if not f.exists():
        return
    for line in f.read_text(encoding="utf-8").splitlines():
        t = line.strip()
        if not t or t.startswith("#"):
            continue
        if "\\" in t or "/" in t or "%" in t:
            dictionary.ignore_paths.add(key_of(expand(t)))
        else:
            dictionary.ignore_names.add(t.lower())


def main() -> int:
    ap = argparse.ArgumentParser(prog="husk", description="Husk: report delle cartelle residue (solo lettura).")
    ap.add_argument("--days", type=int, default=90, help="giorni senza modifiche per 'orfano' (default 90)")
    ap.add_argument("--min-mb", type=int, default=0,
                    help="ignora le cartelle trovate per euristica sotto N MB (default 0: tutte)")
    ap.add_argument("--out", default=".", help="cartella di output (default: corrente)")
    ap.add_argument("--all", action="store_true", help="includi anche le cartelle di sistema")
    ap.add_argument("--no-open", action="store_true", help="non aprire il report alla fine")
    ap.add_argument("--workers", type=int, default=8, help="thread di scansione (default 8)")
    args = ap.parse_args()

    if os.name != "nt":
        print("Attenzione: questo PoC è pensato per Windows.", file=sys.stderr)

    t0 = time.perf_counter()

    # 1) Programmi installati
    print("Raccolta programmi installati...")
    evidence: list[Evidence] = []
    for label, fn in [("registro", read_registry), ("menu Start", read_start_menu), ("Store", read_store_apps)]:
        items = fn()
        print(f"  {label:<11} {len(items):>5} indizi")
        evidence.extend(items)
    installed_names = [ev.name for ev in evidence]
    path_ev, path_exes = read_path_executables()
    print(f"  {'PATH':<11} {len(path_ev):>5} indizi")
    evidence.extend(path_ev)

    matcher = Matcher(ALIASES)
    for ev in evidence:
        matcher.add(ev)

    # 2) Dizionario
    dictionary = load_dictionary()
    load_ignore_txt(dictionary)
    dictionary.detect(installed_names, path_exes)
    dict_index = dictionary.resolve_paths()
    n_apps = sum(1 for e in dictionary.entries.values() if not e.shared)
    n_inst = sum(1 for e in dictionary.entries.values() if e.installed)
    print(f"Dizionario: {len(dictionary.entries)} voci da {', '.join(dictionary.loaded) or 'nessun file'}; "
          f"{n_inst}/{n_apps} programmi rilevati, {len(dict_index)} cartelle note presenti")
    for err in dictionary.errors:
        print(f"  ! {err}", file=sys.stderr)

    def is_ignored(path: str, area: str) -> bool:
        k, name = key_of(path), os.path.basename(path).lower()
        if k in dictionary.ignore_paths or name in dictionary.ignore_names:
            return True
        if k in dict_index:
            return False  # il dizionario ha la precedenza sulle esclusioni integrate
        if name in BUILTIN_IGNORE:
            return True
        return area.startswith("Program") and name.startswith("windows")

    # 3) Cartelle da analizzare
    candidates = enumerate_candidates(scan_roots())
    cand_keys = {key_of(p): p for p, _ in candidates}
    ignored_keys = {key_of(p) for p, a in candidates if is_ignored(p, a)}
    contains: dict[str, list[str]] = {}
    for k, (entry, _, real) in dict_index.items():
        if k in cand_keys:
            continue
        # percorso più profondo: se un antenato è già analizzato lo annotiamo lì, altrimenti lo aggiungiamo
        parent, ancestor = os.path.dirname(k), None
        while parent and parent != os.path.dirname(parent):
            if parent in cand_keys:
                ancestor = parent
                break
            parent = os.path.dirname(parent)
        if ancestor and ancestor not in ignored_keys:
            contains.setdefault(ancestor, []).append(entry.name)
        else:
            candidates.append((real, "Dizionario"))

    todo = [(p, a, is_ignored(p, a)) for p, a in candidates]
    todo = [t for t in todo if args.all or not t[2]]
    print(f"Analisi di {len(todo)} cartelle...")

    min_bytes = args.min_mb * 1024 * 1024
    now = time.time()
    results: list[Result] = []
    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        futures = {pool.submit(measure, p): (p, a, ign) for p, a, ign in todo}
        for i, fut in enumerate(as_completed(futures), 1):
            p, a, ign = futures[fut]
            size, files, last = fut.result()
            if sys.stdout.isatty():
                print(f"\r  {i}/{len(todo)}", end="", flush=True)
            k = key_of(p)
            if size < min_bytes and k not in dict_index:
                continue  # le cartelle del dizionario compaiono sempre
            old = (now - last) / 86400 >= args.days
            extra = []
            if k in contains:
                extra.append("contiene cartelle di: " + ", ".join(sorted(set(contains[k]))))

            if ign:
                r = Result(p, a, size, files, last, "ignorato", "")
            elif k in dict_index:
                entry, kind, _ = dict_index[k]
                if entry.notes:
                    extra.append(entry.notes)
                if entry.clean:
                    extra.append(f"pulizia: {entry.clean}")
                if entry.shared:
                    status, match = "condivisa", entry.name
                elif entry.installed:
                    status, match = "associato", f"{entry.name} ({entry.detected_by})"
                else:
                    status, match = "orfano", f"{entry.name}: non rilevato come installato"
                    if not old:
                        extra.append("modificata di recente: verifica che il programma non sia portabile")
                r = Result(p, a, size, files, last, status, match, "dizionario", kind)
            else:
                match = matcher.find(p)
                status = "associato" if match else ("orfano" if old else "sospetto")
                if not match and a in PROGRAM_AREAS:
                    exes = find_executables(p)
                    if exes:
                        status = "portabile"
                        extra.append("contiene eseguibili (" + ", ".join(exes) + "): "
                                     "forse installato da zip; se lo usi aggiungilo a apps.user.toml")
                r = Result(p, a, size, files, last, status, match or "", "euristica")
            r.details = " · ".join(extra)
            results.append(r)
    print()

    results.sort(key=lambda r: (STATUS_ORDER.index(r.status), -r.size))

    # 4) Output
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    html_path = out / f"husk_report_{stamp}.html"
    apps_path = out / f"husk_programmi_{stamp}.csv"
    sugg_path = out / f"husk_suggerimenti_{stamp}.toml"
    subtitle = (f"Generato il {datetime.now():%d/%m/%Y %H:%M} · {len(evidence)} indizi di programmi installati · "
                f"dizionario: {len(dictionary.entries)} voci, {n_inst} programmi rilevati · "
                f"orfano (euristica) = nessuna corrispondenza e nessuna modifica da {args.days} giorni"
                + (f" · euristica sopra {fmt_size(min_bytes)}" if min_bytes else ""))
    write_html(results, html_path, args, subtitle)
    write_csv(results, out / f"husk_report_{stamp}.csv")
    write_apps_csv(evidence, dictionary, apps_path)
    n_sugg = write_suggestions(results, sugg_path)

    orphans = [r for r in results if r.status == "orfano"]
    print(f"\nProbabili orfani: {len(orphans)} ({fmt_size(sum(r.size for r in orphans))})")
    for r in orphans[:15]:
        print(f"  {fmt_size(r.size):>10}  {fmt_date(r.last_write)}  [{r.source[:4]}]  {r.path}")
    if len(orphans) > 15:
        print(f"  ... e altri {len(orphans) - 15} nel report")
    shared = [r for r in results if r.status == "condivisa"]
    if shared:
        print(f"Cache condivise: {len(shared)} ({fmt_size(sum(r.size for r in shared))})")
    print(f"\nReport:       {html_path}")
    print(f"Programmi:    {apps_path}")
    if n_sugg:
        print(f"Suggerimenti: {sugg_path} ({n_sugg} voci)")
    print(f"Tempo:        {time.perf_counter() - t0:.1f}s")

    if not args.no_open:
        webbrowser.open(html_path.as_uri())
    return 0


if __name__ == "__main__":
    sys.exit(main())
