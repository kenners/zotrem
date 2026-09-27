package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kenners/zotrem/pkg/db"
	"github.com/kenners/zotrem/pkg/ts"
	"github.com/kenners/zotrem/uikit"
	lucide "github.com/wux4an/lucide-templ/icons"
)

type NavCounts struct {
	Documents int
	Errors    int
}

type Status struct {
	Configured           bool
	Counts               map[string]int
	Folder               string
	LastRun              *db.SyncRun
	NextRun              string
	RemarkableConfigured bool
	Running              bool
	ZoteroConfigured     bool
	ZoteroUsername       string
}

type DocumentRow struct {
	db.Document
	AnnotatedURL string
	ZoteroURL    string
}

type PageProps struct {
	Active string
	Counts NavCounts
	Status Status
	Title  string
}

type DashboardProps struct {
	Page   PageProps
	Events []db.SyncEvent
	Recent []DocumentRow
}

type DocumentsProps struct {
	Page      PageProps
	Documents []DocumentRow
	Filter    string
}

type DocumentProps struct {
	Page     PageProps
	Document DocumentRow
	Events   []db.SyncEvent
}

type RunsProps struct {
	Page PageProps
	Runs []db.SyncRun
}

type RunProps struct {
	Page   PageProps
	Events []db.SyncEvent
	Run    db.SyncRun
}

type SettingsProps struct {
	Page       PageProps
	Error      string
	Flash      string
	PairingURL string
	Settings   db.Settings
}

func navItems(active string, counts NavCounts) []uikit.NavItem {
	return []uikit.NavItem{
		{Active: active == "dashboard", Href: "/", Icon: lucide.LayoutDashboard(), Label: "Dashboard"},
		{Active: active == "documents", Count: counts.Documents, Href: "/documents", Icon: lucide.Files(), Label: "Documents", ShowCount: true},
		{Active: active == "runs", Count: counts.Errors, Href: "/runs", Icon: lucide.History(), Label: "Sync log", ShowCount: counts.Errors > 0},
		{Active: active == "settings", Href: "/settings", Icon: lucide.Settings(), Label: "Settings"},
	}
}

func When(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "never"
	}
	return t.Local().Format("2 Jan 15:04")
}

func WhenTS(t db.SyncRun) string {
	return When(&t.StartedAt.Time)
}

func Ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return t.Local().Format("2 Jan")
	}
}

func StateLabel(state string) string {
	switch state {
	case db.StateAnnotated:
		return "Annotated"
	case db.StateError:
		return "Error"
	case db.StateQueued:
		return "Queued"
	case db.StateRemoved:
		return "Removed"
	case db.StateSynced:
		return "On reMarkable"
	}
	return strings.Title(state)
}

func RunLabel(run db.SyncRun) string {
	switch run.Status {
	case db.RunStatusError:
		return "Failed"
	case db.RunStatusRunning:
		return "Running"
	}
	return "OK"
}

func RunSummary(run db.SyncRun) string {
	return fmt.Sprintf("%d sent, %d pulled, %d removed", run.Pushed, run.Pulled, run.Removed)
}

func itoa(n int) string { return strconv.Itoa(n) }

func i64(n int64) string { return strconv.FormatInt(n, 10) }

func runDuration(run db.SyncRun) string {
	if run.FinishedAt == nil {
		return "…"
	}
	d := run.FinishedAt.Sub(run.StartedAt.Time).Round(time.Second)
	if d < time.Second {
		return "<1s"
	}
	return d.String()
}

func eventTime(e db.SyncEvent) string { return e.CreatedAt.Local().Format("15:04:05") }

func countOf(counts map[string]int, state string) int { return counts[state] }

func TotalDocuments(counts map[string]int) int {
	total := 0
	for state, n := range counts {
		if state != db.StateRemoved {
			total += n
		}
	}
	return total
}

func tsPtr(t *ts.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	return &t.Time
}

func groupID(s db.Settings) string {
	if s.ZoteroLibraryType != "group" {
		return ""
	}
	return strconv.FormatInt(s.ZoteroLibraryID, 10)
}

func zoteroItemKey(row DocumentRow) string {
	if row.ZoteroParentKey != "" {
		return row.ZoteroParentKey
	}
	return row.ZoteroKey
}
