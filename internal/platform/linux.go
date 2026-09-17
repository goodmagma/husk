//go:build linux

package platform

import (
	"bufio"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goodmagma/husk/internal/model"
)

// Roots returns the folders to scan.
func Roots() []Root {
	h := home()
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(h, ".config")
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(h, ".local", "share")
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(h, ".cache")
	}
	return []Root{
		{Path: "/opt", Area: "/opt", ProgramArea: true},
		{Path: filepath.Join(h, ".local", "opt"), Area: "~/.local/opt", ProgramArea: true},
		{Path: h, Area: "Profile", DotOnly: true},
		{Path: cfg, Area: "~/.config"},
		{Path: data, Area: "~/.local/share"},
		{Path: filepath.Join(h, ".local", "state"), Area: "~/.local/state"},
		{Path: cache, Area: "~/.cache"},
		{Path: filepath.Join(h, ".var", "app"), Area: "Flatpak (data)"},
	}
}

var builtinIgnore = lowerSet(
	// profile
	".cache", ".config", ".local", ".var", ".ssh", ".gnupg", ".pki", ".dbus", ".mozilla-certs",
	".Trash", ".Trash-1000", ".themes", ".icons", ".fonts", ".xsession-errors",
	// ~/.config and ~/.local/share
	"autostart", "systemd", "dconf", "fontconfig", "fonts", "gtk-2.0", "gtk-3.0", "gtk-4.0",
	"ibus", "pulse", "pipewire", "wireplumber", "menus", "applications", "icons", "themes",
	"keyrings", "gvfs-metadata", "Trash", "mime", "sounds", "desktop-directories", "session",
	"gnome-shell", "gnome-session", "nautilus", "xdg-desktop-portal", "user-dirs.locale",
	"evolution", "tracker", "tracker3", "recently-used", "flatpak", "containers", "environment.d",
	"kdedefaults", "plasma-workspace", "kwinrc", "akonadi", "baloo", "kactivitymanagerd",
	"mesa_shader_cache", "mesa_shader_cache_db", "fontconfig", "thumbnails", "gstreamer-1.0",
	"gnome-software", "gnome-settings-daemon", "goa-1.0", "update-notifier", "ubuntu-advantage",
	"packagekit", "snap", "pki", "dbus-1",
)

// IsSystemFolder reports system folders that must not be listed.
func IsSystemFolder(name, _ string) bool {
	return builtinIgnore[strings.ToLower(name)]
}

// Sources returns the sources of installed programs.
func Sources() []Source {
	return []Source{
		{"dpkg", readDpkg},
		{"rpm", readRpm},
		{"pacman", readPacman},
		{"Flatpak", readFlatpak},
		{"Snap", readSnap},
		{"menu", readDesktopEntries},
		{"processes", readProcesses},
	}
}

func readDpkg() []model.Evidence {
	f, err := os.Open("/var/lib/dpkg/status")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []model.Evidence
	var name, maint string
	installed := false
	flush := func() {
		if name != "" && installed {
			out = append(out, model.Evidence{Name: name, Source: "dpkg", Publisher: maint})
		}
		name, maint, installed = "", "", false
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case l == "":
			flush()
		case strings.HasPrefix(l, "Package: "):
			name = strings.TrimPrefix(l, "Package: ")
		case strings.HasPrefix(l, "Status: "):
			installed = strings.HasSuffix(l, " installed")
		}
	}
	flush()
	return out
}

func readRpm() []model.Evidence {
	var out []model.Evidence
	for _, l := range lines(run("rpm", "-qa", "--qf", "%{NAME}|%{VENDOR}\n")) {
		name, vendor, _ := strings.Cut(l, "|")
		if vendor == "(none)" {
			vendor = ""
		}
		out = append(out, model.Evidence{Name: name, Source: "rpm", Publisher: vendor})
	}
	return out
}

var pacmanVersion = regexp.MustCompile(`-[^-]+-[^-]+$`)

func readPacman() []model.Evidence {
	entries, err := os.ReadDir("/var/lib/pacman/local")
	if err != nil {
		return nil
	}
	var out []model.Evidence
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, model.Evidence{Name: pacmanVersion.ReplaceAllString(e.Name(), ""), Source: "pacman"})
		}
	}
	return out
}

func readFlatpak() []model.Evidence {
	var out []model.Evidence
	for _, l := range lines(run("flatpak", "list", "--app", "--columns=application,name")) {
		parts := strings.Split(l, "\t")
		out = append(out, model.Evidence{Name: parts[0], Source: "Flatpak"})
		if len(parts) > 1 && parts[1] != "" {
			out = append(out, model.Evidence{Name: parts[1], Source: "Flatpak"})
		}
		if i := strings.LastIndex(parts[0], "."); i >= 0 { // "com.spotify.Client" -> also "Client"
			out = append(out, model.Evidence{Name: parts[0][i+1:], Source: "Flatpak", ExactOnly: true})
		}
	}
	return out
}

func readSnap() []model.Evidence {
	var out []model.Evidence
	for i, l := range lines(run("snap", "list")) {
		if i == 0 { // header
			continue
		}
		if f := strings.Fields(l); len(f) > 0 {
			ev := model.Evidence{Name: f[0], Source: "Snap"}
			if len(f) > 4 {
				ev.Publisher = strings.TrimSuffix(f[4], "✓")
			}
			out = append(out, ev)
		}
	}
	return out
}

func readDesktopEntries() []model.Evidence {
	h := home()
	dirs := []string{
		"/usr/share/applications",
		"/usr/local/share/applications",
		"/var/lib/flatpak/exports/share/applications",
		"/var/lib/snapd/desktop/applications",
		filepath.Join(h, ".local/share/applications"),
		filepath.Join(h, ".local/share/flatpak/exports/share/applications"),
	}
	var out []model.Evidence
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || filepath.Ext(p) != ".desktop" {
				return nil
			}
			out = append(out, model.Evidence{Name: strings.TrimSuffix(e.Name(), ".desktop"), Source: "menu"})
			if name := desktopName(p); name != "" {
				out = append(out, model.Evidence{Name: name, Source: "menu"})
			}
			return nil
		})
	}
	return out
}

func desktopName(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := sc.Text(); strings.HasPrefix(l, "Name=") {
			return strings.TrimPrefix(l, "Name=")
		}
	}
	return ""
}

func readProcesses() []model.Evidence {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return readUnixProcesses("-eo", "comm=")
	}
	seen := map[string]bool{}
	var out []model.Evidence
	for _, e := range entries {
		if !e.IsDir() || strings.Trim(e.Name(), "0123456789") != "" {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil || seen[exe] || isSystemPathDir(filepath.Dir(exe)) {
			continue
		}
		seen[exe] = true
		out = append(out, processEvidence(strings.TrimSuffix(exe, " (deleted)")))
	}
	return out
}

func appProgram(e fs.DirEntry) bool {
	return isExecutable(e) && !notAppExe.MatchString(e.Name()) && !isLibrary(e.Name())
}

// OpenURL opens a file, a folder or a URL with the default application.
func OpenURL(target string) error {
	return exec.Command("xdg-open", target).Start()
}
