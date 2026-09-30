package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"

	"github.com/nbharathik/slurm-dashboard/internal/meta"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// Zone IDs of the chrome.
const (
	zoneRefresh = "hdr:refresh"
	zoneTabPrev = "tab:prev"
	zoneTabNext = "tab:next"
)

func zoneTab(name string) string  { return "tab:" + name }
func zoneHint(keyS string) string { return "hint:" + keyS }

// identity is who and where this is: the cluster and user, and "demo" when
// the data is made up. It shrinks to fit room cells, or is left out.
func (a *App) identity(room int) string {
	th := a.th
	sep := th.Faint.Render(" " + th.Sym.Separator + " ")
	var name, user, demo []string
	if a.st.ClusterName != "" {
		name = []string{th.Bold.Render(a.st.ClusterName)}
	}
	if a.st.User != "" {
		user = []string{th.Muted.Render(a.st.User)}
	}
	if a.opt.Demo {
		demo = []string{th.Warn.Render("demo")}
	}
	for _, parts := range [][]string{
		slices.Concat(name, user, demo), slices.Concat(name, demo), demo,
	} {
		if s := strings.Join(parts, sep); s != "" && layout.Width(s) <= room {
			return s
		}
	}
	return ""
}

// freshness is the badge for the visible tab's main source.
func (a *App) freshness() string {
	th := a.th
	src := a.views[a.tab].Source()
	if src == "history" && a.st.Caps.Version != "" && !a.st.Caps.HasSacct {
		return th.Muted.Render("no accounting")
	}
	interval, at, has, tried, err := a.sourceState(src)
	switch f := a.freshnessOf(src, interval); f {
	case state.Failing:
		msg := "error"
		if err != nil {
			msg = layout.FirstLine(err.Error())
		}
		age := ""
		if has {
			age = " " + th.Sym.Separator + " data " + units.FormatShort(a.now().Sub(at)) + " old"
		}
		return th.Crit.Render(th.Sym.Cross+" "+layout.Truncate(msg, 40, th.Sym.Ellipsis)) + th.Muted.Render(age)
	case state.Unknown:
		if tried.IsZero() {
			return th.Muted.Render("loading" + th.Sym.Ellipsis)
		}
		return th.Muted.Render("no data")
	case state.Stale:
		return th.Warn.Render("updated " + units.FormatShort(a.now().Sub(at)) + " ago")
	default:
		return th.OK.Render("updated " + units.FormatShort(a.now().Sub(at)) + " ago")
	}
}

// topBar is the app name, tabs and identity, with a rule marking the open tab.
// Below 60 columns the tabs collapse to "‹ 2/6 Jobs ›".
func (a *App) topBar(w int) (bar, rule string) {
	th := a.th
	ctx := a.ctx
	m := layout.Margin(w)
	left := strings.Repeat(" ", m) + th.Accent.Render(meta.AppName) + th.Faint.Render(" "+th.Sym.Bar)
	x := layout.Width(left)

	var tabs []string
	from, to := 0, 0
	collapsed := a.mode <= layout.Narrow
	if !collapsed {
		at := x
		for i, v := range a.views {
			label := strconv.Itoa(i+1) + " " + v.Title()
			var cell string
			if i == a.tab {
				if b := v.Badge(ctx); b != "" {
					label += " " + b
				}
				cell = th.TabActive.Render(" " + label + " ")
				from = at
			} else {
				cell = " " + th.Faint.Render(strconv.Itoa(i+1)) + " " + th.Tab.Render(v.Title())
				if b := v.Badge(ctx); b != "" {
					cell += " " + th.Warn.Render(b)
				}
				cell += " "
			}
			at += layout.Width(cell)
			if i == a.tab {
				to = at
			}
			tabs = append(tabs, ctx.Mark(zoneTab(v.Name()), cell))
		}
		if at+m > w {
			collapsed = true
		}
	}
	if collapsed {
		v := a.views[a.tab]
		label := fmt.Sprintf("%d/%d %s%s", a.tab+1, len(a.views), v.Title(), badge(v.Badge(ctx)))
		tabs = []string{
			" " + ctx.Mark(zoneTabPrev, th.Key.Render(th.Sym.TabPrev)) + " " + th.TabActive.Render(label) + " " +
				ctx.Mark(zoneTabNext, th.Key.Render(th.Sym.TabNext)),
		}
		from = x + 1 + layout.Width(th.Sym.TabPrev) + 1
		to = from + layout.Width(label)
	}
	line := left + strings.Join(tabs, "")
	if room := w - layout.Width(line) - m - 2; room > 0 {
		if id := a.identity(room); id != "" {
			line += strings.Repeat(" ", w-layout.Width(line)-layout.Width(id)-m) + id
		}
	}
	bar = layout.Pad(line, w, false, th.Sym.Ellipsis)

	to = min(to, w)
	from = min(from, to)
	rule = th.HRule(from) + th.Accent.Render(strings.Repeat(th.Sym.RuleOn, to-from)) + th.HRule(w-to)
	return bar, rule
}

func badge(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

// banner reports a controller outage as a sticky red line.
func (a *App) banner() string {
	err := a.st.MyJobs.Err
	if err == nil || state.KindOf(err) != state.ErrControllerDown {
		return ""
	}
	retry := ""
	for _, s := range a.schedStates() {
		if s.Name == "myjobs" && !s.LastEnd.IsZero() {
			left := s.LastEnd.Add(s.Interval).Sub(a.now())
			if left > 0 {
				retry = ", retrying in " + units.FormatShort(left)
			}
		}
	}
	return "slurmctld not responding" + retry
}

// footer draws the key hints of the active view plus global ones, every one
// clickable, and at the right how fresh the data is.
func (a *App) footer(w int) string {
	th := a.th
	m := layout.Margin(w)
	margin := strings.Repeat(" ", m)
	if a.palette != nil {
		return margin[:m-1] + a.palette.Input(a.th, w-m+1)
	}
	var hints []key.Binding
	ordinary := false
	switch {
	case a.confirm != nil:
		hints = []key.Binding{a.keys.Back}
	case a.settings != nil:
		hints = []key.Binding{a.keys.Back}
	case a.rerun != nil:
		hints = append(hints, a.rerun.Hints(a.ctx)...)
	case a.logView != nil:
		hints = append(hints, a.logView.Hints(a.ctx)...)
		hints = append(hints, a.keys.Help)
	default:
		hints = a.footerBindings()
		ordinary = true
	}
	flashing := a.flash.text != "" && !a.now().After(a.flash.until)
	if flashing && !ordinary {
		return a.flashFooter(w)
	}
	sep := th.Faint.Render(" " + th.Sym.Separator + " ")
	render := func(h key.Binding) string {
		return a.ctx.Mark(zoneHint(h.Keys()[0]), th.Key.Render(h.Help().Key)+" "+th.Muted.Render(h.Help().Desc))
	}
	// Settings and help always show; the view's hints (at most three) fill
	// the rest.
	var tail []string
	if ordinary {
		tail = []string{render(a.keys.Settings), render(a.keys.Help)}
		hints = hints[:len(hints)-2]
	}
	avail := w - 2*m
	right := ""
	if ordinary {
		// A message takes the place of the hints for a few seconds; how
		// fresh the data is stays.
		need := layout.Width(strings.Join(tail, sep))
		if flashing {
			need = layout.Width(a.flashText())
		}
		right = a.statusRight(avail - need - 2)
		if right != "" {
			avail -= layout.Width(right) + 2
		}
	}
	if flashing {
		line := margin + a.flashText()
		if right != "" {
			line += strings.Repeat(" ", max(w-m-layout.Width(line)-layout.Width(right), 1)) + right
		}
		return layout.Pad(line, w, false, th.Sym.Ellipsis)
	}
	used := layout.Width(strings.Join(tail, sep)) + layout.Width(sep)
	var parts []string
	for _, h := range hints {
		if !h.Enabled() || len(h.Keys()) == 0 {
			continue
		}
		hint := render(h)
		wid := layout.Width(hint) + layout.Width(sep)
		if used+wid > avail {
			break
		}
		used += wid
		parts = append(parts, hint)
	}
	line := margin + strings.Join(append(parts, tail...), sep)
	if right != "" {
		line += strings.Repeat(" ", max(w-m-layout.Width(line)-layout.Width(right), 1)) + right
	}
	return layout.Pad(line, w, false, th.Sym.Ellipsis)
}

// statusRight is the data freshness (plus refresh mode); an outage replaces it.
// It shrinks to fit room cells, or is left out.
func (a *App) statusRight(room int) string {
	th := a.th
	fresh := a.freshness()
	if b := a.banner(); b != "" {
		fresh = th.Crit.Render(b)
	}
	sep := th.Faint.Render(" " + th.Sym.Separator + " ")
	full := fresh
	if badge := a.refreshBadge(); badge != "" {
		full = badge + sep + fresh
	}
	for _, s := range []string{full, fresh} {
		if layout.Width(s) <= room {
			return a.ctx.Mark(zoneRefresh, s)
		}
	}
	return ""
}

// now returns the app's clock.
func (a *App) now() time.Time { return a.opt.Now() }

// flashFooter is the footer while a message is showing: the message takes
// the place of the key hints for a few seconds.
func (a *App) flashFooter(w int) string {
	return layout.Pad(strings.Repeat(" ", layout.Margin(w))+a.flashText(), w, false, a.th.Sym.Ellipsis)
}

// flashText is the message, in its colour.
func (a *App) flashText() string {
	th := a.th
	style, icon := th.Flash, th.Sym.Check
	if a.flash.err {
		style, icon = th.FlashErr, th.Sym.Cross
	}
	return style.Render(icon + " " + a.flash.text)
}
