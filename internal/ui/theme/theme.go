package theme

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Palette names.
const (
	Auto         = "auto"
	Dark         = "dark"
	Light        = "light"
	HighContrast = "high-contrast"
	Mono         = "mono"
)

// Names are the theme names users can choose.
var Names = []string{Auto, Dark, Light, HighContrast}

// Palette is a set of semantic colours.
type Palette struct {
	Name    string
	Text    color.Color
	Muted   color.Color
	Faint   color.Color
	Accent  color.Color
	OK      color.Color
	Warn    color.Color
	Crit    color.Color
	Info    color.Color
	Magenta color.Color
	Grey    color.Color
	Border  color.Color
	SelBg   color.Color
	SelFg   color.Color
}

var palettes = map[string]Palette{
	Dark: {
		Name: Dark, Text: lipgloss.Color("#E6E6EA"), Muted: lipgloss.Color("#9A9AA6"), Faint: lipgloss.Color("#5C5C68"),
		Accent: lipgloss.Color("#7AA2F7"), OK: lipgloss.Color("#73D98B"),
		Warn: lipgloss.Color("#E5C07B"), Crit: lipgloss.Color("#F27878"), Info: lipgloss.Color("#56C8D8"),
		Magenta: lipgloss.Color("#D98BD1"), Grey: lipgloss.Color("#8A8A96"), Border: lipgloss.Color("#3C3F4C"),
		SelBg: lipgloss.Color("#2E3650"), SelFg: lipgloss.Color("#FFFFFF"),
	},
	Light: {
		Name: Light, Text: lipgloss.Color("#1F2328"), Muted: lipgloss.Color("#57606A"), Faint: lipgloss.Color("#8C959F"),
		Accent: lipgloss.Color("#0B5FD6"), OK: lipgloss.Color("#1A7F37"),
		Warn: lipgloss.Color("#9A6700"), Crit: lipgloss.Color("#CF222E"), Info: lipgloss.Color("#0A7C8C"),
		Magenta: lipgloss.Color("#A0249E"), Grey: lipgloss.Color("#6E7781"), Border: lipgloss.Color("#D0D7DE"),
		SelBg: lipgloss.Color("#DDE8FB"), SelFg: lipgloss.Color("#000000"),
	},
	HighContrast: {
		Name: HighContrast, Text: lipgloss.Color("#FFFFFF"), Muted: lipgloss.Color("#D0D0D0"), Faint: lipgloss.Color("#A0A0A0"),
		Accent: lipgloss.Color("#5FD7FF"), OK: lipgloss.Color("#00FF5F"),
		Warn: lipgloss.Color("#FFFF00"), Crit: lipgloss.Color("#FF5F5F"), Info: lipgloss.Color("#00FFFF"),
		Magenta: lipgloss.Color("#FF87FF"), Grey: lipgloss.Color("#C0C0C0"), Border: lipgloss.Color("#FFFFFF"),
		SelBg: lipgloss.Color("#FFFF00"), SelFg: lipgloss.Color("#000000"),
	},
}

// Symbols are the glyphs used for states, gauges and markers.
type Symbols struct {
	ASCII     bool
	Filled    string // gauge fill
	Empty     string // gauge background
	GPUMine   string
	GPUOther  string
	GPUFree   string
	GPUDown   string // a GPU on an unavailable node
	Cursor    string
	Expanded  string
	Collapsed string
	Check     string
	Cross     string
	Warn      string
	Dot       string
	Arrow     string
	Ellipsis  string
	Separator string
	TabPrev   string
	TabNext   string
	Idle      string
	Refresh   string // before the refresh interval in the header
	Rule      string // thin horizontal line
	RuleOn    string // heavy line under the active tab
	Bar       string // vertical bar after the app name
	Paused    string
	Spark     string // eight steps of a line, lowest first
}

// UnicodeSymbols and ASCIISymbols are the two symbol sets.
var (
	UnicodeSymbols = Symbols{
		Filled: "█", Empty: "░", GPUMine: "■", GPUOther: "■", GPUFree: "□", GPUDown: "░", Cursor: "▶", Expanded: "▾",
		Collapsed: "▸", Check: "✓", Cross: "✗", Warn: "!", Dot: "·", Arrow: "→", Ellipsis: "…", Separator: "·",
		TabPrev: "‹", TabNext: "›", Idle: "☾", Refresh: "↻", Paused: "‖", Spark: "▁▂▃▄▅▆▇█", Rule: "─", RuleOn: "━", Bar: "│",
	}
	ASCIISymbols = Symbols{
		ASCII: true, Filled: "#", Empty: ".", GPUMine: "Y", GPUOther: "o", GPUFree: ".", GPUDown: "x", Cursor: ">", Expanded: "v",
		Collapsed: ">", Check: "+", Cross: "x", Warn: "!", Dot: "-", Arrow: "->", Ellipsis: "~", Separator: "|",
		TabPrev: "<", TabNext: ">", Idle: "z", Refresh: "@", Paused: "||", Spark: "_.-=+*#@", Rule: "-", RuleOn: "=", Bar: "|",
	}
)

// Theme is the complete look of the UI.
type Theme struct {
	P   Palette
	Sym Symbols

	Base      lipgloss.Style
	Bold      lipgloss.Style
	Muted     lipgloss.Style
	Faint     lipgloss.Style
	Accent    lipgloss.Style
	OK        lipgloss.Style
	Warn      lipgloss.Style
	Crit      lipgloss.Style
	Info      lipgloss.Style
	Header    lipgloss.Style
	TabActive lipgloss.Style
	Tab       lipgloss.Style
	Rule      lipgloss.Style
	Panel     lipgloss.Style
	PanelFoc  lipgloss.Style
	PanelHead lipgloss.Style
	Selected  lipgloss.Style
	ColHead   lipgloss.Style
	Key       lipgloss.Style
	Flash     lipgloss.Style
	FlashErr  lipgloss.Style
	Banner    lipgloss.Style
	Modal     lipgloss.Style
	Input     lipgloss.Style
	Match     lipgloss.Style
	Ghost     lipgloss.Style
}

// New builds a theme. name is a palette name (auto resolves with dark);
// noColor selects the monochrome palette; ascii selects ASCII symbols.
func New(name string, dark, noColor, ascii bool) Theme {
	if name == Auto || name == "" {
		name = Light
		if dark {
			name = Dark
		}
	}
	p, ok := palettes[name]
	if !ok {
		p = palettes[Dark]
	}
	if noColor {
		p = Palette{Name: Mono}
	}
	t := Theme{P: p, Sym: UnicodeSymbols}
	if ascii {
		t.Sym = ASCIISymbols
	}
	s := func() lipgloss.Style { return lipgloss.NewStyle() }
	fg := func(c color.Color) lipgloss.Style {
		if noColor || c == nil {
			return s()
		}
		return s().Foreground(c)
	}
	border := lipgloss.RoundedBorder()
	if ascii {
		border = lipgloss.ASCIIBorder()
	}

	t.Base = fg(p.Text)
	t.Bold = fg(p.Text).Bold(true)
	t.Muted = fg(p.Muted)
	t.Faint = fg(p.Faint)
	t.Accent = fg(p.Accent).Bold(true)
	t.OK = fg(p.OK)
	t.Warn = fg(p.Warn)
	t.Crit = fg(p.Crit).Bold(true)
	t.Info = fg(p.Info)
	t.Header = fg(p.Text).Bold(true)
	t.TabActive = fg(p.Accent).Bold(true)
	t.Tab = fg(p.Muted)
	t.Rule = fg(p.Faint)
	t.Panel = s().Border(border).BorderForeground(p.Border)
	t.PanelFoc = s().Border(border).BorderForeground(p.Accent)
	t.PanelHead = fg(p.Text).Bold(true)
	t.ColHead = fg(p.Muted).Bold(true)
	t.Key = fg(p.Accent).Bold(true)
	t.Flash = fg(p.OK)
	t.FlashErr = fg(p.Crit).Bold(true)
	t.Banner = s().Bold(true).Padding(0, 1)
	t.Modal = s().Border(border).BorderForeground(p.Accent).Padding(0, 2)
	t.Input = fg(p.Text)
	t.Match = fg(p.Accent).Bold(true).Underline(true)
	t.Ghost = fg(p.Faint)
	t.Selected = s().Reverse(true)
	if !noColor {
		t.Selected = s().Background(p.SelBg).Foreground(p.SelFg).Bold(true)
		t.Banner = t.Banner.Background(p.Crit).Foreground(lipgloss.Color("#FFFFFF"))
	} else {
		t.Banner = t.Banner.Reverse(true)
		t.TabActive = t.TabActive.Reverse(true)
		t.Crit = t.Crit.Underline(true)
	}
	if noColor || ascii {
		t.Panel = t.Panel.UnsetBorderForeground()
		t.PanelFoc = t.PanelFoc.UnsetBorderForeground().Bold(true)
	}
	return t
}

// HRule is a thin line w cells wide, dim so it separates without shouting.
func (t Theme) HRule(w int) string {
	if w <= 0 {
		return ""
	}
	return t.Rule.Render(strings.Repeat(t.Sym.Rule, w))
}

// Pick returns u, or a in ASCII mode.
func (t Theme) Pick(u, a string) string {
	if t.Sym.ASCII {
		return a
	}
	return u
}

// StateKind groups job states for display.
type StateKind int

// State groups.
const (
	KindNeutral StateKind = iota
	KindRunning
	KindPending
	KindCompleting
	KindCompleted
	KindFailed
	KindCancelled
	KindHeld
)

// KindOf maps a job state (and reason, for held jobs) to its group.
func KindOf(s model.JobState, reason string) StateKind {
	switch s {
	case model.StateRunning:
		return KindRunning
	case model.StatePending:
		if strings.HasPrefix(reason, "JobHeld") || strings.Contains(reason, "held") {
			return KindHeld
		}
		return KindPending
	case model.StateCompleting, model.StateConfiguring:
		return KindCompleting
	case model.StateCompleted:
		return KindCompleted
	case model.StateFailed, model.StateOOM, model.StateTimeout, model.StateNodeFail, model.StateBootFail, model.StateDeadline:
		return KindFailed
	case model.StateCancelled, model.StatePreempted:
		return KindCancelled
	case model.StateSuspended, model.StateStopped:
		return KindHeld
	}
	return KindNeutral
}

// StateIcon returns the icon for a state group.
func (t Theme) StateIcon(k StateKind) string {
	if t.Sym.ASCII {
		return map[StateKind]string{
			KindRunning: "R", KindPending: "P", KindCompleting: "G", KindCompleted: "C",
			KindFailed: "F", KindCancelled: "X", KindHeld: "H", KindNeutral: "?",
		}[k]
	}
	return map[StateKind]string{
		KindRunning: "●", KindPending: "◌", KindCompleting: "◐", KindCompleted: "✓",
		KindFailed: "✗", KindCancelled: "⊘", KindHeld: "‖", KindNeutral: "·",
	}[k]
}

// StateStyle returns the colour style for a state group.
func (t Theme) StateStyle(k StateKind) lipgloss.Style {
	switch k {
	case KindRunning:
		return t.OK
	case KindPending:
		return t.Warn
	case KindCompleting:
		return t.Info
	case KindCompleted:
		return t.Faint
	case KindFailed:
		return t.Crit
	case KindCancelled:
		return t.fg(t.P.Magenta)
	case KindHeld:
		return t.fg(t.P.Grey)
	}
	return t.Base
}

func (t Theme) fg(c color.Color) lipgloss.Style {
	if c == nil {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(c)
}

// shortStates are compact labels for narrow tables.
var shortStates = map[model.JobState]string{
	model.StateRunning: "RUN", model.StatePending: "PD", model.StateCompleting: "CG", model.StateCompleted: "CD",
	model.StateFailed: "FAIL", model.StateCancelled: "CA", model.StateTimeout: "TO", model.StateOOM: "OOM",
	model.StateNodeFail: "NF", model.StatePreempted: "PR", model.StateSuspended: "S", model.StateConfiguring: "CF",
	model.StateBootFail: "BF", model.StateDeadline: "DL", model.StateRequeued: "RQ", model.StateStopped: "ST",
}

// StateLabel renders "● RUN" style labels: icon plus a text label, coloured.
func (t Theme) StateLabel(s model.JobState, reason string, short bool) string {
	k := KindOf(s, reason)
	label := string(s)
	if short {
		if l, ok := shortStates[s]; ok {
			label = l
		}
		if k == KindHeld && s == model.StatePending {
			label = "HELD"
		}
	}
	return t.StateStyle(k).Render(t.StateIcon(k) + " " + label)
}

// Level styles a percentage: under 70% normal, 70–90% warning, above
// critical.
func (t Theme) Level(frac float64) lipgloss.Style {
	switch {
	case frac >= 0.9:
		return t.Crit
	case frac >= 0.7:
		return t.Warn
	}
	return t.OK
}

// EffLevel styles an efficiency: under 25% red, 25–60% yellow, above green.
func (t Theme) EffLevel(frac float64) lipgloss.Style {
	switch {
	case frac < 0:
		return t.Faint
	case frac < 0.25:
		return t.Crit
	case frac < 0.6:
		return t.Warn
	}
	return t.OK
}
