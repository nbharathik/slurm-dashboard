// Package views implements the tabs and full-screen viewers as pure functions of the store plus view state.
package views

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/keys"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// Context is what every view can read while updating or rendering.
type Context struct {
	Store  *state.Store
	Theme  theme.Theme
	Keys   *keys.Map
	Zones  *zone.Manager
	Config config.Config
	Now    time.Time
	Mode   layout.Mode
	Width  int
	Height int
	// Dismissed alert keys (with expiry), shared with the app's state file.
	Dismissed map[string]time.Time
	// Prefs are view choices remembered in the state file (PrefMsg).
	Prefs map[string]string
}

// Remembered view choices.
const (
	PrefQueueGroup  = "queue_group"
	PrefHistoryDays = "history_days"
	PrefNodesKind   = "nodes_kind"  // all | gpu | cpu
	PrefNodesGroup  = "nodes_group" // yes | no
	PrefNodesSort   = "nodes_sort"  // "name" or "name:desc"
	PrefJobsSort    = "jobs_sort"   // "column" or "column:desc"; "" = default order
	PrefQueueSort   = "queue_sort"
)

// prefSort reads a remembered "column" or "column:desc".
func prefSort(v string) (col string, desc bool) {
	col, d, _ := strings.Cut(v, ":")
	return col, d == "desc"
}

// sortPref writes a sort for PrefMsg.
func sortPref(col string, desc bool) string {
	if desc {
		return col + ":desc"
	}
	return col
}

// PrefMsg asks the app to remember a view choice across runs.
type PrefMsg struct{ Key, Value string }

// Mark wraps s in a mouse zone.
func (c *Context) Mark(id, s string) string {
	if c.Zones == nil {
		return s
	}
	return c.Zones.Mark(id, s)
}

// InZone reports whether a mouse event hit the zone.
func (c *Context) InZone(id string, msg tea.MouseMsg) bool {
	if c.Zones == nil {
		return false
	}
	return c.Zones.Get(id).InBounds(msg)
}

// View is one tab.
type View interface {
	// Name is the stable tab name ("jobs"), used by /tab and the start_tab setting.
	Name() string
	// Title is the tab label.
	Title() string
	// Badge is a short count shown after the title, e.g. "(3)".
	Badge(ctx *Context) string
	// Source names the data source whose freshness the header shows.
	Source() string
	// Refresh rebuilds derived rows after the store changed.
	Refresh(ctx *Context)
	// Update handles a key or mouse message and may return a command.
	Update(ctx *Context, msg tea.Msg) tea.Cmd
	// Render draws the view into w×h cells.
	Render(ctx *Context, w, h int) string
	// Hints are the key hints shown in the footer.
	Hints(ctx *Context) []key.Binding
	// Capturing reports that the view is reading text (a filter line), so
	// global single-key shortcuts must not fire.
	Capturing() bool
}

// Requests from views to the app. Views stay free of I/O by returning
// these as messages.
type (
	// FlashMsg shows a transient message in the flash line.
	FlashMsg struct {
		Text string
		Err  bool
	}
	// RefreshMsg asks the scheduler to refresh sources (all when empty).
	RefreshMsg struct{ Sources []string }
	// SwitchTabMsg switches to a tab by name.
	SwitchTabMsg struct{ Tab string }
	// OpenJobMsg switches to the Jobs tab and opens a job's detail.
	OpenJobMsg struct{ ID string }
	// ActionMsg asks the app to run a registered action on jobs.
	ActionMsg struct {
		Action string
		Jobs   []model.Job
		Args   map[string]string
	}
	// DetailMsg reports which job detail is open (for the jobdetail
	// collector); empty ID means none.
	DetailMsg struct{ ID string }
	// LogMsg opens the log viewer for a job.
	LogMsg struct {
		Job    model.Job
		Stderr bool
	}
	// HistoryRangeMsg changes the history window in days.
	HistoryRangeMsg struct{ Days int }
	// CopyMsg copies text to the clipboard (OSC 52).
	CopyMsg struct{ Text, What string }
	// PaletteMsg opens the command palette with initial text.
	PaletteMsg struct{ Text string }
	// ShellMsg opens an interactive shell inside a running job.
	ShellMsg struct {
		Job  model.Job
		Node string
	}
	// GPUSampleMsg samples GPU use inside a running job.
	GPUSampleMsg struct{ Job model.Job }
	// ScriptMsg shows a job's batch script.
	ScriptMsg struct{ Job model.Job }
	// PagerMsg opens a job's log in $PAGER.
	PagerMsg struct {
		Job    model.Job
		Stderr bool
	}
	// EffMsg shows the efficiency card of a finished job.
	EffMsg struct{ ID string }
	// NodeMsg opens a node's detail on the Nodes tab.
	NodeMsg struct{ Name string }
	// AnalyseMsg starts the disk-usage analyser on a storage path.
	AnalyseMsg struct {
		Path, FSType string
		Fresh        bool // ignore a cached result
	}
	// CancelAnalyseMsg stops a running analysis.
	CancelAnalyseMsg struct{}
	// RunCommandMsg runs a palette command line directly.
	RunCommandMsg struct{ Line string }
	// DismissMsg hides an alert for 24 hours.
	DismissMsg struct{ Key string }
	// HistoryJobMsg opens a finished job on the History tab.
	HistoryJobMsg struct{ ID string }
)

// Emit returns a command that delivers msg.
func Emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// prefChanges is a PrefMsg for every value that differs between two
// snapshots of a view's choices (nil when none does).
func prefChanges(before, after map[string]string) tea.Cmd {
	var cmds []tea.Cmd
	for k, v := range after {
		if before[k] != v {
			cmds = append(cmds, Emit(PrefMsg{Key: k, Value: v}))
		}
	}
	return tea.Batch(cmds...)
}
