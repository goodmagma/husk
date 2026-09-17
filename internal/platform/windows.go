//go:build windows

package platform

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/pathutil"
)

const uninstallKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`

// Roots returns the folders to scan.
func Roots() []Root {
	env := os.Getenv
	profile := env("USERPROFILE")
	if profile == "" {
		profile = home()
	}
	roaming := env("APPDATA")
	if roaming == "" {
		roaming = filepath.Join(profile, "AppData", "Roaming")
	}
	local := env("LOCALAPPDATA")
	if local == "" {
		local = filepath.Join(profile, "AppData", "Local")
	}
	pf := env("ProgramW6432")
	if pf == "" {
		pf = env("ProgramFiles")
	}
	return []Root{
		{Path: pf, Area: "Program Files", ProgramArea: true},
		{Path: env("ProgramFiles(x86)"), Area: "Program Files (x86)", ProgramArea: true},
		{Path: env("ProgramData"), Area: "ProgramData"},
		{Path: roaming, Area: `AppData\Roaming`},
		{Path: local, Area: `AppData\Local`},
		{Path: filepath.Join(local, "Programs"), Area: `AppData\Local\Programs`, ProgramArea: true},
		{Path: filepath.Join(profile, "AppData", "LocalLow"), Area: `AppData\LocalLow`},
		{Path: profile, Area: "Profile"},
		{Path: filepath.Join(profile, ".cache"), Area: `Profilo\.cache`},
		{Path: filepath.Join(profile, ".config"), Area: `Profilo\.config`},
		{Path: filepath.Join(profile, ".local", "share"), Area: `Profilo\.local\share`},
	}
}

var builtinIgnore = lowerSet(
	// User profile
	"AppData", "Desktop", "Documents", "Downloads", "Music", "Pictures", "Videos", "Favorites",
	"Links", "Contacts", "Searches", "Saved Games", "3D Objects", "OneDrive", "MicrosoftEdgeBackups",
	".cache", ".config", ".local", "Application Data", "Cookies", "Local Settings", "My Documents",
	"NetHood", "PrintHood", "Recent", "SendTo", "Start Menu", "Templates",
	// AppData
	"Microsoft", "Packages", "Temp", "Programs", "CrashDumps", "D3DSCache", "ConnectedDevicesPlatform",
	"Comms", "PeerDistRepub", "Publishers", "Common", "VirtualStore", "PlaceholderTileLogoFolder", "History",
	"Microsoft_Corporation", "IsolatedStorage", "Windows Master Store", "speech",
	"SquirrelTemp", // temporary folder of Electron installers (Squirrel)
	// Program Files / ProgramData
	"Common Files", "Internet Explorer", "Reference Assemblies", "MSBuild", "dotnet",
	"Uninstall Information", "Microsoft.NET", "PackageManagement", "Microsoft Update Health Tools",
	"Package Cache", "USOShared", "USOPrivate", "ssh", "regid.1991-06.com.microsoft",
	"ModifiableWindowsApps", "Documents and Settings", "SoftwareDistribution", "Whesvc",
	"boost_interprocess",                     // shared memory of the Boost library
	"InstallShield Installation Information", // uninstall data of InstallShield programs
)

// IsSystemFolder reports system folders that must not be listed.
func IsSystemFolder(name, area string) bool {
	n := strings.ToLower(name)
	return builtinIgnore[n] || (strings.HasPrefix(area, "Program") && strings.HasPrefix(n, "windows"))
}

// Sources returns the sources of installed programs.
func Sources() []Source {
	return []Source{
		{"registry", readRegistry},
		{"Start menu", readStartMenu},
		{"Store", readStoreApps},
		{"processes", readProcesses},
	}
}

func readRegistry() []model.Evidence {
	var out []model.Evidence
	hives := []struct {
		key  registry.Key
		name string
	}{{registry.LOCAL_MACHINE, "HKLM"}, {registry.CURRENT_USER, "HKCU"}}
	views := []struct {
		access uint32
		name   string
	}{{registry.WOW64_64KEY, "64"}, {registry.WOW64_32KEY, "32"}}
	isSuffix := regexp.MustCompile(`_is\d+$`) // e.g. "Ollama_is1"
	for _, h := range hives {
		for _, v := range views {
			access := uint32(registry.READ) | v.access
			root, err := registry.OpenKey(h.key, uninstallKey, access)
			if err != nil {
				continue
			}
			subs, _ := root.ReadSubKeyNames(-1)
			for _, sub := range subs {
				k, err := registry.OpenKey(root, sub, access)
				if err != nil {
					continue
				}
				name := regString(k, "DisplayName")
				if name == "" && !strings.HasPrefix(sub, "{") {
					name = isSuffix.ReplaceAllString(sub, "")
				}
				if name != "" {
					out = append(out, model.Evidence{
						Name:      name,
						Source:    "registry " + h.name + "/" + v.name,
						Publisher: regString(k, "Publisher"),
						Location:  regString(k, "InstallLocation"),
					})
				}
				k.Close()
			}
			root.Close()
		}
	}
	return out
}

func regString(k registry.Key, name string) string {
	s, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func readStartMenu() []model.Evidence {
	var out []model.Evidence
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	dirs := []string{
		filepath.Join(programData, `Microsoft\Windows\Start Menu\Programs`),
		filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`),
	}
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil || p == d {
				return nil
			}
			if e.IsDir() {
				out = append(out, model.Evidence{Name: e.Name(), Source: "Start menu"})
			} else if strings.EqualFold(filepath.Ext(p), ".lnk") {
				out = append(out, model.Evidence{Name: strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), Source: "Start menu"})
			}
			return nil
		})
	}
	return out
}

// readStoreApps reads Microsoft Store apps (they are not listed under the Uninstall key).
func readStoreApps() []model.Evidence {
	outText := run("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"[Console]::OutputEncoding=[Text.Encoding]::UTF8; Get-AppxPackage | ForEach-Object { $_.Name + '|' + $_.Publisher }")
	cn := regexp.MustCompile(`CN=([^,]+)`)
	var out []model.Evidence
	for _, l := range lines(outText) {
		name, pub, _ := strings.Cut(l, "|")
		if name == "" {
			continue
		}
		publisher := ""
		if m := cn.FindStringSubmatch(pub); m != nil {
			publisher = m[1]
		}
		out = append(out, model.Evidence{Name: name, Source: "Store", Publisher: publisher})
		if i := strings.LastIndex(name, "."); i >= 0 { // "SpotifyAB.SpotifyMusic" -> also "SpotifyMusic"
			out = append(out, model.Evidence{Name: name[i+1:], Source: "Store"})
		}
	}
	return out
}

// readProcesses reads running programs: they cover portable programs started from any folder.
func readProcesses() []model.Evidence {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	sysroot := pathutil.Key(os.Getenv("SystemRoot"))
	seen := map[string]bool{}
	var out []model.Evidence
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		exe := processPath(pe.ProcessID)
		if exe == "" {
			continue
		}
		k := pathutil.Key(exe)
		if seen[k] || (sysroot != "." && pathutil.IsUnder(k, sysroot)) {
			continue
		}
		seen[k] = true
		base := filepath.Base(exe)
		out = append(out, model.Evidence{
			Name:      strings.TrimSuffix(base, filepath.Ext(base)),
			Source:    "running",
			Location:  filepath.Dir(exe),
			ExactOnly: true,
		})
	}
	return out
}

func processPath(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

// PathVariables reads the user and system Path variables as stored in the registry.
func PathVariables() []PathVar {
	var out []PathVar
	keys := []struct {
		scope string
		root  registry.Key
		path  string
	}{
		{"user", registry.CURRENT_USER, `Environment`},
		{"system", registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
	}
	for _, k := range keys {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		val, _, err := key.GetStringValue("Path")
		key.Close()
		if err != nil {
			continue
		}
		for _, e := range strings.Split(val, ";") {
			out = append(out, PathVar{k.scope, e})
		}
	}
	return out
}

func isSystemPathDir(d string) bool {
	root := os.Getenv("SystemRoot")
	return root != "" && pathutil.IsUnder(pathutil.Key(d), pathutil.Key(root))
}

var pathExts = lowerSet(".exe", ".cmd", ".bat")

func pathProgram(e fs.DirEntry) (string, bool) {
	if e.IsDir() {
		return "", false
	}
	ext := strings.ToLower(filepath.Ext(e.Name()))
	if !pathExts[ext] && !pathExtEnv()[ext] {
		return "", false
	}
	return strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), true
}

func pathExtEnv() map[string]bool {
	out := map[string]bool{".ps1": true}
	for _, x := range strings.Split(os.Getenv("PATHEXT"), ";") {
		if x != "" {
			out[strings.ToLower(x)] = true
		}
	}
	return out
}

func isLibrary(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".dll")
}

func appProgram(e fs.DirEntry) bool {
	return !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".exe") && !notAppExe.MatchString(e.Name())
}

// IsReparse reports symbolic links and junctions, which must not be followed.
func IsReparse(info fs.FileInfo) bool {
	if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return true
	}
	if a, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return a.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return false
}

// OpenURL opens a file, a folder or a URL with the default application.
func OpenURL(target string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	return cmd.Start()
}

// DefaultPathEntries lists PATH entries that installers add before the folder exists.
var DefaultPathEntries = []DefaultPathEntry{
	{`\go\bin`, "Go", "go install"},
	{`\.dotnet\tools`, ".NET SDK", "dotnet tool install -g"},
	{`\.cargo\bin`, "Rust", "cargo install"},
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func lowerSet(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[strings.ToLower(s)] = true
	}
	return m
}
