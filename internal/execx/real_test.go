package execx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// testRunner returns a RealRunner that may run the harmless local tools the
// tests use, and nothing from the cluster.
func testRunner(opts Options) *RealRunner {
	opts.Policy.ExtraReadOnly = append(opts.Policy.ExtraReadOnly, "sh", "env", "yes", "true")
	return NewReal(opts)
}

func TestSanitizeEnv(t *testing.T) {
	in := []string{
		"HOME=/home/alice",
		"SQUEUE_FORMAT=%i %j",
		"SQUEUE_FORMAT2=JobID",
		"SINFO_FORMAT=%P",
		"SACCT_FORMAT=JobID",
		"SSTAT_FORMAT=x",
		"SPRIO_FORMAT=x",
		"SSHARE_FORMAT=x",
		"SLURM_TIME_FORMAT=relative",
		"SLURM_CONF=/etc/slurm/slurm.conf",
		"LC_ALL=de_DE.UTF-8",
		"LANG=de_DE.UTF-8",
		"PATH=/usr/bin",
		"MY_SQUEUE_FORMAT=kept", // only prefixes are dropped
		"=weird",
	}
	got := SanitizeEnv(in)
	want := []string{
		"HOME=/home/alice",
		"SLURM_CONF=/etc/slurm/slurm.conf",
		"LANG=de_DE.UTF-8",
		"PATH=/usr/bin",
		"MY_SQUEUE_FORMAT=kept",
		"LC_ALL=C",
		"SLURM_TIME_FORMAT=standard",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("SanitizeEnv:\n got %q\nwant %q", got, want)
	}
	if &in[0] == &got[0] {
		t.Fatal("SanitizeEnv must not alias its input")
	}
}

func TestRealRunnerChildEnvIsSanitised(t *testing.T) {
	r := testRunner(Options{BaseEnv: []string{
		"PATH=" + os.Getenv("PATH"),
		"SQUEUE_FORMAT=%i",
		"SACCT_FORMAT=JobID",
		"SLURM_TIME_FORMAT=relative",
		"SLURM_CONF=/etc/slurm/slurm.conf",
	}})
	res, err := r.Run(context.Background(), "env")
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	env := strings.Split(strings.TrimSpace(string(res.Stdout)), "\n")
	for _, bad := range []string{"SQUEUE_FORMAT=%i", "SACCT_FORMAT=JobID", "SLURM_TIME_FORMAT=relative"} {
		if slices.Contains(env, bad) {
			t.Errorf("child saw %q", bad)
		}
	}
	for _, good := range []string{"LC_ALL=C", "SLURM_TIME_FORMAT=standard", "SLURM_CONF=/etc/slurm/slurm.conf"} {
		if !slices.Contains(env, good) {
			t.Errorf("child did not see %q; env=%q", good, env)
		}
	}
}

func TestRealRunnerSuccessAndExitError(t *testing.T) {
	r := testRunner(Options{})
	res, err := r.Run(context.Background(), "sh", "-c", "printf 'a\x1fb'")
	if err != nil || string(res.Stdout) != "a\x1fb" || res.ExitCode != 0 {
		t.Fatalf("Run = %+v, %v", res, err)
	}

	res, err = r.Run(context.Background(), "sh", "-c", "echo out; echo '' >&2; echo 'scancel: error: Kill job error' >&2; exit 3")
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want *ExitError, got %v", err)
	}
	if ee.Code != 3 || ee.Name != "sh" || ee.Stderr != "scancel: error: Kill job error" {
		t.Fatalf("ExitError = %+v", ee)
	}
	if res.ExitCode != 3 || string(res.Stdout) != "out\n" {
		t.Fatalf("result not populated on exit error: %+v", res)
	}
}

func TestRealRunnerNotFound(t *testing.T) {
	r := testRunner(Options{Policy: Policy{ExtraReadOnly: []string{"sdash-no-such-command"}}})
	_, err := r.Run(context.Background(), "sdash-no-such-command")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	_, err = r.Run(context.Background(), "/nonexistent/dir/sdash-no-such-command")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("absolute path: want ErrNotFound, got %v", err)
	}
}

// TestRealRunnerTimeoutKillsProcessGroup starts a shell that forks a
// long-running child and waits. On timeout the whole group must die: Run
// has to return well before WaitDelay (2 s) would force the pipes closed,
// and the grandchild must be gone.
func TestRealRunnerTimeoutKillsProcessGroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses /proc")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	r := testRunner(Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := r.Run(ctx, "sh", "-c", `sleep 30 & echo $! > "$1"; wait`, "sh", pidFile)
	took := time.Since(start)

	if !errors.Is(err, ErrTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want ErrTimeout wrapping DeadlineExceeded, got %v", err)
	}
	if took >= 1500*time.Millisecond {
		t.Fatalf("Run took %s; the grandchild kept the pipes open, so the group was not killed", took)
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("reading pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("pid file %q: %v", raw, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !processGone(pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d survived the timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// processGone reports whether pid no longer runs. A zombie counts as gone:
// it was killed and only waits for a reaper, which in a container may never
// come.
func processGone(pid int) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	s := string(raw)
	i := strings.LastIndexByte(s, ')')
	return i >= 0 && i+2 < len(s) && s[i+2] == 'Z'
}

func TestRealRunnerDefaultTimeout(t *testing.T) {
	r := testRunner(Options{DefaultTimeout: 200 * time.Millisecond})
	_, err := r.Run(context.Background(), "sh", "-c", "sleep 5")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout from the default timeout, got %v", err)
	}
}

func TestRealRunnerCancel(t *testing.T) {
	r := testRunner(Options{})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := r.Run(ctx, "sh", "-c", "sleep 5")
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrTimeout) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestRealRunnerOutputCap(t *testing.T) {
	r := testRunner(Options{MaxStdout: 1024, DefaultTimeout: 10 * time.Second})
	start := time.Now()
	res, err := r.Run(context.Background(), "yes")
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("want ErrOutputTooLarge, got %v", err)
	}
	if len(res.Stdout) != 1024 {
		t.Fatalf("kept %d bytes, want 1024", len(res.Stdout))
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("overflow did not stop the command promptly")
	}
}

// TestRealRunnerOutputCapJustOver covers a command that writes one byte
// past the cap and exits before the kill lands: the result must still be
// an error, never truncated output that looks complete.
func TestRealRunnerOutputCapJustOver(t *testing.T) {
	r := testRunner(Options{MaxStdout: 1024})
	for range 20 {
		res, err := r.Run(context.Background(), "sh", "-c", "printf '%01025d' 0")
		if !errors.Is(err, ErrOutputTooLarge) {
			t.Fatalf("want ErrOutputTooLarge, got %v (stdout %d bytes)", err, len(res.Stdout))
		}
	}
	res, err := r.Run(context.Background(), "sh", "-c", "printf '%01024d' 0")
	if err != nil || len(res.Stdout) != 1024 {
		t.Fatalf("output exactly at the cap must succeed: %d bytes, %v", len(res.Stdout), err)
	}
}

func TestRealRunnerStderrTruncated(t *testing.T) {
	r := testRunner(Options{MaxStderr: 10})
	res, err := r.Run(context.Background(), "sh", "-c", "echo 0123456789abcdef >&2")
	if err != nil {
		t.Fatalf("stderr overflow must not fail the call: %v", err)
	}
	if string(res.Stderr) != "0123456789" {
		t.Fatalf("stderr = %q", res.Stderr)
	}
}

func TestRealRunnerRefusesBeforeExec(t *testing.T) {
	var ran atomic.Int32
	r := NewReal(Options{Policy: Policy{AllowClusterToolsInTests: true}})
	r.exec = func(context.Context, []string) (Result, error) {
		ran.Add(1)
		return Result{ExitCode: 0}, nil
	}

	for _, argv := range [][]string{
		{"scancel", "812"},
		{"scontrol", "hold", "812"},
		{"sbatch", "--parsable"},
		{"srun", "--jobid=812", "--overlap", "nvidia-smi"},
		{"rm", "-rf", "/tmp/x"},
		{"squeue", "--me\n"},
	} {
		if _, err := r.Run(context.Background(), argv...); err == nil {
			t.Errorf("Run(%q) succeeded without authorisation", argv)
		}
	}
	if n := ran.Load(); n != 0 {
		t.Fatalf("exec reached %d times for refused commands", n)
	}

	hist := r.History()
	if len(hist) != 6 || !hist[0].Refused || hist[0].Err == "" {
		t.Fatalf("refusals not recorded: %+v", hist)
	}

	ctx := WithMutation(context.Background(), "cancel", []string{"scancel", "812"})
	if _, err := r.Run(ctx, "scancel", "812"); err != nil {
		t.Fatalf("authorised cancel refused: %v", err)
	}
	if _, err := r.Run(context.Background(), "squeue", "--me"); err != nil {
		t.Fatalf("read-only squeue refused: %v", err)
	}
	if n := ran.Load(); n != 2 {
		t.Fatalf("exec ran %d times, want 2", n)
	}
}

func TestRealRunnerTestGuard(t *testing.T) {
	var ran atomic.Int32
	r := NewReal(Options{}) // no AllowClusterToolsInTests
	r.exec = func(context.Context, []string) (Result, error) {
		ran.Add(1)
		return Result{}, nil
	}
	for _, argv := range [][]string{{"squeue", "--me"}, {"sinfo", "--version"}, {"lfs", "quota"}, {"id", "-un"}} {
		if _, err := r.Run(context.Background(), argv...); !errors.Is(err, ErrClusterToolInTests) {
			t.Errorf("Run(%q) = %v, want ErrClusterToolInTests", argv, err)
		}
	}
	if ran.Load() != 0 {
		t.Fatal("a cluster tool reached exec inside a test binary")
	}
}

func TestRealRunnerSemaphore(t *testing.T) {
	const limit = 3
	r := testRunner(Options{MaxConcurrent: limit})
	release := make(chan struct{})
	var running, peak atomic.Int32
	started := make(chan struct{}, 10)
	r.exec = func(context.Context, []string) (Result, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		running.Add(-1)
		return Result{ExitCode: 0}, nil
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Run(context.Background(), "true")
		}()
	}
	for range limit {
		<-started
	}
	select {
	case <-started:
		t.Fatal("a fourth command started while three were running")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if p := peak.Load(); p != limit {
		t.Fatalf("peak concurrency = %d, want %d", p, limit)
	}
}

func TestRealRunnerSemaphoreHonoursCancel(t *testing.T) {
	r := testRunner(Options{MaxConcurrent: 1})
	block := make(chan struct{})
	r.exec = func(context.Context, []string) (Result, error) {
		<-block
		return Result{}, nil
	}
	defer close(block)
	go func() { _, _ = r.Run(context.Background(), "true") }()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := r.Run(ctx, "true")
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "free slot") {
		t.Fatalf("want slot-wait deadline error, got %v", err)
	}
}

func TestRealRunnerHistoryRing(t *testing.T) {
	r := testRunner(Options{HistorySize: 5})
	r.exec = func(context.Context, []string) (Result, error) {
		return Result{ExitCode: 0}, nil
	}
	if len(r.History()) != 0 {
		t.Fatal("new runner has history")
	}
	for i := range 7 {
		ctx := WithLabel(context.Background(), fmt.Sprintf("call%d", i))
		_, _ = r.Run(ctx, "true", strconv.Itoa(i))
	}
	hist := r.History()
	if len(hist) != 5 {
		t.Fatalf("history len = %d, want 5", len(hist))
	}
	for i, rec := range hist {
		want := strconv.Itoa(i + 2)
		if rec.Argv[1] != want || rec.Label != "call"+want {
			t.Fatalf("history[%d] = %+v, want call %s", i, rec, want)
		}
	}
}

func TestRealRunnerDefaultHistory(t *testing.T) {
	r := testRunner(Options{})
	r.exec = func(context.Context, []string) (Result, error) { return Result{}, nil }
	for range DefaultHistorySize + 5 {
		_, _ = r.Run(context.Background(), "true")
	}
	if n := len(r.History()); n != DefaultHistorySize {
		t.Fatalf("history len = %d, want %d", n, DefaultHistorySize)
	}
}

func TestRunUserShell(t *testing.T) {
	r := NewReal(Options{BaseEnv: []string{"PATH=/usr/bin:/bin", "SQUEUE_FORMAT=bad"}, DefaultTimeout: 5 * time.Second})
	ctx := context.Background()
	res, err := RunUserShell(ctx, r, `echo "$SDASH_JOB_ID:$SQUEUE_FORMAT:$LC_ALL"`, []string{"SDASH_JOB_ID=812; rm -rf ~"})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "812; rm -rf ~::C" {
		t.Fatalf("RunUserShell = %q %v", res.Stdout, err)
	}
	for _, env := range [][]string{{"PATH=/tmp"}, {"SDASH_X"}} {
		if _, err := RunUserShell(ctx, r, "true", env); !errors.Is(err, ErrInvalidArgv) {
			t.Errorf("env %v: %v", env, err)
		}
	}
	if _, err := RunUserShell(ctx, r, "  ", nil); !errors.Is(err, ErrInvalidArgv) {
		t.Fatalf("empty script: %v", err)
	}
	if _, err := RunUserShell(ctx, r, "exit 3", nil); err == nil {
		t.Fatal("exit 3 succeeded")
	}
	tctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err := RunUserShell(tctx, r, "sleep 5", nil); !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout: %v", err)
	}
	h := r.History()
	if len(h) == 0 || h[len(h)-1].Label != "user-shell" || Key(h[len(h)-1].Argv) != "sh -c 'sleep 5'" && !strings.HasPrefix(Key(h[len(h)-1].Argv), "sh -c") {
		t.Fatalf("history = %+v", h[len(h)-1])
	}
}
