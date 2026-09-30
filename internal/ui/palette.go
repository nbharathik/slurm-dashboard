package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// MaxHistory is how many palette entries are remembered.
const MaxHistory = 200

// maxSuggestions shown above the input.
const maxSuggestions = 8

// Suggestion is one completion candidate.
type Suggestion struct {
	Value string // what tab inserts (the whole input)
	Label string // what is shown
	Args  string
	Desc  string
	Match []int
}

// Palette is the slash-command line.
type Palette struct {
	text    []rune
	cur     int
	sugg    []Suggestion
	sel     int
	err     string
	hist    []string
	histPos int // -1 = not browsing
}

func zoneSuggestion(i int) string { return fmt.Sprintf("palette:sugg:%d", i) }

func (a *App) openPalette(initial string) {
	a.palette = &Palette{text: []rune(initial), cur: len([]rune(initial)), hist: loadHistory(a.opt.StateDir), histPos: -1}
	a.palette.refresh(a)
}

// String returns the input text.
func (p *Palette) String() string { return string(p.text) }

func (p *Palette) set(s string) {
	p.text = []rune(s)
	p.cur = len(p.text)
}

func (p *Palette) insert(s string) {
	r := []rune(s)
	p.text = slices.Insert(p.text, p.cur, r...)
	p.cur += len(r)
}

func (a *App) updatePalette(msg tea.KeyPressMsg) tea.Cmd {
	p := a.palette
	switch msg.String() {
	case "esc":
		a.palette = nil
		return nil
	case "enter":
		text := strings.TrimSpace(p.String())
		if text == "" {
			a.palette = nil
			return nil
		}
		cmd, err := a.runPalette(text)
		if err != nil {
			p.err = err.Error()
			return nil
		}
		saveHistory(a.opt.StateDir, append(p.hist, text))
		a.palette = nil
		return cmd
	case "tab":
		if len(p.sugg) > 0 {
			p.set(p.sugg[p.sel].Value)
		}
	case "up":
		if strings.TrimSpace(p.String()) == "" || p.histPos >= 0 {
			if n := len(p.hist); n > 0 {
				if p.histPos < 0 {
					p.histPos = n
				}
				p.histPos = max(p.histPos-1, 0)
				p.set(p.hist[p.histPos])
			}
		} else if p.sel > 0 {
			p.sel--
		}
		return nil
	case "down":
		if p.histPos >= 0 {
			p.histPos++
			if p.histPos >= len(p.hist) {
				p.histPos = -1
				p.set("")
			} else {
				p.set(p.hist[p.histPos])
			}
		} else if p.sel < len(p.sugg)-1 {
			p.sel++
		}
		return nil
	case "left":
		p.cur = max(p.cur-1, 0)
		return nil
	case "right":
		p.cur = min(p.cur+1, len(p.text))
		return nil
	case "home", "ctrl+a":
		p.cur = 0
		return nil
	case "end", "ctrl+e":
		p.cur = len(p.text)
		return nil
	case "backspace":
		if p.cur > 0 {
			p.text = slices.Delete(p.text, p.cur-1, p.cur)
			p.cur--
		} else if len(p.text) == 0 {
			a.palette = nil
			return nil
		}
	case "delete":
		if p.cur < len(p.text) {
			p.text = slices.Delete(p.text, p.cur, p.cur+1)
		}
	case "ctrl+u":
		p.text = p.text[p.cur:]
		p.cur = 0
	case "ctrl+w":
		i := p.cur
		for i > 0 && p.text[i-1] == ' ' {
			i--
		}
		for i > 0 && p.text[i-1] != ' ' {
			i--
		}
		p.text = slices.Delete(p.text, i, p.cur)
		p.cur = i
	case "space":
		p.insert(" ")
	default:
		if msg.Text != "" {
			p.insert(msg.Text)
		} else {
			return nil
		}
	}
	p.histPos = -1
	p.err = ""
	p.refresh(a)
	return nil
}

func (a *App) mousePalette(msg tea.MouseMsg) tea.Cmd {
	p := a.palette
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
		for i := range p.sugg {
			if a.ctx.InZone(zoneSuggestion(i), msg) {
				p.set(p.sugg[i].Value)
				p.sel = i
				p.refresh(a)
				return nil
			}
		}
		a.palette = nil
	}
	return nil
}

// refresh recomputes the suggestions for the current input.
func (p *Palette) refresh(a *App) {
	p.sugg = a.suggest(p.String())
	if p.sel >= len(p.sugg) {
		p.sel = 0
	}
}

// Input renders the footer line: the prompt, text, cursor and ghost text.
func (p *Palette) Input(th theme.Theme, w int) string {
	before := string(p.text[:p.cur])
	after := string(p.text[p.cur:])
	cursor := th.Selected.Render(" ")
	if len(after) > 0 {
		cursor = th.Selected.Render(string([]rune(after)[0]))
		after = string([]rune(after)[1:])
	}
	ghost := ""
	if len(p.sugg) > 0 && p.cur == len(p.text) {
		if rest, ok := strings.CutPrefix(p.sugg[p.sel].Value, p.String()); ok && rest != "" {
			ghost = th.Ghost.Render(rest)
		}
	}
	line := " " + th.Accent.Render("/") + th.Input.Render(before) + cursor + th.Input.Render(after) + ghost
	return layout.Pad(line, w, false, th.Sym.Ellipsis)
}

// Overlay draws the suggestions (and any parse error) at the bottom of
// the body.
func (p *Palette) Overlay(th theme.Theme, ctx *views.Context, body string, w, h int) string {
	m := layout.Margin(w)
	bw := w - 2*m // the list sits inside the margins, like everything else
	var lines []string
	for i, s := range p.sugg {
		label := s.Label
		if len(s.Match) > 0 {
			label = highlight(th, label, s.Match)
		}
		row := " " + layout.Pad(label, 18, false, th.Sym.Ellipsis) + " " + th.Muted.Render(layout.Pad(s.Args, 26, false, th.Sym.Ellipsis)) + " " + th.Faint.Render(s.Desc)
		row = layout.Pad(row, bw-2, false, th.Sym.Ellipsis)
		if i == p.sel {
			row = th.Selected.Render(ansi.Strip(row))
		}
		lines = append(lines, ctx.Mark(zoneSuggestion(i), row))
	}
	if p.err != "" {
		lines = append(lines, " "+th.Crit.Render(th.Sym.Cross+" "+p.err))
	}
	if len(lines) == 0 {
		return body
	}
	box := th.Panel.Width(bw).Render(strings.Join(lines, "\n"))
	_, bh := lipgloss.Size(box)
	return layout.OverlayAt(body, box, m, max(h-bh, 0))
}

func highlight(th theme.Theme, s string, idx []int) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if slices.Contains(idx, i) {
			b.WriteString(th.Match.Render(string(c)))
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// suggest completes command names, then arguments.
func (a *App) suggest(input string) []Suggestion {
	in := strings.TrimPrefix(strings.TrimLeft(input, " "), "/")
	name, rest, hasSpace := strings.Cut(in, " ")
	if !hasSpace {
		var keys []string
		var owners []int
		for i, c := range paletteCommands {
			keys = append(keys, c.Name)
			owners = append(owners, i)
			for _, al := range c.Aliases {
				keys = append(keys, al)
				owners = append(owners, i)
			}
		}
		var out []Suggestion
		seen := map[int]bool{}
		if name == "" {
			for i, c := range paletteCommands {
				out = append(out, Suggestion{Value: "/" + c.Name + " ", Label: c.Name, Args: c.Args, Desc: c.Desc})
				seen[i] = true
				if len(out) == maxSuggestions {
					break
				}
			}
			return out
		}
		for _, m := range fuzzy.Find(name, keys) {
			i := owners[m.Index]
			if seen[i] {
				continue
			}
			seen[i] = true
			c := paletteCommands[i]
			match := m.MatchedIndexes
			if keys[m.Index] != c.Name {
				match = nil
			}
			out = append(out, Suggestion{Value: "/" + c.Name + " ", Label: c.Name, Args: c.Args, Desc: c.Desc, Match: match})
			if len(out) == maxSuggestions {
				break
			}
		}
		return out
	}
	c, ok := findCommand(name)
	if !ok || c.Complete == nil {
		return nil
	}
	fields := strings.Split(rest, " ")
	prefix := fields[len(fields)-1]
	head := "/" + name + " " + strings.Join(fields[:len(fields)-1], " ")
	if len(fields) > 1 {
		head += " "
	}
	var out []Suggestion
	for _, cand := range c.Complete(a, len(fields)-1, prefix) {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(cand.Value), strings.ToLower(prefix)) {
			continue
		}
		out = append(out, Suggestion{Value: head + cand.Value, Label: cand.Value, Desc: cand.Desc})
		if len(out) == maxSuggestions {
			break
		}
	}
	return out
}

// runPalette parses and runs one command line.
func (a *App) runPalette(text string) (tea.Cmd, error) {
	in := strings.TrimPrefix(strings.TrimSpace(text), "/")
	fields := strings.Fields(in)
	if len(fields) == 0 {
		return nil, errors.New("type a command")
	}
	c, ok := findCommand(fields[0])
	if !ok {
		if best := a.suggest(fields[0]); len(best) > 0 {
			return nil, fmt.Errorf("unknown command /%s; did you mean /%s?", fields[0], best[0].Label)
		}
		return nil, fmt.Errorf("unknown command /%s", fields[0])
	}
	return c.Run(a, fields[1:])
}

// resolveJobs turns a job reference into jobs: 812, 812_4, 812,813, "."
// (cursor row or selection), @last, @running, @pending, @failed.
func (a *App) resolveJobs(ref string) ([]model.Job, error) {
	if ref == "" {
		ref = "."
	}
	mine := a.st.MyJobs.Data
	switch ref {
	case ".":
		if s, ok := a.views[a.tab].(interface{ SelectedJobs() []model.Job }); ok {
			if jobs := s.SelectedJobs(); len(jobs) > 0 {
				return jobs, nil
			}
		}
		if s, ok := a.viewByName("jobs").(interface{ SelectedJobs() []model.Job }); ok {
			if jobs := s.SelectedJobs(); len(jobs) > 0 {
				return jobs, nil
			}
		}
		return nil, errors.New("no job under the cursor; give a job ID")
	case "@running", "@pending":
		want := model.StateRunning
		if ref == "@pending" {
			want = model.StatePending
		}
		var out []model.Job
		for _, j := range mine {
			if j.State == want {
				out = append(out, j)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("no %s jobs", strings.TrimPrefix(ref, "@"))
		}
		return out, nil
	case "@last":
		if a.state.LastSubmitted == "" {
			return nil, errors.New("no job submitted from sdash yet")
		}
		ref = a.state.LastSubmitted
	case "@failed":
		var out []model.Job
		for _, h := range a.st.History.Data.Jobs {
			if h.State.IsFailure() {
				out = append(out, model.Job{ID: h.ID, Name: h.Name, State: h.State, User: a.st.User, Partition: h.Partition})
			}
		}
		if len(out) == 0 {
			return nil, errors.New("no failed jobs in the current history range")
		}
		return out, nil
	}
	var out []model.Job
	for _, id := range strings.Split(ref, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		found := false
		for _, j := range mine {
			if j.ID.Raw == id {
				out = append(out, j)
				found = true
				break
			}
		}
		// A bare array job ID ("815") means every element of the array
		// still in the queue: "815_1", "815_[3-9]", ...
		if n, err := strconv.ParseUint(id, 10, 64); !found && err == nil {
			for _, j := range mine {
				if j.ID.IsArray() && j.ID.ArrayJobID == n {
					out = append(out, j)
					found = true
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("job %s is not one of your jobs in the queue", id)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("give a job ID")
	}
	return out, nil
}

func (a *App) viewByName(name string) views.View {
	for _, v := range a.views {
		if v.Name() == name {
			return v
		}
	}
	return nil
}

func loadHistory(dir string) []string {
	if dir == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(dir, "history"))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// saveHistory keeps the last MaxHistory unique entries, newest last.
func saveHistory(dir string, hist []string) {
	if dir == "" {
		return
	}
	var out []string
	seen := map[string]bool{}
	for i := len(hist) - 1; i >= 0 && len(out) < MaxHistory; i-- {
		if !seen[hist[i]] {
			seen[hist[i]] = true
			out = append(out, hist[i])
		}
	}
	slices.Reverse(out)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "history"), []byte(strings.Join(out, "\n")+"\n"), 0o600)
}
