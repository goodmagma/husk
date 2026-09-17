//go:build !windows

package platform

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/pathutil"
)

// PathVariables reads the process PATH: Linux and macOS have no single place
// it is configured in (shell profiles, launchd, systemd).
func PathVariables() []PathVar {
	var out []PathVar
	for _, e := range filepath.SplitList(os.Getenv("PATH")) {
		out = append(out, PathVar{"process", e})
	}
	return out
}

func isSystemPathDir(d string) bool {
	k := pathutil.Key(d)
	for _, sys := range []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/libexec", "/System"} {
		if k == pathutil.Key(sys) {
			return true
		}
	}
	return false
}

func isExecutable(e fs.DirEntry) bool {
	if e.IsDir() {
		return false
	}
	info, err := e.Info()
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func pathProgram(e fs.DirEntry) (string, bool) {
	if !isExecutable(e) {
		return "", false
	}
	return e.Name(), true
}

var libraryName = regexp.MustCompile(`\.(so(\.\d+)*|dylib)$`)

func isLibrary(name string) bool {
	return libraryName.MatchString(name)
}

// IsReparse reports symbolic links, which must not be followed.
func IsReparse(info fs.FileInfo) bool {
	return info.Mode()&fs.ModeSymlink != 0
}

func hideWindow(*exec.Cmd) {}

// IsSystemFile reports system files in the home folder that must not be listed.
func IsSystemFile(name string) bool {
	return name == ".DS_Store" || name == ".localized" || name == ".CFUserTextEncoding"
}

// readUnixProcesses reads the full process paths with ps.
func readUnixProcesses(args ...string) []model.Evidence {
	seen := map[string]bool{}
	var out []model.Evidence
	for _, exe := range lines(run("ps", args...)) {
		if !filepath.IsAbs(exe) || seen[exe] || isSystemPathDir(filepath.Dir(exe)) {
			continue
		}
		seen[exe] = true
		out = append(out, processEvidence(exe))
	}
	return out
}

func processEvidence(exe string) model.Evidence {
	return model.Evidence{
		Name:      filepath.Base(exe),
		Source:    "running",
		Location:  filepath.Dir(exe),
		ExactOnly: true,
	}
}

func lowerSet(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[strings.ToLower(s)] = true
	}
	return m
}
