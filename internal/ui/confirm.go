package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/actions"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// Confirm is the confirmation dialog for a destructive action. It shows
// the jobs and the exact command; focus starts on "Keep". Above
// actions.MaxJobs jobs the user must type the count.
type Confirm struct {
	Rerun  *rerunReq      // a rerun instead of an action on jobs
	Custom *customConfirm // any other yes/no question
	Action actions.Action
	Jobs   []model.Job
	Argvs  [][]string
	Focus  int // 0 = keep, 1 = confirm
	Typed  string
}

const (
	zoneConfirmYes  = "confirm:yes"
	zoneConfirmKeep = "confirm:keep"
)

func (c *Confirm) needCount() bool { return len(c.Jobs) > actions.MaxJobs }

func (c *Confirm) title() string {
	if c.Rerun != nil {
		return "Submit this rerun?"
	}
	if c.Custom != nil {
		return c.Custom.title
	}
	noun := "job"
	if len(c.Jobs) != 1 {
		noun = "jobs"
	}
	return fmt.Sprintf("%s %d %s?", c.Action.Title, len(c.Jobs), noun)
}

// Render draws the dialog.
func (c *Confirm) Render(th theme.Theme, ctx *views.Context, w int) string {
	width := min(max(w-8, 36), 72)
	var b strings.Builder
	if c.Rerun != nil {
		return c.renderRerun(th, ctx, width)
	}
	if c.Custom != nil {
		yes := ctx.Mark(zoneConfirmYes, components.Button(th, c.Custom.yes, c.Focus == 1, true))
		keep := ctx.Mark(zoneConfirmKeep, components.Button(th, "Cancel (esc)", c.Focus == 0, false))
		return components.Modal(th, c.title(), strings.Join(layout.Wrap(c.Custom.body, width-6), "\n"), []string{yes, keep}, width)
	}
	shown := min(len(c.Jobs), 8)
	for _, j := range c.Jobs[:shown] {
		where := strings.Join(j.NodeList, ",")
		if where != "" {
			where = " on " + where
		}
		line := fmt.Sprintf("%-8s %-14s %s %s%s", j.ID.Raw, layout.Truncate(j.Name, 14, th.Sym.Ellipsis),
			th.StateLabel(j.State, j.Reason, true), units.FormatDuration(j.TimeUsed), where)
		b.WriteString(layout.Truncate(line, width-6, th.Sym.Ellipsis) + "\n")
	}
	if len(c.Jobs) > shown {
		b.WriteString(th.Muted.Render(fmt.Sprintf("%s and %d more", th.Sym.Ellipsis, len(c.Jobs)-shown)) + "\n")
	}
	b.WriteString("\n" + th.Muted.Render("Will run:") + "\n")
	for _, argv := range c.Argvs {
		b.WriteString("  " + th.Bold.Render(layout.Truncate(actions.Command([][]string{argv}), width-8, th.Sym.Ellipsis)) + "\n")
	}
	if c.needCount() {
		b.WriteString("\n" + th.Warn.Render(fmt.Sprintf("Type %d to confirm: ", len(c.Jobs))) + th.Bold.Render(c.Typed+th.Pick("▌", "_")))
	}
	yes := ctx.Mark(zoneConfirmYes, components.Button(th, c.Action.Title+" (y)", c.Focus == 1, true))
	keep := ctx.Mark(zoneConfirmKeep, components.Button(th, "Keep (esc)", c.Focus == 0, false))
	if c.needCount() {
		yes = ctx.Mark(zoneConfirmYes, components.Button(th, c.Action.Title+" (enter)", c.Focus == 1, true))
	}
	return components.Modal(th, c.title(), strings.TrimRight(b.String(), "\n"), []string{yes, keep}, width)
}

func (a *App) updateConfirm(msg tea.KeyPressMsg) tea.Cmd {
	c := a.confirm
	switch s := msg.String(); {
	case s == "esc" || s == "n" || (s == "q" && !c.needCount()):
		a.confirm = nil
		a.setFlash("Nothing changed", false)
	case s == "tab" || s == "left" || s == "right" || s == "shift+tab":
		c.Focus = 1 - c.Focus
	case s == "y" && !c.needCount():
		return a.acceptConfirm()
	case s == "enter":
		if c.Focus == 0 && !c.needCount() {
			a.confirm = nil
			a.setFlash("Nothing changed", false)
			return nil
		}
		return a.acceptConfirm()
	case s == "backspace" && c.needCount():
		if len(c.Typed) > 0 {
			c.Typed = c.Typed[:len(c.Typed)-1]
		}
	case c.needCount() && len(s) == 1 && s[0] >= '0' && s[0] <= '9':
		if len(c.Typed) < 6 {
			c.Typed += s
		}
		c.Focus = 1
	}
	return nil
}

func (a *App) acceptConfirm() tea.Cmd {
	c := a.confirm
	if c.needCount() && c.Typed != strconv.Itoa(len(c.Jobs)) {
		a.setFlash(fmt.Sprintf("Type %d to confirm", len(c.Jobs)), true)
		return nil
	}
	a.confirm = nil
	if c.Rerun != nil {
		return a.acceptRerun(c.Rerun)
	}
	if c.Custom != nil {
		return c.Custom.run()
	}
	return a.runAction(c.Action, c.Jobs, c.Argvs)
}

// renderRerun shows what a rerun will run: the exact argv, wrapped but
// never shortened, the directory and the script size.
func (c *Confirm) renderRerun(th theme.Theme, ctx *views.Context, width int) string {
	s := c.Rerun
	var b strings.Builder
	lines := strings.Count(strings.TrimRight(s.Script, "\n"), "\n") + 1
	fmt.Fprintf(&b, "%s rerun of %s\n", th.Muted.Render("Job:      "), s.Title)
	fmt.Fprintf(&b, "%s %s\n", th.Muted.Render("Directory:"), layout.Truncate(s.Dir, width-18, th.Sym.Ellipsis))
	fmt.Fprintf(&b, "%s %d lines on stdin (as in the preview)\n", th.Muted.Render("Script:   "), lines)
	b.WriteString("\n" + th.Muted.Render("Will run:") + "\n")
	for _, l := range layout.Wrap(quoteArgv(s.Argv), width-8) {
		b.WriteString("  " + th.Bold.Render(l) + "\n")
	}
	yes := ctx.Mark(zoneConfirmYes, components.Button(th, "Submit (y)", c.Focus == 1, true))
	keep := ctx.Mark(zoneConfirmKeep, components.Button(th, "Keep editing (esc)", c.Focus == 0, false))
	return components.Modal(th, c.title(), strings.TrimRight(b.String(), "\n"), []string{yes, keep}, width)
}

// quoteArgv shows an argv the way a shell would need it typed.
func quoteArgv(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t'\"\\$`!*?[](){}<>|&;#~") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

func (a *App) mouseConfirm(msg tea.MouseMsg) tea.Cmd {
	if click, ok := msg.(tea.MouseClickMsg); !ok || click.Button != tea.MouseLeft {
		return nil
	}
	switch {
	case a.ctx.InZone(zoneConfirmYes, msg):
		a.confirm.Focus = 1
		return a.acceptConfirm()
	case a.ctx.InZone(zoneConfirmKeep, msg):
		a.confirm = nil
		a.setFlash("Nothing changed", false)
	}
	return nil
}
