//go:build darwin

package platform

import (
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
	lib := filepath.Join(h, "Library")
	return []Root{
		{Path: "/Applications", Area: "/Applications", ProgramArea: true},
		{Path: filepath.Join(h, "Applications"), Area: "~/Applications", ProgramArea: true},
		{Path: "/Library/Application Support", Area: "/Library/Application Support"},
		{Path: filepath.Join(lib, "Application Support"), Area: "~/Library/Application Support"},
		{Path: filepath.Join(lib, "Caches"), Area: "~/Library/Caches"},
		{Path: filepath.Join(lib, "Logs"), Area: "~/Library/Logs"},
		{Path: filepath.Join(lib, "Preferences"), Area: "~/Library/Preferences"},
		{Path: filepath.Join(lib, "Containers"), Area: "~/Library/Containers"},
		{Path: filepath.Join(lib, "Group Containers"), Area: "~/Library/Group Containers"},
		{Path: h, Area: "Profile", DotOnly: true},
		{Path: filepath.Join(h, ".config"), Area: "~/.config"},
		{Path: filepath.Join(h, ".local", "share"), Area: "~/.local/share"},
		{Path: filepath.Join(h, ".cache"), Area: "~/.cache"},
	}
}

var builtinIgnore = lowerSet(
	".cache", ".config", ".local", ".ssh", ".Trash", ".CFUserTextEncoding", ".zsh_sessions",
	"Apple", "CloudDocs", "CrashReporter", "Knowledge", "AddressBook", "CallHistoryDB",
	"CallHistoryTransactions", "FileProvider", "iCloud", "Mobile Documents", "SyncedPreferences",
	"Keychains", "Metadata", "DifferentialPrivacy", "AppleMediaServices", "Safari", "Mail",
	"Messages", "FaceTime", "Photos", "Music", "TV", "News", "Stocks", "Weather", "Maps",
	"Dock", "Accounts", "Animoji", "Assistant", "Siri", "Spotlight", "icdd", "networkserviceproxy",
	"familycircled", "identityservicesd", "ByHost", "Utilities", "Xcode.app",
)

// IsSystemFolder reports system folders that must not be listed.
func IsSystemFolder(name, _ string) bool {
	n := strings.ToLower(name)
	return builtinIgnore[n] ||
		strings.HasPrefix(n, "com.apple.") || strings.HasPrefix(n, "group.com.apple.") ||
		strings.HasSuffix(n, ".app") // bundles are installed programs, not leftovers
}

// Sources returns the sources of installed programs.
func Sources() []Source {
	return []Source{
		{"Applications", readApplications},
		{"pkgutil", readPkgutil},
		{"Homebrew", readHomebrew},
		{"processes", func() []model.Evidence { return readUnixProcesses("-axo", "comm=") }},
	}
}

var bundleID = regexp.MustCompile(`<key>CFBundleIdentifier</key>\s*<string>([^<]+)</string>`)

func readApplications() []model.Evidence {
	var out []model.Evidence
	dirs := []string{"/Applications", "/System/Applications", filepath.Join(home(), "Applications")}
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil || !e.IsDir() {
				return nil
			}
			if strings.HasSuffix(e.Name(), ".app") {
				out = append(out, model.Evidence{Name: strings.TrimSuffix(e.Name(), ".app"), Source: "Applications", Location: p})
				if id := readBundleID(filepath.Join(p, "Contents", "Info.plist")); id != "" {
					out = append(out, model.Evidence{Name: id, Source: "Applications", ExactOnly: true})
				}
				return fs.SkipDir
			}
			if strings.Count(strings.TrimPrefix(p, d), string(filepath.Separator)) > 2 {
				return fs.SkipDir
			}
			return nil
		})
	}
	return out
}

func readBundleID(plist string) string {
	data, err := os.ReadFile(plist)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(string(data), "<?xml") { // binary plist
		data = []byte(run("plutil", "-convert", "xml1", "-o", "-", plist))
	}
	if m := bundleID.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

func readPkgutil() []model.Evidence {
	var out []model.Evidence
	for _, id := range lines(run("pkgutil", "--pkgs")) {
		if strings.HasPrefix(id, "com.apple.") {
			continue
		}
		out = append(out, model.Evidence{Name: id, Source: "pkgutil", ExactOnly: true})
		parts := strings.Split(id, ".")
		if len(parts) >= 3 { // com.vendor.product -> product, publisher vendor
			out = append(out, model.Evidence{Name: parts[2], Source: "pkgutil", Publisher: parts[1]})
		}
	}
	return out
}

func readHomebrew() []model.Evidence {
	var out []model.Evidence
	for _, kind := range []string{"--formula", "--cask"} {
		for _, name := range lines(run("brew", "list", kind, "-1")) {
			out = append(out, model.Evidence{Name: name, Source: "Homebrew " + strings.TrimPrefix(kind, "--")})
		}
	}
	return out
}

func appProgram(e fs.DirEntry) bool {
	if e.IsDir() {
		return strings.HasSuffix(e.Name(), ".app")
	}
	return isExecutable(e) && !notAppExe.MatchString(e.Name()) && !isLibrary(e.Name())
}

// OpenURL opens a file, a folder or a URL with the default application.
func OpenURL(target string) error {
	return exec.Command("open", target).Start()
}
