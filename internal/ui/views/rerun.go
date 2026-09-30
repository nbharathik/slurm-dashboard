package views

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/actions"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// Requests from the rerun form.
type (
	// RerunMsg opens the rerun form for one of your jobs.
	RerunMsg struct{ ID string }
	// RerunRequestMsg asks the app to confirm and submit a rerun.
	RerunRequestMsg struct {
		Argv        []string
		Script, Dir string
		Title       string
	}
	// RerunEstimateMsg asks "sbatch --test-only" with the form's options.
	RerunEstimateMsg struct {
		Opts        []actions.Opt
		Script, Dir string
	}
	// RerunEditMsg asks the app to open a copy of the script in $EDITOR.
	RerunEditMsg struct{ ID, Script string }
	// CloseRerunMsg closes the form.
	CloseRerunMsg struct{}
)

// RerunInput is what the app fetched for a rerun.
type RerunInput struct {
	ID, Name, Dir string
	Script        string
	Line          string            // the recorded command line, "" when unknown
	Suggest       map[string]string // right-size suggestions by option name
}

// RerunView is the rerun form: resources with right-size suggestions, carried-over options and a script preview.
type RerunView struct {
	RerunInput
	Unread       []string // words of Line that could not be read safely
	Estimate     string
	EstErr, Busy bool

	line   *parse.SbatchLine
	plan   actions.RerunPlan
	values map[string]string
	focus  int        // a field index; len(plan.Fields) is the Rerun button
	edit   *LineInput // the field being edited, or nil
}

// NewRerun builds the form.
func NewRerun(in RerunInput) *RerunView {
	v := &RerunView{RerunInput: in, values: map[string]string{}}
	if in.Line != "" {
		if l, err := parse.SubmitLine(in.Line, in.Name); err == nil {
			v.line, v.Unread = &l, l.Unread
		} else {
			v.Unread = []string{in.Line}
		}
	}
	v.plan = actions.PlanRerun(v.line, parse.ScriptDirectives(in.Script), in.Suggest)
	return v
}

// SetScript replaces the script after editing and reads its #SBATCH
// lines again; values you changed are kept.
func (v *RerunView) SetScript(script string) {
	v.Script, v.Estimate = script, ""
	v.plan = actions.PlanRerun(v.line, parse.ScriptDirectives(script), v.Suggest)
	v.focus = min(v.focus, len(v.plan.Fields))
}

// Capturing reports that a field is being edited, so keys are text.
func (v *RerunView) Capturing() bool { return v.edit != nil }

func (v *RerunView) value(f actions.RerunField) string {
	if s, ok := v.values[f.Name]; ok {
		return s
	}
	return f.Original
}

func (v *RerunView) title() string {
	return strings.TrimSpace(v.ID + " " + v.Name)
}

func (v *RerunView) request(estimate bool) tea.Cmd {
	opts, err := v.plan.Opts(v.values)
	if err != nil {
		return Emit(FlashMsg{Text: err.Error(), Err: true})
	}
	if estimate {
		v.Busy, v.Estimate, v.EstErr = true, "Asking the scheduler...", false
		return Emit(RerunEstimateMsg{Opts: opts, Script: v.Script, Dir: v.Dir})
	}
	argv, err := actions.RerunArgv(opts)
	if err != nil {
		return Emit(FlashMsg{Text: err.Error(), Err: true})
	}
	return Emit(RerunRequestMsg{Argv: argv, Script: v.Script, Dir: v.Dir, Title: v.title()})
}

// Update handles keys and clicks.
func (v *RerunView) Update(ctx *Context, msg tea.Msg) tea.Cmd {
	n := len(v.plan.Fields)
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft && v.edit == nil {
		m := tea.MouseMsg(click)
		switch {
		case ctx.InZone("rerun:go", m):
			return v.request(false)
		case ctx.InZone("rerun:cancel", m):
			return Emit(CloseRerunMsg{})
		}
		for i := range v.plan.Fields {
			if ctx.InZone("rerun:field:"+strconv.Itoa(i), m) {
				v.focus = i
			}
		}
		return nil
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if v.edit != nil {
		done, cancel := v.edit.Update(k)
		switch {
		case done:
			v.values[v.plan.Fields[v.focus].Name] = strings.TrimSpace(v.edit.Value())
			v.edit, v.Estimate = nil, ""
		case cancel:
			v.edit = nil
		}
		return nil
	}
	switch s := k.String(); {
	case s == "esc" || s == "q":
		return Emit(CloseRerunMsg{})
	case key.Matches(k, ctx.Keys.Up) || s == "shift+tab":
		v.focus = (v.focus + n) % (n + 1)
	case key.Matches(k, ctx.Keys.Down) || s == "tab":
		v.focus = (v.focus + 1) % (n + 1)
	case s == "enter" && v.focus == n, s == "r":
		return v.request(false)
	case s == "enter":
		v.edit = NewLineInput("", v.value(v.plan.Fields[v.focus]))
	case s == "a":
		applied := 0
		for _, f := range v.plan.Fields {
			if f.Suggest != "" {
				v.values[f.Name] = f.Suggest
				applied++
			}
		}
		v.Estimate = ""
		if applied == 0 {
			return Emit(FlashMsg{Text: "No suggestions for this job"})
		}
	case s == "u":
		v.values, v.Estimate = map[string]string{}, ""
	case s == "s":
		return v.request(true)
	case s == "e":
		return Emit(RerunEditMsg{ID: v.ID, Script: v.Script})
	}
	return nil
}

// Hints are the footer keys.
func (v *RerunView) Hints(*Context) []key.Binding {
	if v.edit != nil {
		return []key.Binding{bind("enter", "keep"), bind("esc", "undo")}
	}
	return []key.Binding{bind("enter", "change"), bind("a", "use suggestions"), bind("s", "estimate"), bind("e", "edit script"), bind("r", "rerun"), bind("esc", "close")}
}

// Render draws the form and, when there is room, the script beside it.
func (v *RerunView) Render(ctx *Context, w, h int) string {
	formW := min(max(w*11/20, 44), w)
	if ctx.Mode <= layout.Compact {
		formW = w
	}
	form := v.renderForm(ctx, formW, h)
	if formW >= w {
		return form
	}
	return joinH(form, formW, v.renderScript(ctx, w-formW, h))
}

func (v *RerunView) renderForm(ctx *Context, w, h int) string {
	th := ctx.Theme
	inner := w - 4
	labelW, valW := 17, max(min(inner-17-2, 22), 8)
	var b []string
	b = append(b, th.Muted.Render(layout.Truncate("From "+v.Dir, inner, th.Sym.Ellipsis)), "")
	for i, f := range v.plan.Fields {
		label := layout.Pad(f.Label, labelW-2, false, "")
		if i == v.focus {
			label = th.Accent.Render(th.Sym.Cursor + " " + label)
		} else {
			label = th.Muted.Render("  " + label)
		}
		val := v.value(f)
		var cell string
		switch {
		case i == v.focus && v.edit != nil:
			cell = v.edit.View(th, valW)
		case val == "":
			cell = th.Faint.Render(layout.Pad("default", valW, false, ""))
		case val != f.Original:
			cell = th.Accent.Render(layout.Pad(layout.Truncate(val, valW, th.Sym.Ellipsis), valW, false, ""))
		default:
			cell = layout.Pad(layout.Truncate(val, valW, th.Sym.Ellipsis), valW, false, "")
		}
		note := ""
		switch {
		case f.Suggest != "" && f.Suggest != val:
			note = th.Info.Render("suggested " + f.Suggest)
		case val != f.Original:
			orig := f.Original
			if orig == "" {
				orig = "default"
			}
			note = th.Faint.Render("was " + orig)
		}
		line := label + cell + "  " + note
		b = append(b, ctx.Mark("rerun:field:"+strconv.Itoa(i), layout.Truncate(line, inner, th.Sym.Ellipsis)))
	}
	b = append(b, "")
	wrap := func(style func(...string) string, text string) {
		for _, l := range layout.Wrap(text, inner) {
			b = append(b, style(l))
		}
	}
	if len(v.plan.Carried) > 0 {
		var s []string
		for _, o := range v.plan.Carried {
			s = append(s, "--"+o.Name+"="+o.Value)
		}
		wrap(th.Muted.Render, "Also passed: "+strings.Join(s, " "))
	}
	if len(v.plan.Dropped) > 0 {
		wrap(th.Muted.Render, "Not carried over: "+strings.Join(v.plan.Dropped, " ")+" (edit the script to keep them)")
	}
	if len(v.Unread) > 0 {
		wrap(th.Warn.Render, "Not read, because Slurm stores the command line without quotes: "+strings.Join(v.Unread, " ")+". Check the fields.")
	}
	if len(v.plan.ScriptArgs) > 0 {
		wrap(th.Warn.Render, "The job was started with arguments ("+strings.Join(v.plan.ScriptArgs, " ")+"); a rerun cannot pass them. Edit the script (e) to set them.")
	}
	if v.Line == "" {
		wrap(th.Faint.Render, "Slurm did not record the command line; the fields come from the script.")
	}
	b = append(b, "", ctx.Mark("rerun:go", components.Button(th, "Rerun (r)", v.focus == len(v.plan.Fields), true))+"  "+
		ctx.Mark("rerun:cancel", th.Faint.Render("esc closes")))
	if v.Estimate != "" {
		style := th.Info.Render
		if v.EstErr {
			style = th.Crit.Render
		}
		b = append(b, "")
		wrap(style, v.Estimate)
	}
	return components.PaddedPanel(th, "Rerun "+v.title(), strings.Join(b, "\n"), w, h, true)
}

func (v *RerunView) renderScript(ctx *Context, w, h int) string {
	th := ctx.Theme
	lines := strings.Split(strings.TrimRight(textsafe.Text(v.Script), "\n"), "\n")
	for i, l := range lines {
		lines[i] = layout.Truncate(l, w-4, th.Sym.Ellipsis)
	}
	title := fmt.Sprintf("Script (%d lines)", len(lines))
	return components.PaddedPanel(th, title, strings.Join(lines, "\n"), w, h, false)
}
