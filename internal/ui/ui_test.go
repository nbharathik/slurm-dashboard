package ui

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/demo"
	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/meta"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/notify"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

var update = flag.Bool("update", false, "rewrite UI snapshot files")

// t0 is the demo's start time in the snapshots.
var t0 = time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)

func TestMain(m *testing.M) {
	time.Local = time.UTC
	schedule = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	os.Exit(m.Run())
}

// fixture is a store loaded from the demo simulation at a fixed time.
type fixture struct {
	st     *state.Store
	src    *state.Sources
	clock  *state.FakeClock
	opt    Options
	quotas func() []model.Quota
}

func newFixture(t testing.TB, at time.Duration) *fixture {
	t.Helper()
	return newScenarioFixture(t, "default", at)
}

func newScenarioFixture(t testing.TB, name string, at time.Duration) *fixture {
	t.Helper()
	return newCustomFixture(t, name, at, nil)
}

// newCustomFixture is newScenarioFixture with a change to the scenario.
func newCustomFixture(t testing.TB, name string, at time.Duration, mutate func(*demo.Scenario)) *fixture {
	t.Helper()
	sc, err := demo.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(&sc)
	}
	clock := state.NewFakeClock(t0)
	sim := demo.New(sc, clock)
	r := sim.Runner()
	ctx := context.Background()
	caps, err := slurm.Probe(ctx, r, sc.User)
	if err != nil {
		t.Fatal(err)
	}
	src := &state.Sources{Runner: r, Cmd: slurm.Commands{Caps: caps, User: sc.User}}
	st := &state.Store{User: sc.User, UID: "1000", ClusterName: sc.Cluster, Caps: caps, Trends: insights.New(sim.TrendSamples(t0), t0), WarnWaste: true}
	f := &fixture{st: st, src: src, clock: clock}
	f.load(t)
	if at > 0 {
		clock.Advance(at)
		f.load(t)
	}
	f.opt = Options{
		Config: config.Default(), Store: st, Runner: r, Sources: src, Theme: "dark", Demo: true, DuRunner: r, HasIonice: true,
		Now: clock.Now, StateDir: "", CacheDir: t.TempDir(),
		// Stand-in for the jobdetail collector.
		OnViewState: func(msg tea.Msg) {
			m, ok := msg.(views.DetailMsg)
			if !ok || m.ID == "" {
				return
			}
			d := state.Detail{ID: m.ID}
			d.Job, _ = src.JobDetail(ctx, m.ID)
			d.Stat, _ = src.JobStat(ctx, m.ID)
			st.Apply(state.Update{Source: "jobdetail", Data: d, At: clock.Now()})
		},
	}
	f.quotas = sim.Quotas
	f.opt.LogFS = sim.LogFS()
	return f
}

// load runs every source once.
func (f *fixture) load(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	now := f.clock.Now()
	apply := func(source string, data any, err error) {
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		f.st.Apply(state.Update{Source: source, Data: data, At: now})
	}
	jobs, err := f.src.MyJobs(ctx)
	apply("myjobs", jobs, err)
	all, err := f.src.AllJobs(ctx)
	apply("alljobs", all, err)
	cl, err := f.src.Cluster(ctx, nil)
	apply("cluster", cl, err)
	q, err := f.src.QueueRank(ctx, state.PartitionsOfPending(jobs))
	apply("queuerank", q, err)
	n, err := f.src.Nodes(ctx)
	apply("nodes", n, err)
	p, err := f.src.Partitions(ctx)
	apply("partitions", p, err)
	res, err := f.src.Reservations(ctx)
	apply("reservations", res, err)
	if !f.st.Caps.HasSacct {
		return // like the dashboard: no accounting, no accounting sources
	}
	h, err := f.src.History(ctx, 7)
	apply("history", h, err)
	fs, err := f.src.Fairshare(ctx)
	apply("fairshare", fs, err)
	pr, err := f.src.Priorities(ctx)
	apply("sprio", pr, err)
	lim, err := f.src.Limits(ctx)
	apply("limits", lim, err)
	if f.st.Site.NoUsageGather() || !f.st.Caps.HasSstat {
		return
	}
	var ids []string
	for _, j := range jobs {
		if j.State == model.StateRunning && j.TimeUsed >= 25*time.Minute && strings.Trim(j.ID.Raw, "0123456789") == "" {
			ids = append(ids, j.ID.Raw)
		}
	}
	stats, err := f.src.MyStats(ctx, ids)
	apply("mystats", stats, err)
}

func (f *fixture) app(t testing.TB, w, h int, ascii bool) *App {
	t.Helper()
	opt := f.opt
	opt.ASCII = ascii
	a := New(opt)
	t.Cleanup(a.Close)
	a.st.Apply(state.Update{Source: "storage", Data: f.quotas(), At: f.clock.Now()})
	for _, v := range a.views {
		v.Refresh(a.ctx)
	}
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return a
}

// screen renders the app as plain text.
func screen(a *App) string { return ansi.Strip(a.View().Content) }

// press sends keys: single characters, or names like "enter" and "esc".
// Returned commands finish before the next key; tests disable scheduled ticks.
func press(a *App, keys ...string) {
	for _, k := range keys {
		_, cmd := a.Update(keyPress(k))
		drive(a, cmd, 0)
	}
}

const defaultDriveTimeout = 5 * time.Second

var driveTimeout = defaultDriveTimeout

func drive(a *App, cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 8 {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(driveTimeout):
		panic("UI test command timed out")
	}
	switch m := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range m {
			drive(a, c, depth+1)
		}
		return
	}
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		for i := range v.Len() {
			drive(a, v.Index(i).Interface().(tea.Cmd), depth+1)
		}
		return
	}
	_, next := a.Update(msg)
	drive(a, next, depth+1)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing snapshot (run go test ./internal/ui/... -update): %v", err)
	}
	if string(want) != got {
		t.Errorf("snapshot %s differs; review and run with -update if intended\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestSnapshots(t *testing.T) {
	f := newFixture(t, 0)
	sizes := []struct{ w, h int }{{60, 20}, {100, 30}, {160, 45}}
	for _, sz := range sizes {
		for _, ascii := range []bool{false, true} {
			for i, tb := range model.Tabs {
				tab := tb.Name
				charset := "utf8"
				if ascii {
					charset = "ascii"
				}
				name := fmt.Sprintf("%s-%dx%d-%s", tab, sz.w, sz.h, charset)
				t.Run(name, func(t *testing.T) {
					a := f.app(t, sz.w, sz.h, ascii)
					press(a, fmt.Sprint(i+1))
					got := screen(a)
					checkSize(t, got, sz.w, sz.h)
					if ascii {
						for i, r := range got {
							if r > 127 {
								t.Fatalf("non-ASCII %q at byte %d in ASCII mode:\n%s", r, i, got)
							}
						}
					}
					golden(t, name, got)
				})
			}
		}
	}
}

// TestDetailedSnapshots is every tab with layout = "detailed", the extra
// columns and lines that the clean layout leaves out.
func TestDetailedSnapshots(t *testing.T) {
	f := newFixture(t, 0)
	f.opt.Config.Layout = "detailed"
	for _, sz := range []struct{ w, h int }{{100, 30}, {160, 45}} {
		for i, tb := range model.Tabs {
			name := fmt.Sprintf("detailed-%s-%dx%d", tb.Name, sz.w, sz.h)
			t.Run(name, func(t *testing.T) {
				a := f.app(t, sz.w, sz.h, false)
				press(a, fmt.Sprint(i+1))
				got := screen(a)
				checkSize(t, got, sz.w, sz.h)
				golden(t, name, got)
			})
		}
	}
}

// TestMoreSnapshots covers the mixed demo cluster (CPU-only nodes, several
// GPU models, MIG, untracked memory) and the light and high-contrast
// themes.
func TestMoreSnapshots(t *testing.T) {
	hetero := newScenarioFixture(t, "hetero", 5*time.Minute)
	for _, tab := range []string{"1", "3", "4"} {
		name := "hetero-" + map[string]string{"1": "overview", "3": "queue", "4": "nodes"}[tab]
		t.Run(name, func(t *testing.T) {
			a := hetero.app(t, 140, 40, false)
			press(a, tab)
			got := screen(a)
			checkSize(t, got, 140, 40)
			golden(t, name, got)
		})
	}
	t.Run("hetero-node-detail", func(t *testing.T) {
		a := hetero.app(t, 140, 40, false)
		press(a, "4", "/", "m", "i", "g", "enter", "j", "enter")
		got := screen(a)
		for _, want := range []string{"1g.33gb", "1g.16gb", "not tracked", "Booted"} {
			if !strings.Contains(got, want) {
				t.Errorf("mig01 detail lacks %q:\n%s", want, got)
			}
		}
		golden(t, "hetero-node-detail", got)
	})
	basic := newScenarioFixture(t, "basic", time.Minute)
	for i, tb := range model.Tabs {
		name := "basic-" + tb.Name
		t.Run(name, func(t *testing.T) {
			a := basic.app(t, 100, 30, false)
			press(a, fmt.Sprint(i+1))
			got := screen(a)
			checkSize(t, got, 100, 30)
			if strings.Contains(got, "loading") {
				t.Errorf("%s still loading on a cluster without GPUs or accounting:\n%s", tb.Name, got)
			}
			golden(t, name, got)
		})
	}
	f := newFixture(t, 0)
	for _, th := range []string{"light", "high-contrast"} {
		for _, tab := range []string{"1", "4"} {
			name := fmt.Sprintf("theme-%s-%s", th, map[string]string{"1": "overview", "4": "nodes"}[tab])
			t.Run(name, func(t *testing.T) {
				f.opt.Theme = th
				a := f.app(t, 100, 30, false)
				press(a, tab)
				golden(t, name, a.View().Content) // with colours
			})
		}
	}
}

// checkSize asserts the frame is exactly w×h cells.
func checkSize(t *testing.T, s string, w, h int) {
	t.Helper()
	lines := strings.Split(s, "\n")
	if len(lines) != h {
		t.Errorf("frame has %d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if n := ansi.StringWidth(l); n > w {
			t.Errorf("line %d is %d cells wide, want <= %d: %q", i+1, n, w, l)
		}
	}
}

// TestKeyScripts covers key flows: palette, the exact
// command in the confirmation, esc closing it; plus details and overlays.
func TestKeyScripts(t *testing.T) {
	f := newFixture(t, 0)
	cases := []struct {
		name string
		keys []string
		want []string
	}{
		{"palette-cancel", []string{"2", ":", "c", "a", "n", "c", "e", "l", " ", ".", "enter"}, []string{"Cancel 1 job?", "scancel 812", "Keep (esc)"}},
		{"cancel-key", []string{"2", "c"}, []string{"scancel 812"}},
		{"confirm-esc", []string{"2", "c", "esc"}, []string{"812"}},
		{"palette-suggest", []string{":", "h", "o"}, []string{"hold"}},
		{"slash-palette", []string{"/", "h", "o"}, []string{"hold"}},
		{"jobs-filter", []string{"2", "/", "s", "t", "a", "t", "e", ":", "P", "D", "enter"}, []string{"filter: state:PD", "815_[3-9]"}},
		{"jobs-detail", []string{"2", "enter"}, []string{"RUNNING on gpu01", "Cancel", "All fields"}},
		{"why-pending", []string{"2", "j", "j", "j", "enter"}, []string{"PENDING", "Resources", "#2 of 5 in partition gpu"}},
		{"help", []string{"?"}, []string{"Keys", "Global", "quit"}},
		{"usage-card", []string{"5", "j", "j", "enter"}, []string{"Right-sizing", "#SBATCH --mem=8G"}},
		{"usage-sort-waste", []string{"5", ":", "s", "o", "r", "t", " ", "w", "a", "s", "t", "e", " ", "d", "e", "s", "c", "enter"}, []string{"last 7 days", "IDLE ▼"}},
		{"usage-alias", []string{":", "t", "a", "b", " ", "h", "i", "s", "t", "o", "r", "y", "enter"}, []string{"last 7 days", "Limits"}},
		{"storage-trend", []string{"6", "j", "enter"}, []string{"Trend", "growing about", "full in about 19 days"}},
		{"gpu-node", []string{"4", "j", "enter"}, []string{"Running jobs", "you", "H100"}},
		{"nodes-flat", []string{"4", "g"}, []string{"cpu04", "bad DIMM"}},
		{"nodes-gpu-only", []string{"4", "G"}, []string{"GPU nodes", "gpu01"}},
		{"nodes-cpu-only", []string{"4", "G", "G"}, []string{"CPU-only nodes", "cpu01"}},
		{"nodes-filter", []string{"4", "/", "s", "t", "a", "t", "e", ":", "d", "r", "a", "i", "n", "enter"}, []string{"state:drain", "cpu04"}},
		{"nodes-fold", []string{"4", "enter"}, []string{"▸ gpu", "cpu01"}},
		{"overview-cluster", []string{"tab", "enter"}, []string{"filter: part:gpu", "gpu01"}},
		{"queue", []string{"3"}, []string{"18 jobs", "waiting on", "running", "alice"}},
		{"queue-scope", []string{"3", "v", "v"}, []string{"Showing pending jobs", "showing pending", "Priority"}},
		{"queue-by-user", []string{"3", "g"}, []string{"Grouped by user", "carol"}},
		{"queue-filter-as-scope", []string{"3", "/", "s", "t", "a", "t", "e", ":", "P", "D", "enter"}, []string{"showing pending", "Priority"}},
		{"queue-estimate", []string{"3", "/", "u", "s", "e", "r", ":", "y", "o", "u", " ", "s", "t", "a", "t", "e", ":", "P", "D", "enter", "j", "e"}, []string{"expected to start"}},
		{"log-viewer", []string{"2", "l"}, []string{"stdout", "812 train-lora", "step", "following", "train-lora-812.out"}},
		{"log-search", []string{"2", "l", "/", "c", "h", "e", "c", "k", "p", "o", "i", "n", "t", "enter"}, []string{"search: checkpoint", "paused"}},
		{"log-stderr", []string{"2", "l", "e"}, []string{"stderr", "UserWarning"}},
		{"log-close", []string{"2", "l", "esc"}, []string{"11 jobs"}},
		{"script", []string{"2", "v"}, []string{"batch script", "#SBATCH --job-name=train-lora", "srun python"}},
		{"gpu-sample", []string{"2", "g"}, []string{"GPU use of 812", "NVIDIA H100", "Sampled once with: srun --jobid=812 --overlap"}},
		{"shell-demo", []string{"2", "t"}, []string{"not available in demo mode"}},
		{"settings", []string{","}, []string{"Settings", "Refresh", "normal", "my jobs 10s, cluster 30s", "Hide partitions", "Storage warning at", "the demo does not save"}},
		{"settings-manual", []string{",", "right", "right"}, []string{"manual", "load once, then press r"}},
		{"settings-partitions", []string{",", "down", "space"}, []string{"[x] gpu", "[ ] cpu"}},
		{"settings-order", []string{":", "s", "e", "t", "t", "i", "n", "g", "s", "enter", "esc", "2"}, []string{"11 jobs"}},
		{"rerun-form", []string{"5", "j", "n"}, []string{"Rerun 806 eval-bench", "Also passed: --account=", "Script (", "#SBATCH --job-name=eval-bench"}},
		{"rerun-edit", []string{"5", "j", "n", "j", "enter", "ctrl+u", "c", "p", "u", "enter"}, []string{"was gpu"}},
		{"rerun-invalid", []string{"5", "j", "n", "j", "j", "j", "j", "enter", "ctrl+u", "s", "o", "o", "n", "enter", "r"}, []string{"is not valid for --time"}},
		{"rerun-estimate", []string{"5", "j", "n", "s"}, []string{"Would start today at", "(scheduler estimate)"}},
		{"rerun-confirm", []string{"5", "j", "n", "r"}, []string{"Submit this rerun?", "sbatch --parsable --account=", "Directory:", "stdin"}},
		{"rerun-done", []string{"5", "j", "n", "r", "y"}, []string{"Submitted job 1000 (rerun of 806; @last)"}},
		{"rerun-suggested", []string{"5", "j", "n", "a", "r"}, []string{"--account=ml-lab --mem=3G", "--time=01:00:00"}},
		{"rerun-not-mine", []string{":", "r", "e", "r", "u", "n", " ", "9", "9", "9", "enter"}, []string{"Rerun works on your own jobs"}},
		{"du-confirm", []string{"6", "j", "a"}, []string{"Scan /scratch/you?", "lustre filesystem", "nice -n 19 ionice -c3 du -x -d1 -k -- /scratch/you"}},
		{"du-result", []string{"6", "j", "a", "y"}, []string{"Largest directories", "runs", "9.2T total"}},
		{"du-home", []string{"6", "a"}, []string{"Largest directories", "checkpoints"}},
		{"fairshare", []string{"tab", "tab", "j", "j", "j", "enter"}, []string{"Fairshare and priority", "fairshare 0.620", "815"}},
		{"why-array", []string{"6", "/", "w", "h", "y", " ", "8", "1", "5", "enter"}, []string{"815_[3-9]", "PENDING", "Resources"}},
		{"cancel-array", []string{":", "c", "a", "n", "c", "e", "l", " ", "8", "1", "5", "enter"}, []string{"Cancel 1 job?", "scancel 815_[3-9]"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := f.app(t, 120, 36, false)
			press(a, tc.keys...)
			got := screen(a)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("screen lacks %q:\n%s", w, got)
				}
			}
			if tc.name == "confirm-esc" && (a.confirm != nil || strings.Contains(got, "Cancel 1 job?")) {
				t.Errorf("esc did not close the confirmation:\n%s", got)
			}
			golden(t, "script-"+tc.name, got)
		})
	}
}

// TestResizeNeverPanics renders many sizes, including absurd ones.
func TestResizeNeverPanics(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 80, 24, false)
	for w := 0; w <= 220; w += 11 {
		for h := 0; h <= 70; h += 7 {
			for tab := 1; tab <= len(model.Tabs); tab++ {
				a.Update(tea.WindowSizeMsg{Width: w, Height: h})
				press(a, fmt.Sprint(tab))
				got := screen(a)
				if w > 0 && h > 0 {
					checkSize(t, got, w, h)
				}
			}
		}
	}
}

func FuzzResize(f *testing.F) {
	fx := newFixture(f, 0)
	f.Add(uint8(80), uint8(24), uint8(1), false)
	f.Add(uint8(40), uint8(12), uint8(2), true)
	f.Fuzz(func(t *testing.T, w, h, tab uint8, ascii bool) {
		a := fx.app(t, int(w), int(h), ascii)
		press(a, fmt.Sprint(int(tab)%len(model.Tabs)+1), "enter", "j", "tab")
		_ = screen(a)
	})
}

// TestScriptedTimeline advances the demo past the OOM failure and checks
// the Overview picks it up.
func TestScriptedTimeline(t *testing.T) {
	f := newFixture(t, 2*time.Minute)
	a := f.app(t, 120, 36, false)
	got := screen(a)
	if !strings.Contains(got, "816 preproc: OUT_OF_MEMORY") {
		t.Fatalf("OOM alert missing after 2 minutes:\n%s", got)
	}
}

// panicView is a view that panics on a key press, after the first frame
// has put the terminal into the alternate screen.
type panicView struct{ views.View }

func (p panicView) Update(ctx *viewsContext, msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		panic("boom")
	}
	return p.View.Update(ctx, msg)
}

func TestCrashWritesReportAndRestoresTerminal(t *testing.T) {
	f := newFixture(t, 0)
	crashPath = ""
	t.Cleanup(func() { crashPath = "" })
	opt := f.opt
	var out syncBuffer
	opt.Output = &out
	in, inW := io.Pipe()
	defer func() { _ = inW.Close() }()
	opt.Input = in
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = inW.Write([]byte("j"))
	}()
	opt.CacheDir = t.TempDir()
	opt.StartTab = "overview"
	testViewHook = func(a *App) {
		a.views[0] = panicView{View: a.views[0]}
		a.resize(80, 24) // no terminal, so no size message arrives
	}
	t.Cleanup(func() { testViewHook = nil })
	err := Run(context.Background(), opt)
	var ce *CrashError
	if !errors.As(err, &ce) {
		t.Fatalf("Run = %v, want CrashError", err)
	}
	b, rerr := os.ReadFile(ce.Path)
	if rerr != nil || !strings.Contains(string(b), "boom") {
		t.Fatalf("crash report %s: %v %q", ce.Path, rerr, b)
	}
	// Leaving the alternate screen restores the terminal.
	if !strings.Contains(out.String(), "\x1b[?1049l") {
		t.Fatalf("terminal not restored: %q", out.String())
	}
}

// syncBuffer is a bytes.Buffer safe for the renderer's goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestJobEndNotification follows the demo's OOM failure through the
// pipeline: transition, batched sacct lookup, flash, OSC 9 bytes and hook.
func TestJobEndNotification(t *testing.T) {
	f := newFixture(t, 0)
	var hooked []string
	f.opt.Hook = func(e notify.Event) error {
		hooked = append(hooked, e.JobID+" "+e.State)
		return nil
	}
	a := f.app(t, 120, 36, false)
	f.clock.Advance(2 * time.Minute)
	jobs, err := f.src.MyJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, cmd := a.Update(updateMsg(state.Update{Source: "myjobs", Data: jobs, At: f.clock.Now()}))
	if cmd == nil || len(a.ended) != 1 || !a.finalPending {
		t.Fatalf("ended = %+v pending %v", a.ended, a.finalPending)
	}
	_, cmd = a.Update(finalTickMsg{})
	msg := cmd()
	_, cmd = a.Update(msg)
	if !strings.Contains(a.flash.text, "816 preproc: OUT_OF_MEMORY") || !a.flash.err {
		t.Fatalf("flash = %+v", a.flash)
	}
	var raw strings.Builder
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch m := c().(type) {
		case tea.BatchMsg:
			for _, c := range m {
				run(c)
			}
		case nil:
		default:
			fmt.Fprintf(&raw, "%v", m)
		}
	}
	run(cmd)
	if !strings.Contains(raw.String(), "\x1b]9;Job out of memory: 816 preproc: OUT_OF_MEMORY") || !strings.Contains(raw.String(), "\a") {
		t.Fatalf("terminal bytes = %q", raw.String())
	}
	if len(hooked) != 1 || hooked[0] != "816 OUT_OF_MEMORY" {
		t.Fatalf("hook calls = %v", hooked)
	}
	if len(a.st.Ended) != 1 || !strings.Contains(screen(a), "816 preproc: OUT_OF_MEMORY") {
		t.Fatalf("store.Ended = %v", a.st.Ended)
	}
}

// With notify off there is no message, bell or desktop notice, but the
// notify_command hook still runs for the job end.
func TestNotifyOffStillRunsCommand(t *testing.T) {
	f := newFixture(t, 0)
	var hooked []string
	f.opt.Hook = func(e notify.Event) error {
		hooked = append(hooked, e.JobID+" "+e.State)
		return nil
	}
	f.opt.Config.Notify = false
	a := f.app(t, 120, 36, false)
	f.clock.Advance(2 * time.Minute)
	jobs, err := f.src.MyJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a.Update(updateMsg(state.Update{Source: "myjobs", Data: jobs, At: f.clock.Now()}))
	_, cmd := a.Update(finalTickMsg{})
	_, cmd = a.Update(cmd())
	var raw strings.Builder
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch m := c().(type) {
		case tea.BatchMsg:
			for _, c := range m {
				run(c)
			}
		case nil:
		default:
			fmt.Fprintf(&raw, "%v", m)
		}
	}
	run(cmd)
	if strings.Contains(a.flash.text, "preproc") || raw.Len() > 0 {
		t.Fatalf("notify = false still notified: flash %q bytes %q", a.flash.text, raw.String())
	}
	if len(hooked) != 1 || hooked[0] != "816 OUT_OF_MEMORY" {
		t.Fatalf("hook calls = %v", hooked)
	}
}

// The Settings screen applies a change at once and saves just that setting,
// keeping the rest of the user's file.
func TestSettingsSaveAndApply(t *testing.T) {
	f := newFixture(t, 0)
	path := filepath.Join(t.TempDir(), "sdash", "config.toml")
	f.opt.Demo = false
	f.opt.ConfigPath = path
	sched := state.NewScheduler(f.clock, 1)
	sched.Add(state.Func{N: "myjobs", I: func() time.Duration { return 10 * time.Second }, Fn: func(context.Context) (any, error) { return nil, nil }}, state.Options{})
	f.opt.Scheduler = sched
	f.opt.ApplyRefresh = func(iv config.Intervals) { sched.SetManual(iv.Manual) }
	a := f.app(t, 120, 36, false)

	press(a, ",", "right", "right") // normal, slow, manual
	if !sched.Manual() || !strings.Contains(screen(a), "Saved to") {
		t.Fatalf("manual not applied or saved:\n%s", screen(a))
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "refresh = \"manual\"\n") || strings.Contains(string(b), "theme") {
		t.Fatalf("file = %q, %v", b, err)
	}
	press(a, "esc")
	if s := screen(a); !strings.Contains(s, "manual") || a.settings != nil {
		t.Fatalf("badge:\n%s", firstLines(s, 2))
	}

	// Theme, plain symbols and a hidden partition all apply live; each is one
	// line in the file, and a hand-written comment survives.
	if err := os.WriteFile(path, append(b, []byte("mouse = true   # keep the mouse\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	press(a, ",")
	settingsTo(t, a, "theme")
	press(a, "right") // auto -> light
	settingsTo(t, a, "ascii")
	press(a, "right") // ascii on
	if a.opt.Config.Theme != "light" || !a.opt.Config.ASCII || !a.th.Sym.ASCII {
		t.Fatalf("theme/ascii not applied: %+v", a.opt.Config)
	}
	settingsTo(t, a, "hide_partitions")
	press(a, "space") // the first partition
	if len(a.opt.Config.HidePartitions) != 1 {
		t.Fatalf("hide_partitions = %v", a.opt.Config.HidePartitions)
	}
	b, _ = os.ReadFile(path)
	for _, want := range []string{"theme = \"light\"", "ascii = true", "hide_partitions = [\"", "mouse = true   # keep the mouse"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("file lacks %q:\n%s", want, b)
		}
	}
	got, issues, err := config.Load(path, func(string) string { return "" })
	if err != nil || len(issues) != 0 || got.Refresh != "manual" || got.Theme != "light" || !got.ASCII {
		t.Fatalf("reload = %+v %v %v", got, issues, err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("no backup: %v", err)
	}
}

// settingsTo moves the Settings cursor to the row of a setting.
func settingsTo(t *testing.T, a *App, key string) {
	t.Helper()
	for range 40 {
		if a.settings != nil && a.settings.rows[a.settings.cur].Key == key {
			return
		}
		press(a, "down")
	}
	t.Fatalf("no settings row %q", key)
}

// The warning level cannot be moved to the critical one, and a bad save is
// reported without losing the change for this session.
func TestSettingsRefusesBadStorageLevels(t *testing.T) {
	f := newFixture(t, 0)
	f.opt.Config.StorageWarn, f.opt.Config.StorageCrit = 96, 97
	a := f.app(t, 120, 36, false)
	press(a, ",")
	settingsTo(t, a, "storage_warn")
	press(a, "right") // 96 -> 97 would equal the critical level
	if a.opt.Config.StorageWarn != 96 || !strings.Contains(screen(a), "must stay below") {
		t.Fatalf("warn = %d\n%s", a.opt.Config.StorageWarn, screen(a))
	}

	f2 := newFixture(t, 0)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	f2.opt.Demo, f2.opt.ConfigPath = false, filepath.Join(dir, "sub", "config.toml")
	a2 := f2.app(t, 120, 36, false)
	press(a2, ",")
	settingsTo(t, a2, "theme")
	press(a2, "right")
	if os.Getuid() != 0 && (!strings.Contains(screen(a2), "not saved") || a2.opt.Config.Theme != "light") {
		t.Fatalf("a failed save:\n%s", screen(a2))
	}
}

// The Settings screen fits small terminals, in Unicode and ASCII, and never
// panics when it is taller than the space it has.
func TestSettingsSmallScreens(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {45, 14}, {40, 12}, {100, 30}} {
		for _, ascii := range []bool{false, true} {
			f := newFixture(t, 0)
			f.opt.ASCII = ascii
			a := f.app(t, size[0], size[1], false)
			press(a, ",")
			s := screen(a)
			if !strings.Contains(s, "Settings") && size[1] >= 14 {
				t.Errorf("%v ascii=%v: no Settings box:\n%s", size, ascii, s)
			}
			for i, line := range strings.Split(s, "\n") {
				if w := ansi.StringWidth(line); w > size[0] {
					t.Errorf("%v ascii=%v: line %d is %d wide: %q", size, ascii, i, w, line)
				}
			}
			press(a, "esc")
			if a.settings != nil {
				t.Errorf("%v: esc did not close", size)
			}
		}
	}
}

// On a cluster that stores no job scripts, rerunning a finished job says
// why in one line instead of failing with a Slurm error.
func TestRerunWithoutStoredScripts(t *testing.T) {
	f := newCustomFixture(t, "default", 0, func(sc *demo.Scenario) { sc.NoJobScripts = true })
	res, err := f.src.Runner.Run(context.Background(), "scontrol", "show", "config")
	if err != nil {
		t.Fatal(err)
	}
	if f.st.Site, err = parse.ClusterConfig(res.Stdout); err != nil {
		t.Fatal(err)
	}
	a := f.app(t, 120, 36, false)
	press(a, "5", "j", "n")
	if a.rerun != nil || !strings.Contains(a.flash.text, "does not store job scripts") || !a.flash.err {
		t.Fatalf("flash = %+v, rerun open = %v", a.flash, a.rerun != nil)
	}
}

func TestWarningNotices(t *testing.T) {
	f := newScenarioFixture(t, "hetero", 5*time.Minute)
	a := f.app(t, 140, 40, false)
	stats := state.Update{Source: "mystats", Data: f.st.MyStats.Data, At: f.clock.Now()}

	if cmd := a.applyUpdate(stats); cmd == nil {
		t.Error("a new warning should ring the bell")
	}
	for _, want := range []string{"4102 notebook uses 12% of its 4 CPUs", "ask for less next time"} {
		if !strings.Contains(a.flash.text, want) {
			t.Errorf("flash %q lacks %q", a.flash.text, want)
		}
	}
	// The same warning is announced once, not on every refresh.
	a.flash = flash{}
	a.applyUpdate(stats)
	if a.flash.text != "" {
		t.Errorf("announced twice: %q", a.flash.text)
	}

	// With notify off the alert stays on the Overview but nothing is said.
	quiet := newScenarioFixture(t, "hetero", 5*time.Minute)
	quiet.opt.Config.Notify = false
	b := quiet.app(t, 140, 40, false)
	b.applyUpdate(state.Update{Source: "mystats", Data: quiet.st.MyStats.Data, At: quiet.clock.Now()})
	if b.flash.text != "" {
		t.Errorf("notify off, but flashed %q", b.flash.text)
	}
	if !strings.Contains(screen(b), "4102 notebook uses 12% of its 4 CPUs") {
		t.Errorf("the alert should still show:\n%s", screen(b))
	}
}

func TestWarningSettingsApplyLive(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 140, 40, false)
	if !strings.Contains(screen(a), "checkpoint or save now") {
		t.Fatalf("time-limit warning missing:\n%s", screen(a))
	}
	cfg := a.opt.Config
	cfg.WarnTimeLeft, cfg.WarnWaste = "off", false
	a.applyConfig(cfg)
	for _, v := range a.views {
		v.Refresh(a.ctx)
	}
	if strings.Contains(screen(a), "checkpoint or save now") {
		t.Errorf("warn_time_left = off, but the warning shows:\n%s", screen(a))
	}
	if a.st.TimeLeftWarn >= 0 || a.st.WarnWaste {
		t.Errorf("store not updated: %v %v", a.st.TimeLeftWarn, a.st.WarnWaste)
	}
}

// Group, sort, kind and range choices made with keys are still there the
// next time sdash starts.
func TestViewChoicesAreRemembered(t *testing.T) {
	f := newFixture(t, 0)
	f.opt.StateDir = t.TempDir()
	a := f.app(t, 120, 36, false)
	press(a, "esc")              // the welcome card
	press(a, "4", "G", "g", "S") // GPU nodes only, not grouped, reversed
	press(a, "3", "S")           // Queue: reversed
	press(a, "2")
	_ = screen(a)      // draw the table, so the sort knows its columns
	press(a, "s")      // Jobs: sorted by the first column
	press(a, "5", "]") // Usage: 30 days

	raw, err := os.ReadFile(filepath.Join(f.opt.StateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"nodes_kind": "gpu"`, `"nodes_group": "no"`, `"nodes_sort": "name:desc"`, `"queue_sort"`, `"jobs_sort": "id"`, `"history_days": "30"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("state lacks %s:\n%s", want, raw)
		}
	}

	b := f.app(t, 120, 36, false) // a new start reads the file
	press(b, "4")
	if s := screen(b); !strings.Contains(s, "GPU nodes only") || !strings.Contains(s, "not grouped") || !strings.Contains(s, "sorted by name (reversed)") {
		t.Errorf("Nodes did not come back as it was:\n%s", screen(b))
	}
	press(b, "2")
	if s := screen(b); !strings.Contains(s, "sorted by id") {
		t.Errorf("Jobs did not come back as it was:\n%s", screen(b))
	}
	press(b, "5")
	if s := screen(b); !strings.Contains(s, "last 30 days") {
		t.Errorf("Usage did not come back as it was:\n%s", screen(b))
	}
}

// The layout setting changes the tables at once and is saved.
func TestLayoutSetting(t *testing.T) {
	f := newFixture(t, 0)
	path := filepath.Join(t.TempDir(), "sdash", "config.toml")
	f.opt.Demo, f.opt.ConfigPath = false, path
	a := f.app(t, 140, 40, false)
	press(a, "4")
	if s := screen(a); strings.Contains(s, "LOAD") || strings.Contains(s, "Partitions") {
		t.Fatalf("the clean layout shows the extras:\n%s", s)
	}
	press(a, ",")
	settingsTo(t, a, "layout")
	_, save := a.Update(keyPress("right"))
	if save == nil {
		t.Fatal("layout change must save the config")
	}
	// Check the save completes before leaving the settings screen.
	a.Update(save())
	press(a, "esc")
	if s := screen(a); !strings.Contains(s, "LOAD") || !strings.Contains(s, "Partitions") {
		t.Errorf("layout = detailed did not show the extras:\n%s", s)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), `layout = "detailed"`) {
		t.Errorf("file = %q, %v", b, err)
	}
}

// A short terminal scrolls the settings instead of cutting them off.
func TestSettingsScrollOnShortTerminals(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 80, 16, false)
	press(a, ",")
	if s := screen(a); !strings.Contains(s, "more below") || strings.Contains(s, "Shell in a job via") {
		t.Fatalf("the first screenful:\n%s", s)
	}
	settingsTo(t, a, "shell")
	s := screen(a)
	if !strings.Contains(s, "Shell in a job via") || !strings.Contains(s, "more above") {
		t.Errorf("the cursor row must show, with a marker for what is above:\n%s", s)
	}
}

// The footer never carries more than five hints, on any tab or state, and
// always ends with settings and help.
func TestFooterHasAtMostFiveHints(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 140, 40, false)
	for i, tb := range model.Tabs {
		for _, keys := range [][]string{{fmt.Sprint(i + 1)}, {fmt.Sprint(i + 1), "enter"}, {fmt.Sprint(i + 1), "j", "enter"}} {
			press(a, "esc")
			press(a, keys...)
			hints := a.footerBindings()
			if len(hints) > 5 {
				t.Errorf("%s %v: %d hints", tb.Name, keys, len(hints))
			}
			if n := len(hints); n < 2 || hints[n-2].Help().Desc != "settings" || hints[n-1].Help().Desc != "help" {
				t.Errorf("%s %v: the last two hints are not settings and help", tb.Name, keys)
			}
		}
		press(a, "esc")
	}
}

// The top bar names the app and lists every tab; the heavy stretch of the
// rule under it sits exactly under the open tab, in UTF-8 and in ASCII, wide
// and collapsed.
func TestTopBarMarksTheOpenTab(t *testing.T) {
	f := newFixture(t, 0)
	for _, ascii := range []bool{false, true} {
		heavy := "━"
		if ascii {
			heavy = "="
		}
		for _, w := range []int{50, 60, 100, 160} {
			a := f.app(t, w, 30, ascii)
			for i, tb := range model.Tabs {
				press(a, "esc")
				press(a, fmt.Sprint(i+1))
				lines := strings.Split(screen(a), "\n")
				bar, rule := lines[0], lines[1]
				if !strings.Contains(bar, meta.AppName) {
					t.Fatalf("%dx30 %s: no app name in %q", w, tb.Name, bar)
				}
				if w >= 100 {
					for _, other := range model.Tabs {
						if !strings.Contains(bar, other.Title) {
							t.Errorf("%dx30 %s: tab %q missing from %q", w, tb.Name, other.Title, bar)
						}
					}
				}
				start := strings.Index(rule, heavy)
				if start < 0 {
					t.Fatalf("%dx30 %s: no heavy stretch in %q", w, tb.Name, rule)
				}
				from := ansi.StringWidth(rule[:start])
				to := from + ansi.StringWidth(strings.TrimRight(rule[start:], "─-"))
				title := ansi.Cut(bar, from, to)
				if !strings.Contains(title, tb.Title) {
					t.Errorf("%dx30 %s (ascii=%v): the heavy rule sits under %q, not the tab\n%s\n%s", w, tb.Name, ascii, title, bar, rule)
				}
			}
		}
	}
}

// Everything sits inside a margin, and the footer keeps the hints on the
// left and how fresh the data is on the right.
func TestMarginsAndFooter(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 100, 30, false)
	lines := strings.Split(screen(a), "\n")
	for i, l := range lines {
		if i == 1 || i == len(lines)-2 { // the two rules run edge to edge
			continue
		}
		if l != "" && !strings.HasPrefix(l, "  ") && strings.TrimSpace(l) != "" {
			t.Errorf("line %d starts inside the margin: %q", i, l)
		}
		if strings.TrimSpace(l) != "" && ansi.StringWidth(strings.TrimRight(l, " ")) > 98 {
			t.Errorf("line %d runs into the right margin: %q", i, l)
		}
	}
	foot := lines[len(lines)-1]
	if !strings.Contains(foot, "? help") || strings.Contains(foot, "ago") {
		t.Errorf("Overview footer should keep help and leave freshness in the sections: %q", foot)
	}
	if rule := lines[len(lines)-2]; rule != strings.Repeat("─", 100) {
		t.Errorf("no rule above the footer: %q", rule)
	}
	// Squeezed, the hints win; settings and help stay.
	a = f.app(t, 44, 20, false)
	lines = strings.Split(screen(a), "\n")
	if foot := lines[len(lines)-1]; !strings.Contains(foot, ", settings") || !strings.Contains(foot, "? help") {
		t.Errorf("a narrow footer lost settings or help: %q", foot)
	}
}

// A partition's row is part of the table: its totals end in the same columns
// as the header and as the nodes under it, and folding it hides the nodes but
// not the row.
func TestNodesPartitionRowsShareTheColumns(t *testing.T) {
	f := newScenarioFixture(t, "hetero", 5*time.Minute)
	a := f.app(t, 140, 40, false)
	press(a, "4")
	lines := strings.Split(screen(a), "\n")
	var head string
	var rows []string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "CPU FREE") && head == "":
			head = l
		case head != "" && (strings.Contains(l, "▾") || strings.Contains(l, " up ") || strings.Contains(l, "01 ") || strings.Contains(l, "02 ")):
			rows = append(rows, l)
		}
	}
	if head == "" || len(rows) < 4 {
		t.Fatalf("no table found:\n%s", strings.Join(lines, "\n"))
	}
	// The right-aligned FREE columns end where their titles end: a cell with
	// text ends on that column and nothing spills into the gap after it.
	for _, title := range []string{"CPU FREE", "MEM FREE", "GPU FREE"} {
		end := ansi.StringWidth(head[:strings.Index(head, title)]) + len(title) // titles are ASCII
		for _, l := range rows {
			if next := ansi.Cut(l, end, end+1); next != " " {
				t.Errorf("%s: text at the gap after the column: %q", title, l)
			}
			if window := ansi.Cut(l, end-6, end); strings.Contains(window, "/") && ansi.Cut(l, end-1, end) == " " {
				t.Errorf("%s: the cell does not end under its title: %q", title, l)
			}
		}
	}
	// Folding the first partition hides its nodes only.
	before := strings.Count(screen(a), "cpu0")
	press(a, "left")
	after := screen(a)
	if strings.Count(after, "cpu0") >= before || !strings.Contains(after, "▸ cpu") {
		t.Errorf("collapse did not fold the partition (cpu0 rows %d -> %d):\n%s", before, strings.Count(after, "cpu0"), after)
	}
}
