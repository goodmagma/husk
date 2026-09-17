// Package model holds the types shared by the scanner, the reports and the user interfaces.
package model

// Evidence is a hint that a program is present on the system.
type Evidence struct {
	Name      string
	Source    string
	Publisher string
	Location  string
	// ExactOnly: the name counts only as an exact match (executables, processes).
	ExactOnly bool
	// DictOnly: the hint is used only by the dictionary, not by the name heuristic
	// (portable program folders would otherwise match themselves).
	DictOnly bool
}

// Status is the classification of a scanned folder.
type Status string

const (
	Orphan     Status = "orphan"
	Disposable Status = "disposable"
	Suspect    Status = "suspect"
	Portable   Status = "portable"
	Shared     Status = "shared"
	Associated Status = "associated"
	Ignored    Status = "ignored"
)

// StatusOrder is the display order of the statuses.
var StatusOrder = []Status{Orphan, Disposable, Suspect, Portable, Shared, Associated, Ignored}

// StatusLabel describes the statuses in reports and user interfaces.
var StatusLabel = map[Status]string{
	Orphan:     "Likely orphan",
	Disposable: "Disposable files",
	Suspect:    "No program, but recently used",
	Portable:   "Possible portable program",
	Shared:     "Shared folder",
	Associated: "Belongs to a program",
	Ignored:    "System / ignored",
}

// StatusTitle is the plural heading of each status in lists.
var StatusTitle = map[Status]string{
	Orphan:     "Likely orphans",
	Disposable: "Disposable files (logs, crash dumps)",
	Suspect:    "No program, but recently used",
	Portable:   "Possible portable programs",
	Shared:     "Shared folders",
	Associated: "Folders that belong to a program",
	Ignored:    "System / ignored folders",
}

// KindLabel describes the content of dictionary folders.
var KindLabel = map[string]string{
	"models": "Models",
	"cache":  "Cache",
	"config": "Configuration",
	"data":   "User data (!)",
	"app":    "Program",
	"logs":   "Logs",
	"dump":   "Crash dump",
}

// DisposableKinds are the kinds of files that can be deleted even if their program is installed.
var DisposableKinds = map[string]bool{"logs": true, "dump": true}

// What a result row describes.
const (
	TypeFolder = "folder"
	TypeFiles  = "files" // one file or a group of files matching a pattern
)

// How a folder was classified.
const (
	SourceDictionary = "dictionary"
	SourceHeuristic  = "heuristic"
)

// Result is a scanned folder, or a file or group of files (Type == TypeFiles,
// Path is then the file or a pattern such as C:\Users\me\jcef_*.log).
type Result struct {
	Type      string
	Path      string
	Area      string
	Size      int64
	Files     int64
	LastWrite int64 // Unix seconds
	Status    Status
	Match     string
	Source    string
	Kind      string
	Details   string
}

// PathIssue is a PATH entry that should be reviewed.
type PathIssue struct {
	Scope    string // user, system, process
	Entry    string // value as written in the variable
	Expanded string
	Problem  string
	Details  string
}
