package ui

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/logs"
	"github.com/nbharathik/slurm-dashboard/internal/meta"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/notify"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/keys"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

type viewsContext = views.Context

// testViewHook lets tests replace views after construction.
var testViewHook func(a *App)

// IdleAfter is how long without input before idle mode (intervals ×3).
const IdleAfter = 30 * time.Minute

// FlashFor is how long a flash message stays.
const FlashFor = 4 * time.Second

// Options configure the App.
type Options struct {
	Config    config.Config
	Store     *state.Store
	Scheduler *state.Scheduler // nil in tests that feed updates directly
	Runner    execx.Runner     // for actions
	Sources   *state.Sources
	History   func() []execx.CallRecord // runner call history for the debug overlay
	Warnings  func() map[string]int     // parse warnings for the debug overlay

	Theme    string // palette name; "" = config
	ASCII    bool
	NoMouse  bool
	NoColor  bool
	StartTab string
	Demo     bool
	// Notice is shown in the flash line at start (e.g. a deprecated flag).
	Notice string

	StateDir string
	CacheDir string
	Log      *slog.Logger
	Now      func() time.Time
	// Interval returns a source's configured interval (for freshness).
	Interval func(source string) time.Duration
	// ApplyRefresh puts a new refresh speed into effect while running.
	ApplyRefresh func(config.Intervals)
	// OnConfig is told the settings after each change made on the Settings
	// screen, for collectors that depend on them.
	OnConfig func(config.Config)
	// OnViewState tells the collectors about view state they depend on:
	// the open job detail, the jobs scope, the history range.
	OnViewState func(msg tea.Msg)
	// Getenv reads the environment (TMUX, SHELL, EDITOR, ...).
	Getenv func(string) string

	// LogFS reads job logs; nil means the real filesystem.
	LogFS logs.FS
	// Hook runs notify.on_job_end for an event (nil: no hook, e.g. demo).
	Hook func(e notify.Event) error
	// DuRunner runs the disk-usage analyser (10 min limit); HasIonice
	// says whether ionice exists.
	DuRunner  execx.Runner
	HasIonice bool
	// ConfigPath is the config file /config edits; ConfigLayers are all
	// the files read (site first) when it is reloaded.
	ConfigPath   string
	ConfigLayers []string
	// Doctor runs the environment checks for /doctor and returns them as
	// text.
	Doctor func() string

	// Input and Output replace the terminal (tests).
	Input  io.Reader
	Output io.Writer
}

type flash struct {
	text  string
	err   bool
	until time.Time
}

type overlay int

const (
	overlayNone overlay = iota
	overlayHelp
	overlayDebug
)

// App is the root Bubble Tea model.
type App struct {
	opt   Options
	st    *state.Store
	sched *state.Scheduler
	keys  *keys.Map
	th    theme.Theme
	zm    *zone.Manager
	ctx   *views.Context
	views []views.View
	tab   int
	w, h  int
	mode  layout.Mode

	dark      bool
	themeName string
	flash     flash
	overlay   overlay
	// overlayScroll is the first visible line of the help or debug overlay.
	overlayScroll int
	lastInput     time.Time
	idle          bool
	palette       *Palette
	confirm       *Confirm
	state         *uiState
	info          *infoBox

	// The log or script viewer covers the tab body while open.
	logView   *views.LogView
	logReader *logs.Reader
	logPaths  [2]string // stdout, stderr
	logGen    int       // stops the polls of a closed viewer

	// duCancel stops a running disk-usage analysis.
	duCancel context.CancelFunc

	// The rerun form covers the tab body while open.
	rerun *views.RerunView

	// settings is the Settings screen (,), a box over the tab body.
	settings *settingsState

	// estimates caches start estimates by job ID.
	estimates map[string]startEstimateMsg

	// Jobs that left the queue, awaiting their final state from sacct.
	ended        []endedJob
	warned       map[string]bool // time-limit and idle-job alerts already announced
	finalPending bool
	lastFinal    time.Time
}

// New builds the App.
func New(opt Options) *App {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if opt.Getenv == nil {
		opt.Getenv = func(string) string { return "" }
	}
	if opt.Interval == nil {
		opt.Interval = func(string) time.Duration { return 10 * time.Second }
	}
	cfg := opt.Config
	a := &App{
		opt:       opt,
		st:        opt.Store,
		sched:     opt.Scheduler,
		keys:      keys.Default(),
		zm:        zone.New(),
		dark:      true,
		themeName: themeFor(opt.Theme, cfg.Theme),
		lastInput: opt.Now(),
		state:     loadState(opt.StateDir),
	}
	a.zm.SetEnabled(!opt.NoMouse && cfg.Mouse)
	a.rebuildTheme()
	a.ctx = &views.Context{
		Store: a.st, Theme: a.th, Keys: a.keys, Zones: a.zm, Config: cfg, Now: opt.Now(),
		Dismissed: a.state.Dismissed, Prefs: a.state.Prefs,
	}
	a.views = views.All(a.ctx)
	if u, ok := a.viewByName(model.TabUsage).(interface{ Days() int }); ok && opt.OnViewState != nil {
		opt.OnViewState(views.HistoryRangeMsg{Days: u.Days()}) // the range chosen last time
	}
	if testViewHook != nil {
		testViewHook(a)
	}
	a.openTab(firstNonEmpty(opt.StartTab, cfg.StartTab, a.state.LastTab))
	if opt.Notice != "" {
		a.setFlash(opt.Notice, false)
	}
	a.welcome()
	return a
}

// applyConfig puts a changed config into effect at once: theme, symbols,
// mouse and the refresh speed.
func (a *App) applyConfig(cfg config.Config) {
	a.opt.Config, a.ctx.Config = cfg, cfg
	a.themeName = themeFor(a.opt.Theme, cfg.Theme)
	a.zm.SetEnabled(!a.opt.NoMouse && cfg.Mouse)
	a.rebuildTheme()
	a.applyRefresh()
	// The alert rules read their thresholds from the store.
	a.st.StorageWarn, a.st.StorageCrit = cfg.StorageWarn, cfg.StorageCrit
	a.st.TimeLeftWarn, a.st.WarnWaste = cfg.TimeLeftWarn(), cfg.WarnWaste
	if a.opt.OnConfig != nil {
		a.opt.OnConfig(cfg)
	}
}

// themeFor picks the theme: the configured one, unless it is auto and the
// program was started with a fixed theme (tests).
func themeFor(forced, configured string) string {
	if configured == "" || configured == theme.Auto {
		return firstNonEmpty(forced, theme.Auto)
	}
	return configured
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// tabIndex finds a tab by name or alias ("gpus" is Nodes); 0 when unknown.
func (a *App) tabIndex(name string) int {
	if t, _, ok := model.ResolveTab(name); ok {
		name = t
	}
	for i, v := range a.views {
		if v.Name() == name {
			return i
		}
	}
	return 0
}

// openTab switches to a tab by name or alias; "gpus" also turns on the
// Nodes tab's GPU filter.
func (a *App) openTab(name string) {
	i := a.tabIndex(name)
	if a.views == nil || i >= len(a.views) {
		return
	}
	if a.tab != i {
		a.switchTab(i)
	}
	a.tab = i
	if _, gpu, _ := model.ResolveTab(name); gpu {
		if g, ok := a.views[i].(interface{ ShowGPUNodes(*views.Context) }); ok {
			g.ShowGPUNodes(a.ctx)
		}
	}
}

func (a *App) rebuildTheme() {
	ascii := a.opt.ASCII || a.opt.Config.ASCII
	noColor := a.opt.NoColor
	a.th = theme.New(a.themeName, a.dark, noColor, ascii)
	if a.ctx != nil {
		a.ctx.Theme = a.th
		for _, v := range a.views {
			v.Refresh(a.ctx)
		}
	}
}

// Messages internal to the app.
type (
	updateMsg state.Update
	tickMsg   time.Time
)

func (a *App) waitUpdate() tea.Cmd {
	if a.sched == nil {
		return nil
	}
	ch := a.sched.Updates()
	return func() tea.Msg {
		u, ok := <-ch
		if !ok {
			return nil
		}
		return updateMsg(u)
	}
}

// schedule is replaced by tests that advance time explicitly.
var schedule = tea.Tick

func tick() tea.Cmd {
	return schedule(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init starts the update loop, the clock and background colour detection.
func (a *App) Init() tea.Cmd {
	a.syncVisible()
	cmds := []tea.Cmd{a.waitUpdate(), tick()}
	if a.themeName == theme.Auto {
		cmds = append(cmds, tea.RequestBackgroundColor)
	}
	return tea.Batch(cmds...)
}

// Update handles every message.
func (a *App) Update(msg tea.Msg) (m tea.Model, cmd tea.Cmd) {
	defer a.crashGuard()
	a.ctx.Now = a.now()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		return a, nil
	case tea.BackgroundColorMsg:
		a.dark = msg.IsDark()
		a.rebuildTheme()
		return a, nil
	case updateMsg:
		return a, tea.Batch(a.applyUpdate(state.Update(msg)), a.waitUpdate())
	case tickMsg:
		a.checkIdle()
		return a, tick()
	case tea.FocusMsg:
		a.setIdle(false)
		return a, nil
	case tea.BlurMsg:
		a.setIdle(true)
		return a, nil
	case tea.KeyPressMsg:
		a.touch()
		return a, a.handleKey(msg)
	case tea.MouseClickMsg, tea.MouseWheelMsg:
		a.touch()
		return a, a.handleMouse(msg.(tea.MouseMsg))
	case tea.MouseMsg:
		return a, nil
	}
	return a, a.handleRequest(msg)
}

func (a *App) resize(w, h int) {
	a.w, a.h = w, h
	a.mode = layout.ModeFor(w, h)
	a.ctx.Mode, a.ctx.Width, a.ctx.Height = a.mode, w, h
}

// applyUpdate records collector results and refreshes the views.
func (a *App) applyUpdate(u state.Update) tea.Cmd {
	transitions := a.st.Apply(u)
	if u.Err != nil {
		a.opt.Log.Debug("source failed", "source", u.Source, "err", u.Err)
	}
	for _, v := range a.views {
		v.Refresh(a.ctx)
	}
	cmd := a.onTransitions(transitions)
	if u.Source == "myjobs" || u.Source == "mystats" {
		cmd = tea.Batch(cmd, a.warnNotices())
	}
	return cmd
}

func (a *App) touch() {
	a.lastInput = a.now()
	if a.idle {
		a.setIdle(false)
	}
}

func (a *App) checkIdle() {
	if !a.idle && a.now().Sub(a.lastInput) >= IdleAfter {
		a.setIdle(true)
	}
}

func (a *App) setIdle(idle bool) {
	a.idle = idle
	if a.sched != nil {
		a.sched.SetIdle(idle)
	}
}

func (a *App) syncVisible() {
	if a.sched != nil {
		a.sched.SetVisible(a.views[a.tab].Name())
	}
}

func (a *App) setFlash(text string, isErr bool) {
	a.flash = flash{text: text, err: isErr, until: a.now().Add(FlashFor)}
}

func (a *App) switchTab(i int) {
	if i < 0 || i >= len(a.views) || i == a.tab {
		return
	}
	a.tab = i
	a.state.LastTab = a.views[i].Name()
	a.syncVisible()
	a.views[i].Refresh(a.ctx)
	// Sources that only poll while their tab is open catch up at once.
	src := a.views[i].Source()
	if interval, at, has, _, _ := a.sourceState(src); !has || a.now().Sub(at) > interval {
		a.refresh(src)
	}
}

// handleKey routes a key press: confirmation and palette first, then
// overlays, then text input in the view, then global keys, then the view.
func (a *App) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return a.quit()
	}
	if a.confirm != nil {
		return a.updateConfirm(msg)
	}
	if a.palette != nil {
		return a.updatePalette(msg)
	}
	if a.settings != nil {
		return a.updateSettings(msg)
	}
	if a.info != nil {
		if a.info.anyKey || key.Matches(msg, a.keys.Back, a.keys.Open, a.keys.Quit) {
			a.info = nil
		}
		return nil
	}
	if a.overlay != overlayNone {
		switch {
		case key.Matches(msg, a.keys.Back, a.keys.Help, a.keys.Debug, a.keys.Quit):
			a.overlay, a.overlayScroll = overlayNone, 0
		case key.Matches(msg, a.keys.Up):
			a.overlayScroll--
		case key.Matches(msg, a.keys.Down):
			a.overlayScroll++
		case key.Matches(msg, a.keys.PageUp):
			a.overlayScroll -= 10
		case key.Matches(msg, a.keys.PageDown):
			a.overlayScroll += 10
		}
		return nil
	}
	if rv := a.rerun; rv != nil {
		return rv.Update(a.ctx, msg)
	}
	if lv := a.logView; lv != nil {
		if !lv.Capturing() && key.Matches(msg, a.keys.Help) {
			a.overlay = overlayHelp
			return nil
		}
		return lv.Update(a.ctx, msg)
	}
	v := a.views[a.tab]
	if v.Capturing() {
		return v.Update(a.ctx, msg)
	}
	for i, t := range a.keys.Tabs {
		if key.Matches(msg, t) {
			a.switchTab(i)
			return nil
		}
	}
	_, search := v.(interface{ HasSearch() })
	switch {
	case key.Matches(msg, a.keys.Quit):
		return a.quit()
	case msg.String() == "/" && !search:
		// Tabs without a table filter keep / for the command line.
		a.openPalette("")
	case key.Matches(msg, a.keys.Help):
		a.overlay = overlayHelp
	case key.Matches(msg, a.keys.Debug):
		a.overlay = overlayDebug
	case key.Matches(msg, a.keys.Palette):
		a.openPalette("")
	case key.Matches(msg, a.keys.Refresh):
		a.refresh(a.visibleSources()...)
	case key.Matches(msg, a.keys.RefreshAll):
		a.refresh()
		a.setFlash("Refreshing everything", false)
	case key.Matches(msg, a.keys.Pause):
		a.togglePause()
	case key.Matches(msg, a.keys.Settings):
		a.openSettings()
	default:
		return v.Update(a.ctx, msg)
	}
	return nil
}

func (a *App) quit() tea.Cmd {
	saveState(a.opt.StateDir, a.state)
	return tea.Quit
}

// visibleSources are the sources the visible tab shows.
func (a *App) visibleSources() []string {
	if s, ok := a.views[a.tab].(interface{ Sources() []string }); ok {
		return s.Sources()
	}
	return []string{a.views[a.tab].Source()}
}

func (a *App) refresh(sources ...string) {
	if a.sched != nil {
		a.sched.Refresh(sources...)
	}
}

// handleMouse resolves clicks on the chrome, then passes the event to the
// view.
func (a *App) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if a.confirm != nil {
		return a.mouseConfirm(msg)
	}
	if a.palette != nil {
		return a.mousePalette(msg)
	}
	if a.settings != nil {
		return a.mouseSettings(msg)
	}
	if a.info != nil {
		if _, ok := msg.(tea.MouseClickMsg); ok {
			a.info = nil
		}
		return nil
	}
	if a.overlay != overlayNone {
		switch m := msg.(type) {
		case tea.MouseClickMsg:
			a.overlay, a.overlayScroll = overlayNone, 0
		case tea.MouseWheelMsg:
			switch m.Button {
			case tea.MouseWheelUp:
				a.overlayScroll -= 3
			case tea.MouseWheelDown:
				a.overlayScroll += 3
			}
		}
		return nil
	}
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
		switch {
		case a.ctx.InZone(zoneRefresh, msg):
			a.refresh(a.visibleSources()...)
			return nil
		case a.ctx.InZone(zoneTabPrev, msg):
			a.switchTab((a.tab + len(a.views) - 1) % len(a.views))
			return nil
		case a.ctx.InZone(zoneTabNext, msg):
			a.switchTab((a.tab + 1) % len(a.views))
			return nil
		}
		for i, v := range a.views {
			if a.ctx.InZone(zoneTab(v.Name()), msg) {
				a.switchTab(i)
				return nil
			}
		}
		if a.rerun != nil {
			return a.rerun.Update(a.ctx, msg)
		}
		if a.logView != nil {
			for _, h := range a.logView.Hints(a.ctx) {
				if len(h.Keys()) > 0 && a.ctx.InZone(zoneHint(h.Keys()[0]), msg) {
					return a.handleKey(keyPress(h.Keys()[0]))
				}
			}
			return a.logView.Update(a.ctx, msg)
		}
		for _, h := range a.footerBindings() {
			if len(h.Keys()) > 0 && a.ctx.InZone(zoneHint(h.Keys()[0]), msg) {
				return a.handleKey(keyPress(h.Keys()[0]))
			}
		}
	}
	if a.logView != nil {
		return a.logView.Update(a.ctx, msg)
	}
	return a.views[a.tab].Update(a.ctx, msg)
}

// footerBindings are the footer's hints: at most five from the view, then
// the command line and help. Clicks resolve against the same list.
func (a *App) footerBindings() []key.Binding {
	hints := a.views[a.tab].Hints(a.ctx)
	return append(hints[:min(len(hints), 3):min(len(hints), 3)], a.keys.Settings, a.keys.Help)
}

// keyPress builds the key message a clickable hint stands for.
func keyPress(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	if strings.HasPrefix(k, "ctrl+") && len(k) == 6 {
		return tea.KeyPressMsg{Code: rune(k[5]), Mod: tea.ModCtrl}
	}
	r := []rune(k)
	if len(r) == 1 {
		p := tea.KeyPressMsg{Code: r[0], Text: k}
		if r[0] >= 'A' && r[0] <= 'Z' {
			p.Code, p.ShiftedCode, p.Mod = r[0]+32, r[0], tea.ModShift
		}
		return p
	}
	return tea.KeyPressMsg{}
}

// View renders the whole screen.
func (a *App) View() tea.View {
	defer a.crashGuard()
	a.ctx.Now = a.now()
	var v tea.View
	v.AltScreen = true
	v.ReportFocus = true
	if a.zm.Enabled() {
		v.MouseMode = tea.MouseModeCellMotion
	}
	title := meta.AppName
	if a.st.ClusterName != "" {
		title += " · " + a.st.ClusterName
	}
	v.WindowTitle = title
	v.SetContent(a.zm.Scan(a.render()))
	return v
}

func (a *App) render() string {
	w, h := a.w, a.h
	if w == 0 || h == 0 {
		return ""
	}
	if a.mode == layout.TooSmall {
		msg := a.th.Warn.Render("Terminal too small") + "\n" + a.th.Muted.Render("need 40"+a.th.Pick("×", "x")+"12, have ") + strconv.Itoa(w) + a.th.Pick("×", "x") + strconv.Itoa(h)
		return layout.FitLines(centred(msg, w, h), w, h)
	}
	// Top bar and its rule, the rule above the footer and the footer itself;
	// a blank line under the top bar when the terminal is tall enough.
	gap := 0
	if h >= 28 {
		gap = 1
	}
	bodyH := h - 4 - gap
	m := layout.Margin(w)
	inner := w - 2*m
	var body string
	if a.rerun != nil {
		body = a.rerun.Render(a.ctx, inner, bodyH)
	} else if a.logView != nil {
		body = a.logView.Render(a.ctx, inner, bodyH)
	} else {
		body = a.views[a.tab].Render(a.ctx, inner, bodyH)
	}
	body = layout.FitLines(layout.Indent(layout.FitLines(body, inner, bodyH), m), w, bodyH)
	switch {
	case a.confirm != nil:
		body = layout.Overlay(body, a.confirm.Render(a.th, a.ctx, w), w, bodyH)
	case a.palette != nil:
		body = a.palette.Overlay(a.th, a.ctx, body, w, bodyH)
	case a.settings != nil:
		body = layout.Overlay(body, a.settingsBox(w, bodyH), w, bodyH)
	case a.overlay == overlayHelp:
		body = layout.Overlay(body, a.helpBox(w, bodyH), w, bodyH)
	case a.overlay == overlayDebug:
		body = layout.Overlay(body, a.debugBox(w, bodyH), w, bodyH)
	case a.info != nil:
		text := a.info.body
		if !a.info.anyKey {
			text += "\n\n" + a.th.Muted.Render("esc closes")
		}
		box := components.Modal(a.th, a.info.title, text, nil, min(w-2, 90))
		body = layout.Overlay(body, box, w, bodyH)
	}
	bar, rule := a.topBar(w)
	lines := []string{bar, rule}
	if gap > 0 {
		lines = append(lines, strings.Repeat(" ", w))
	}
	lines = append(lines, body, a.th.HRule(w), a.footer(w))
	return strings.Join(lines, "\n")
}

func centred(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	top := max((h-len(lines))/2, 0)
	var out []string
	for range top {
		out = append(out, "")
	}
	for _, l := range lines {
		pad := max((w-layout.Width(l))/2, 0)
		out = append(out, strings.Repeat(" ", pad)+l)
	}
	return strings.Join(out, "\n")
}

// sourceState returns what the header needs about a source.
func (a *App) sourceState(src string) (interval time.Duration, at time.Time, has bool, tried time.Time, err error) {
	interval = a.opt.Interval(src)
	s := a.st
	get := func(e error, t time.Time, h bool, tr time.Time) {
		err, at, has, tried = e, t, h, tr
	}
	switch src {
	case "myjobs":
		get(s.MyJobs.Err, s.MyJobs.At, s.MyJobs.Has, s.MyJobs.Tried)
	case "alljobs":
		get(s.AllJobs.Err, s.AllJobs.At, s.AllJobs.Has, s.AllJobs.Tried)
	case "nodes":
		get(s.Nodes.Err, s.Nodes.At, s.Nodes.Has, s.Nodes.Tried)
	case "cluster":
		get(s.Cluster.Err, s.Cluster.At, s.Cluster.Has, s.Cluster.Tried)
	case "history":
		get(s.History.Err, s.History.At, s.History.Has, s.History.Tried)
	case "storage":
		get(s.Storage.Err, s.Storage.At, s.Storage.Has, s.Storage.Tried)
	}
	return interval, at, has, tried, err
}

func (a *App) freshnessOf(src string, interval time.Duration) state.Freshness {
	_, at, has, _, err := a.sourceState(src)
	switch {
	case err != nil:
		return state.Failing
	case !has:
		return state.Unknown
	case a.now().Sub(at) > 2*interval:
		return state.Stale
	}
	return state.Fresh
}

func (a *App) schedStates() []state.State {
	if a.sched == nil {
		return nil
	}
	return a.sched.States()
}

// Close releases the zone manager.
func (a *App) Close() { a.zm.Close() }

// Run starts the TUI and blocks until the user quits.
func Run(ctx context.Context, opt Options) error {
	a := New(opt)
	defer a.Close()
	popts := []tea.ProgramOption{tea.WithContext(ctx)}
	if opt.Input != nil {
		popts = append(popts, tea.WithInput(opt.Input))
	}
	if opt.Output != nil {
		popts = append(popts, tea.WithOutput(opt.Output))
	}
	p := tea.NewProgram(a, popts...)
	_, err := p.Run()
	if crashPath != "" {
		return &CrashError{Path: crashPath, Err: err}
	}
	return err
}
