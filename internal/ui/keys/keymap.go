// Package keys defines every key binding once; footer, help and palette all read from it.
package keys

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Map holds all key bindings.
type Map struct {
	// Global
	Tabs                         []key.Binding // one per tab, in model.Tabs order
	NextFocus, PrevFocus         key.Binding
	Palette, Refresh, RefreshAll key.Binding
	Pause, Settings              key.Binding
	Help, Back, Quit, Debug      key.Binding

	// Tables
	Up, Down, PageUp, PageDown, Home, End key.Binding
	Open, Select, SelectAll               key.Binding
	Sort, SortReverse, Filter, Menu, Copy key.Binding
	Expand, Collapse, Group               key.Binding

	// Jobs
	Scope, Cancel, Hold, Release, Requeue key.Binding
	Stdout, Stderr, Pager, Shell, GPU     key.Binding
	Why, Script                           key.Binding

	// Overview
	Dismiss key.Binding

	// Nodes
	GPUOnly key.Binding

	// Queue
	Estimate, ScopeCycle key.Binding

	// Usage
	RangePrev, RangeNext, Rerun key.Binding

	// Storage
	Analyse key.Binding

	// Log viewer
	Follow, Wrap, Search, NextMatch, PrevMatch, SwitchStream key.Binding
	NextHighlight, PrevHighlight                             key.Binding
}

func b(help, desc string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
}

// Default returns the default bindings.
func Default() *Map {
	return &Map{
		Tabs:      tabBindings(),
		NextFocus: b("tab", "next section", "tab"), PrevFocus: b("shift+tab", "previous panel", "shift+tab"),
		Palette: b(":", "command", ":"), Refresh: b("r", "refresh", "r"), RefreshAll: b("R", "refresh all", "R"),
		Help: b("?", "help", "?"), Back: b("esc", "back", "esc"), Quit: b("q", "quit", "q", "ctrl+c"),
		Debug: b("ctrl+d", "debug", "ctrl+d"),
		Pause: b("p", "pause", "p"), Settings: b(",", "settings", ","),

		Up: b("↑/k", "up", "up", "k"), Down: b("↓/j", "down", "down", "j"),
		PageUp: b("pgup", "page up", "pgup", "ctrl+b"), PageDown: b("pgdn", "page down", "pgdown", "ctrl+f"),
		Home: b("home", "first", "home"), End: b("end", "last", "end"),
		Open: b("enter", "details", "enter"), Select: b("space", "select", "space"),
		SelectAll: b("ctrl+a", "select all", "ctrl+a"), Sort: b("s", "sort", "s"), SortReverse: b("S", "reverse sort", "S"),
		Filter: b("/", "filter", "/", "f"), Menu: b("m", "actions", "m"), Copy: b("y", "copy id", "y"),
		Expand: b("→", "expand", "right"), Collapse: b("←", "collapse", "left"), Group: b("g", "group", "g"),

		Scope: b("a", "everyone's jobs", "a"), Cancel: b("c", "cancel", "c"), Hold: b("h", "hold", "h"),
		Release: b("u", "release", "u"), Requeue: b("Q", "requeue", "Q"), Stdout: b("l", "logs", "l"),
		Stderr: b("e", "stderr", "e"), Pager: b("o", "pager", "o"), Shell: b("t", "shell", "t"),
		GPU: b("g", "gpu use", "g"), Why: b("w", "why", "w"),
		Script: b("v", "script", "v"),

		Dismiss:  b("x", "dismiss", "x"),
		GPUOnly:  b("G", "all/gpu/cpu", "G"),
		Estimate: b("e", "start estimate", "e"), ScopeCycle: b("v", "what to show", "v"),

		RangePrev: b("[", "shorter range", "["), RangeNext: b("]", "longer range", "]"),
		Rerun: b("n", "rerun", "n"),

		Analyse: b("a", "analyse", "a"),

		Follow: b("F", "follow", "F"), Wrap: b("w", "wrap", "w"), Search: b("/", "search", "/"),
		NextMatch: b("n", "next match", "n"), PrevMatch: b("N", "previous match", "N"),
		SwitchStream:  b("e", "stdout/stderr", "e"),
		NextHighlight: b("]", "next error", "]"), PrevHighlight: b("[", "previous error", "["),
	}
}

// shown is a line of the help for keys that are bound elsewhere (or in
// several bindings): it lists them in one row and matches nothing.
func shown(keys, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys), key.WithHelp(keys, desc))
}

// tabBindings binds 1..6 to the tabs.
func tabBindings() []key.Binding {
	out := make([]key.Binding, len(model.Tabs))
	for i, t := range model.Tabs {
		n := strconv.Itoa(i + 1)
		out[i] = b(n, strings.ToLower(t.Title), n)
	}
	return out
}

// Group is a titled list of bindings for the help overlay.
type Group struct {
	Title    string
	Bindings []key.Binding
}

// Groups returns every binding grouped by context.
func (m *Map) Groups() []Group {
	return []Group{
		{"Global", []key.Binding{shown("1-6", "go to a tab"), m.NextFocus, m.Palette, m.Refresh, m.RefreshAll, m.Pause, m.Settings, m.Help, m.Back, m.Quit, m.Debug}},
		{"Tables", []key.Binding{m.Up, m.Down, shown("pgup pgdn", "page"), shown("home end", "top / bottom"), m.Open, m.Select, m.SelectAll, shown("s S", "sort / reverse"), m.Filter, m.Menu, m.Copy, shown("← →", "fold / unfold")}},
		{"Jobs", []key.Binding{m.Scope, m.Cancel, m.Hold, m.Release, m.Requeue, m.Stdout, m.Stderr, m.Pager, m.Shell, m.GPU, m.Why, m.Script}},
		{"Queue", []key.Binding{m.Estimate, m.ScopeCycle, m.Group, m.Open}},
		{"Overview", []key.Binding{m.Open, m.Dismiss}},
		{"Nodes", []key.Binding{m.GPUOnly, m.Group, m.Open}},
		{"Usage", []key.Binding{m.RangePrev, m.RangeNext, m.Open, m.Stdout, m.Stderr, m.Rerun}},
		{"Storage", []key.Binding{m.Open, m.Analyse}},
		{"Log viewer", []key.Binding{m.Follow, m.Wrap, m.Search, m.NextMatch, m.PrevMatch, m.SwitchStream, m.NextHighlight, m.PrevHighlight, m.Pager, m.Back}},
	}
}
