package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/logs"
	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// historyJobHandler answers "sacct -j ID" from the recorded history.
func historyJobHandler(t *testing.T, f *execx.FakeRunner) {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/fixtures/23.11/history.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Handler = func(_ context.Context, argv []string) (execx.Result, error) {
		if argv[0] == "sacct" && len(argv) > 5 && argv[4] == "-j" {
			var b strings.Builder
			for _, line := range strings.Split(string(raw), "\n") {
				id := strings.SplitN(line, parse.Sep, 2)[0]
				if id == argv[5] || strings.HasPrefix(id, argv[5]+".") {
					b.WriteString(line + "\n")
				}
			}
			return execx.Result{Stdout: []byte(b.String())}, nil
		}
		if argv[0] == "scontrol" && len(argv) == 5 && argv[2] == "job" {
			return execx.Result{ExitCode: 1, Stderr: []byte("slurm_load_jobs error: Invalid job id specified\n")},
				&execx.ExitError{Name: "scontrol", Code: 1, Stderr: "slurm_load_jobs error: Invalid job id specified"}
		}
		return execx.Result{}, execx.ErrNoFixture
	}
}

func TestWhyAgainstFixtures(t *testing.T) {
	h, a, _ := fixtureApp(t)
	if code := runApp(h, a, "why", "19"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	out := h.stdout.String()
	for _, want := range []string{"19 job10", "PartitionTimeLimit", "never start", "Priority 2111 ="} {
		if !strings.Contains(out, want) {
			t.Errorf("why output lacks %q:\n%s", want, out)
		}
	}
	if code := runApp(h, a, "why", "19", "--json"); code != ExitOK {
		t.Fatalf("json exit %d", code)
	}
	var doc report.WhyDoc
	if err := json.Unmarshal(h.stdout.Bytes(), &doc); err != nil || !doc.NeverStarts || doc.Code != "PartitionTimeLimit" {
		t.Fatalf("why json = %+v %v", doc, err)
	}
	if code := runApp(h, a, "why", "14"); code != ExitOK || !strings.Contains(h.stdout.String(), "not pending") {
		t.Fatalf("running job: %d %q", code, h.stdout.String())
	}
	if code := runApp(h, a, "why", "999"); code != ExitError || !strings.Contains(h.stderr.String(), "not one of your queued jobs") {
		t.Fatalf("unknown job: %d %q", code, h.stderr.String())
	}
	if code := runApp(h, a, "why"); code != ExitUsage {
		t.Fatalf("no args: %d", code)
	}
}

func TestEffAgainstFixtures(t *testing.T) {
	h, a, f := fixtureApp(t)
	historyJobHandler(t, f)
	if code := runApp(h, a, "eff"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	if out := h.stdout.String(); !strings.Contains(out, "CPU%") || !strings.Contains(out, "job15") {
		t.Fatalf("eff table:\n%s", out)
	}
	if code := runApp(h, a, "eff", "3"); code != ExitOK {
		t.Fatalf("eff 3: %d %s", code, h.stderr.String())
	}
	if out := h.stdout.String(); !strings.Contains(out, "State:         COMPLETED") || !strings.Contains(out, "Cores:         4") {
		t.Fatalf("eff 3:\n%s", out)
	}
	if code := runApp(h, a, "eff", "4", "--json"); code != ExitOK {
		t.Fatalf("eff 4 json: %d %s", code, h.stderr.String())
	}
	var doc report.EffDoc
	if err := json.Unmarshal(h.stdout.Bytes(), &doc); err != nil || len(doc.Jobs) != 1 || !strings.Contains(doc.Jobs[0].Hint, "Exited with code 3") {
		t.Fatalf("eff json = %+v %v", doc, err)
	}
	if code := runApp(h, a, "eff", "--days", "31"); code != ExitUsage {
		t.Fatalf("--days 31: %d", code)
	}
	if code := runApp(h, a, "eff", "999"); code != ExitError || !strings.Contains(h.stderr.String(), "not in the accounting") {
		t.Fatalf("unknown: %d %s", code, h.stderr.String())
	}
}

func TestLogsCommand(t *testing.T) {
	h, a, f := fixtureApp(t)
	historyJobHandler(t, f)
	dir := t.TempDir()
	path := filepath.Join(dir, "job2-14.out")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Point the recorded detail of job 14 at the temp file.
	raw, _ := os.ReadFile("../../testdata/fixtures/23.11/jobdetail-running.txt")
	detail := strings.Replace(string(raw), "StdOut=/home/user1/job2-14.out", "StdOut="+path, 1)
	f.Set([]string{"scontrol", "show", "job", "-o", "14"}, execx.FakeResponse{Stdout: []byte(detail)})
	if code := runApp(h, a, "logs", "14"); code != ExitOK || h.stdout.String() != "hello\nworld\n" {
		t.Fatalf("logs: %d %q %s", code, h.stdout.String(), h.stderr.String())
	}
	rawLog := "hello\x1b]52;c;ZXZpbA==\x07\n\xff"
	if err := os.WriteFile(path, []byte(rawLog), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runApp(h, a, "logs", "14"); code != ExitOK || h.stdout.String() != rawLog {
		t.Fatalf("redirected logs must retain raw bytes: %d %q", code, h.stdout.String())
	}
	if code := runApp(h, a, "logs", "14", "--err"); code != ExitError || !strings.Contains(h.stderr.String(), "does not exist") {
		t.Fatalf("missing stderr: %d %s", code, h.stderr.String())
	}
	// A finished job falls back to slurm-ID.out in its work directory.
	if code := runApp(h, a, "logs", "3"); code != ExitError || !strings.Contains(h.stderr.String(), "guessing") || !strings.Contains(h.stderr.String(), "/tmp/slurm-3.out") {
		t.Fatalf("fallback: %d %s", code, h.stderr.String())
	}
}

func TestLogPrinterTerminalAndRedirected(t *testing.T) {
	parts := []string{"hello\x1b]52;", "c;ZXZpbA==\x1b", "\\日", "本語\n\xe6"}
	for _, terminal := range []bool{false, true} {
		var out bytes.Buffer
		printer := logPrinter{out: &out, terminal: terminal}
		for i, part := range parts {
			if err := printer.write(logs.Chunk{Data: []byte(part), Reset: i == 0}); err != nil {
				t.Fatal(err)
			}
		}
		if err := printer.flush(); err != nil {
			t.Fatal(err)
		}
		want := strings.Join(parts, "")
		if terminal {
			want = "hello日本語\n\ufffd"
		}
		if out.String() != want {
			t.Fatalf("terminal=%v: got %q, want %q", terminal, out.String(), want)
		}
	}
}

func TestLogPrinterResetsAfterRotation(t *testing.T) {
	var out bytes.Buffer
	printer := logPrinter{out: &out, terminal: true}
	for _, c := range []logs.Chunk{
		{Data: []byte("old\x1b]unfinished")},
		{Data: []byte("new\n"), Reset: true},
	} {
		if err := printer.write(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := printer.flush(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "oldnew\n" {
		t.Fatalf("new file swallowed by old control sequence: %q", out.String())
	}
}
