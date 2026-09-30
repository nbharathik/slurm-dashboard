package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// settingsState is the Settings screen (,): one row per setting that
// config.Settings marks for the screen. A change applies at once and is
// saved to the user's config file.
type settingsState struct {
	rows  []config.Setting
	cur   int
	pcur  int             // the partition under the cursor, on the hide_partitions row
	site  map[string]bool // settings that only the site file sets
	saved string          // the last save result, shown at the bottom
	bad   bool            // saved is an error
}

type settingsSavedMsg struct {
	key string
	err error
}

func zoneSettingsRow(i int) string { return fmt.Sprintf("settings:row:%d", i) }

// openSettings shows the Settings screen.
func (a *App) openSettings() {
	s := &settingsState{site: map[string]bool{}}
	for _, st := range config.Settings {
		if st.Screen {
			s.rows = append(s.rows, st)
		}
	}
	layers := a.opt.ConfigLayers
	if len(layers) == 0 && a.opt.ConfigPath != "" {
		layers = []string{a.opt.ConfigPath}
	}
	for key, file := range config.SetIn(layers) {
		if file != a.opt.ConfigPath {
			s.site[key] = true
		}
	}
	a.settings = s
}

// partitionNames lists the partitions that can be hidden: those the
// cluster has, plus any already hidden so they can be shown again.
func (a *App) partitionNames() []string {
	var out []string
	for _, p := range a.st.Partitions.Data {
		if !slices.Contains(out, p.Name) {
			out = append(out, p.Name)
		}
	}
	for _, h := range a.opt.Config.HidePartitions {
		if !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

func (a *App) updateSettings(msg tea.KeyPressMsg) tea.Cmd {
	s := a.settings
	st := s.rows[s.cur]
	parts := a.partitionNames()
	onParts := st.Key == "hide_partitions"
	step := func(d int) tea.Cmd {
		switch {
		case onParts:
			if len(parts) > 0 {
				s.pcur = (s.pcur + d + len(parts)) % len(parts)
			}
			return nil
		}
		return a.changeSetting(st, func(c *config.Config) { st.Step(c, d) })
	}
	switch k := msg.String(); k {
	case "esc", "q", ",", "ctrl+c":
		a.settings = nil
	case "up", "k":
		s.cur = (s.cur + len(s.rows) - 1) % len(s.rows)
	case "down", "j", "tab":
		s.cur = (s.cur + 1) % len(s.rows)
	case "left", "h":
		return step(-1)
	case "right", "l":
		return step(1)
	case "enter", "space":
		if onParts {
			if len(parts) == 0 {
				return nil
			}
			name := parts[min(s.pcur, len(parts)-1)]
			return a.changeSetting(st, func(c *config.Config) {
				if i := slices.Index(c.HidePartitions, name); i >= 0 {
					c.HidePartitions = slices.Delete(slices.Clone(c.HidePartitions), i, i+1)
				} else {
					c.HidePartitions = append(slices.Clone(c.HidePartitions), name)
				}
			})
		}
		return step(1)
	}
	return nil
}

// changeSetting applies edit to a copy of the config. It rejects a change
// that makes the config invalid (the warning level must stay below the
// critical one), and otherwise applies it now and saves it.
func (a *App) changeSetting(st config.Setting, edit func(*config.Config)) tea.Cmd {
	cfg := a.opt.Config
	cfg.HidePartitions = slices.Clone(cfg.HidePartitions)
	edit(&cfg)
	if cfg.StorageWarn >= cfg.StorageCrit {
		a.settingsNote("the warning level must stay below the critical level", true)
		return nil
	}
	a.applyConfig(cfg)
	if a.settings != nil {
		delete(a.settings.site, st.Key)
	}
	path := a.opt.ConfigPath
	if a.opt.Demo || path == "" {
		a.settingsNote("changed for this session (not saved in the demo)", false)
		return nil
	}
	key := st.Key
	return func() tea.Msg { return settingsSavedMsg{key: key, err: config.Save(path, cfg, key)} }
}

// settingsNote sets the line at the bottom of the Settings screen (nothing
// happens when the screen is closed).
func (a *App) settingsNote(text string, bad bool) {
	if a.settings != nil {
		a.settings.saved, a.settings.bad = text, bad
	}
}

func (a *App) settingsSaved(m settingsSavedMsg) {
	if a.settings == nil {
		if m.err != nil {
			a.setFlash("settings: "+layout.FirstLine(m.err.Error()), true)
		}
		return
	}
	if m.err != nil {
		a.settings.saved, a.settings.bad = "not saved: "+layout.FirstLine(m.err.Error()), true
		return
	}
	a.settings.saved, a.settings.bad = "Saved to "+tildePath(a.opt.ConfigPath, a.opt.Getenv("HOME")), false
}

// tildePath shortens a path in the home directory to ~/....
func tildePath(path, home string) string {
	if home != "" && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func (a *App) mouseSettings(msg tea.MouseMsg) tea.Cmd {
	s := a.settings
	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		switch m.Button {
		case tea.MouseWheelUp:
			s.cur = (s.cur + len(s.rows) - 1) % len(s.rows)
		case tea.MouseWheelDown:
			s.cur = (s.cur + 1) % len(s.rows)
		}
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil
		}
		for i := range s.rows {
			if a.ctx.InZone(zoneSettingsRow(i), msg) {
				s.cur = i
				if s.rows[i].Key != "hide_partitions" {
					return a.updateSettings(tea.KeyPressMsg{Code: tea.KeyRight})
				}
				return nil
			}
		}
	}
	return nil
}

// settingsLine is a heading (row < 0) or a setting row of the Settings
// screen.
type settingsLine struct {
	heading string
	row     int
}

// settingsLines lists rows under group headings, fitting limit lines; short of
// room it drops headings and scrolls to the cursor row.
func (s *settingsState) lines(limit int) []settingsLine {
	var all []settingsLine
	group := ""
	for i, st := range s.rows {
		if st.Group != "" && st.Group != group {
			all = append(all, settingsLine{heading: st.Group, row: -1})
		}
		group = st.Group
		all = append(all, settingsLine{row: i})
	}
	if len(all) <= limit {
		return all
	}
	rows := make([]settingsLine, len(s.rows))
	for i := range rows {
		rows[i] = settingsLine{row: i}
	}
	if len(rows) <= limit || limit < 3 {
		return rows[:min(len(rows), max(limit, 1))]
	}
	// Scroll: reserve a line for each "more" marker that is needed.
	start := 0
	for {
		room := limit
		if start > 0 {
			room--
		}
		if start+room < len(rows) {
			room--
		}
		if s.cur < start+room {
			break
		}
		start++
	}
	out := []settingsLine{}
	if start > 0 {
		out = append(out, settingsLine{heading: fmt.Sprintf("%d more above", start), row: -2})
	}
	room := limit - len(out)
	end := min(start+room, len(rows))
	if end < len(rows) {
		room--
		end = start + room
	}
	out = append(out, rows[start:end]...)
	if end < len(rows) {
		out = append(out, settingsLine{heading: fmt.Sprintf("%d more below", len(rows)-end), row: -2})
	}
	return out
}

// settingsBox draws the Settings screen in a body of h lines.
func (a *App) settingsBox(w, h int) string {
	th, s := a.th, a.settings
	width := min(max(w-4, 40), 78)
	inner := width - 6
	const labelW = 22
	var b strings.Builder
	for _, ln := range s.lines(max(h-7, 3)) {
		if ln.row < 0 {
			style := th.Faint
			if ln.row == -1 {
				style = th.Bold
			}
			b.WriteString("  " + style.Render(ln.heading) + "\n")
			continue
		}
		i, st := ln.row, s.rows[ln.row]
		on := i == s.cur
		mark := "  "
		label := th.Muted.Render(layout.Pad(st.Label, labelW, false, ""))
		if on {
			mark = th.Accent.Render(th.Sym.Cursor) + " "
			label = th.Bold.Render(layout.Pad(st.Label, labelW, false, ""))
		}
		avail := inner - 2 - labelW
		line := mark + label + a.settingValue(st, on, avail)
		b.WriteString(a.ctx.Mark(zoneSettingsRow(i), layout.Truncate(line, inner, th.Sym.Ellipsis)) + "\n")
	}
	b.WriteString("\n")
	note := "Changes apply now and are saved to " + tildePath(a.opt.ConfigPath, a.opt.Getenv("HOME"))
	switch {
	case s.saved != "" && s.bad:
		note = th.Crit.Render(layout.Truncate(s.saved, inner, th.Sym.Ellipsis))
	case s.saved != "":
		note = th.Info.Render(layout.Truncate(s.saved, inner, th.Sym.Ellipsis))
	case a.opt.Demo || a.opt.ConfigPath == "":
		note = "Changes apply now; the demo does not save them"
	default:
		note = th.Muted.Render(layout.Truncate(note, inner, th.Sym.Ellipsis))
	}
	b.WriteString(note + "\n")
	b.WriteString(th.Faint.Render(th.Pick("↑↓", "up/down") + " move " + th.Sym.Separator + " " + th.Sym.TabPrev + th.Sym.TabNext + " change " + th.Sym.Separator + " space toggles " + th.Sym.Separator + " esc closes"))
	return components.Modal(th, "Settings", b.String(), nil, width)
}

// settingValue is the right part of a row: the value, and what it means.
func (a *App) settingValue(st config.Setting, on bool, avail int) string {
	th := a.th
	cfg := a.opt.Config
	style := func(s string) string {
		if on {
			return th.Accent.Render(s)
		}
		return s
	}
	var site string
	if a.settings.site[st.Key] {
		site = th.Faint.Render("  (site)")
	}
	arrows := func(v string) string {
		return style(th.Sym.TabPrev+" "+v+" "+th.Sym.TabNext) + site
	}
	switch st.Key {
	case "hide_partitions":
		return a.partitionChecks(avail)
	case "refresh":
		iv := config.SpeedIntervals(cfg.Refresh)
		hint := "my jobs " + units.FormatShort(iv.MyJobs) + ", cluster " + units.FormatShort(iv.Cluster)
		if iv.Manual {
			hint = "load once, then press r"
		}
		return arrows(cfg.Refresh) + "  " + th.Faint.Render(hint)
	case "storage_warn", "storage_crit":
		return arrows(st.Format(&cfg) + "%")
	}
	return arrows(st.Format(&cfg))
}

// partitionChecks draws "[x] login  [ ] debug", scrolled so that the
// partition under the cursor is visible.
func (a *App) partitionChecks(avail int) string {
	th, s := a.th, a.settings
	parts := a.partitionNames()
	if len(parts) == 0 {
		return th.Faint.Render("no partitions known yet")
	}
	hidden := a.opt.Config.HidePartitions
	item := func(i int) string {
		box := "[ ] "
		if slices.Contains(hidden, parts[i]) {
			box = "[x] "
		}
		text := box + parts[i]
		if a.settings.cur < len(s.rows) && s.rows[s.cur].Key == "hide_partitions" && i == min(s.pcur, len(parts)-1) {
			return th.Accent.Render(text)
		}
		return text
	}
	start := 0
	for start < s.pcur {
		w := 0
		for i := start; i <= min(s.pcur, len(parts)-1); i++ {
			w += layout.Width(item(i)) + 2
		}
		if w <= avail {
			break
		}
		start++
	}
	var out []string
	used := 0
	for i := start; i < len(parts); i++ {
		it := item(i)
		if used+layout.Width(it) > avail && len(out) > 0 {
			out = append(out, th.Faint.Render(th.Sym.Ellipsis))
			break
		}
		used += layout.Width(it) + 2
		out = append(out, it)
	}
	return strings.Join(out, "  ")
}
