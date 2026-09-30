package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/meta"
)

type harness struct {
	home   string
	stdout bytes.Buffer
	stderr bytes.Buffer
	vars   map[string]string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	home := t.TempDir()
	return &harness{home: home, vars: map[string]string{"HOME": home, "EDITOR": "true"}}
}

func (h *harness) env() Env {
	return Env{
		Stdin:  strings.NewReader(""),
		Stdout: &h.stdout,
		Stderr: &h.stderr,
		Getenv: func(k string) string { return h.vars[k] },
	}
}

func (h *harness) run(args ...string) int {
	h.stdout.Reset()
	h.stderr.Reset()
	return Main(context.Background(), args, h.env())
}

func TestVersion(t *testing.T) {
	oldV, oldC, oldD := meta.Version, meta.Commit, meta.Date
	t.Cleanup(func() { meta.Version, meta.Commit, meta.Date = oldV, oldC, oldD })
	meta.Version, meta.Commit, meta.Date = "v0.0.0-test", "abc1234", "2026-09-28T00:00:00Z"

	h := newHarness(t)
	if code := h.run("version"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	out := h.stdout.String()
	for _, want := range []string{"sdash v0.0.0-test", "commit:   abc1234", "built:    2026-09-28T00:00:00Z", "go:       go"} {
		if !strings.Contains(out, want) {
			t.Errorf("version output missing %q:\n%s", want, out)
		}
	}

	if code := h.run("version", "--json"); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Schema  int    `json:"schema"`
		Name    string `json:"name"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal(h.stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", h.stdout.String(), err)
	}
	if got.Schema != 1 || got.Name != "sdash" || got.Version != "v0.0.0-test" || got.Commit != "abc1234" {
		t.Fatalf("json = %+v", got)
	}
}

func TestConfigInitValidatePath(t *testing.T) {
	h := newHarness(t)
	want := filepath.Join(h.home, ".config", "sdash", "config.toml")

	if code := h.run("config", "path"); code != ExitOK || strings.TrimSpace(h.stdout.String()) != want {
		t.Fatalf("config path = %q (exit %d)", h.stdout.String(), code)
	}

	if code := h.run("config", "validate"); code != ExitOK || !strings.Contains(h.stdout.String(), "No config file") {
		t.Fatalf("validate without file: exit %d, %q", code, h.stdout.String())
	}

	if code := h.run("config", "init"); code != ExitOK {
		t.Fatalf("init exit %d: %s", code, h.stderr.String())
	}
	fi, err := os.Stat(want)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("config file: %v %v", fi, err)
	}

	if code := h.run("config", "validate"); code != ExitOK || !strings.HasSuffix(strings.TrimSpace(h.stdout.String()), ": OK") {
		t.Fatalf("validate after init: exit %d, %q", code, h.stdout.String())
	}

	if code := h.run("config", "init"); code != ExitError || !strings.Contains(h.stderr.String(), "--force") {
		t.Fatalf("second init: exit %d, %q", code, h.stderr.String())
	}
	if code := h.run("config", "init", "--force"); code != ExitOK || !strings.Contains(h.stdout.String(), ".bak") {
		t.Fatalf("init --force: exit %d, %q", code, h.stdout.String())
	}
}

func TestConfigValidateReportsLines(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.home, "custom.toml")
	src := "start_tab = \"jobs\"\ntheme = \"neon\"\ncolour = true\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	code := h.run("--config", path, "config", "validate")
	out := h.stdout.String()
	if code != ExitError {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	for _, want := range []string{path + ":2:", "error: theme", path + ":3:", "warning: colour", "1 error, 1 warning"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	if err := os.WriteFile(path, []byte("[ui]\ncolour = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("--config", path, "config", "validate"); code != ExitOK || !strings.Contains(h.stdout.String(), "OK with 1 warning") {
		t.Fatalf("warnings only: exit %d, %q", code, h.stdout.String())
	}
}

func TestConfigEditCreatesAndValidates(t *testing.T) {
	h := newHarness(t)
	if code := h.run("config", "edit"); code != ExitOK {
		t.Fatalf("edit exit %d: %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), ": OK") {
		t.Fatalf("edit did not validate: %q", h.stdout.String())
	}

	h.vars["EDITOR"] = "sdash-no-such-editor --wait"
	if code := h.run("config", "edit"); code != ExitError || !strings.Contains(h.stderr.String(), "sdash-no-such-editor") {
		t.Fatalf("missing editor: exit %d, %q", code, h.stderr.String())
	}

	h.vars["EDITOR"] = "srun --pty"
	if code := h.run("config", "edit"); code != ExitError || !strings.Contains(h.stderr.String(), "not allowed") {
		t.Fatalf("Slurm as editor: exit %d, %q", code, h.stderr.String())
	}
}

func TestUsageErrors(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"frobnicate"},
		{"--no-such-flag"},
		{"version", "extra"},
		{"config", "init", "--bogus"},
	} {
		if code := h.run(args...); code != ExitUsage {
			t.Errorf("%q: exit %d, want %d (stderr %q)", args, code, ExitUsage, h.stderr.String())
		}
		if !strings.Contains(h.stderr.String(), "--help") {
			t.Errorf("%q: no usage hint in %q", args, h.stderr.String())
		}
	}
}

func TestRootNeedsTerminal(t *testing.T) {
	h := newHarness(t)
	if code := h.run(); code != ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.stderr.String(), "needs an interactive terminal") {
		t.Fatalf("stderr = %q", h.stderr.String())
	}
	for _, args := range [][]string{{"--refresh-scale", "0.5"}, {"--theme", "pink"}, {"--tab", "nope"}} {
		h := newHarness(t)
		if code := h.run(args...); code != ExitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

func TestDebugLogWritten(t *testing.T) {
	h := newHarness(t)
	if code := h.run("--debug", "version"); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	b, err := os.ReadFile(filepath.Join(h.home, ".cache", "sdash", "debug.log"))
	if err != nil || !strings.Contains(string(b), "command=\"sdash version\"") {
		t.Fatalf("debug log = %q, %v", b, err)
	}
}

func TestWorksWithoutHome(t *testing.T) {
	h := newHarness(t)
	delete(h.vars, "HOME")
	if code := h.run("version"); code != ExitOK {
		t.Fatalf("version without $HOME: exit %d, %s", code, h.stderr.String())
	}
	if code := h.run("config", "path"); code != ExitError || !strings.Contains(h.stderr.String(), "$HOME") {
		t.Fatalf("config path without $HOME: exit %d, %q", code, h.stderr.String())
	}
	explicit := filepath.Join(t.TempDir(), "c.toml")
	if code := h.run("--config", explicit, "config", "init"); code != ExitOK {
		t.Fatalf("--config without $HOME: exit %d, %s", code, h.stderr.String())
	}
}

func TestPanicWritesCrashReport(t *testing.T) {
	h := newHarness(t)
	a := &app{env: h.env()}
	root := newRoot(a)
	root.AddCommand(&cobra.Command{
		Use: "boom",
		RunE: func(*cobra.Command, []string) error {
			panic("kaboom")
		},
	})
	code := run(context.Background(), root, a, []string{"boom"})
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	errOut := h.stderr.String()
	if !strings.Contains(errOut, "crashed: kaboom") || !strings.Contains(errOut, "crash report was saved") {
		t.Fatalf("stderr = %q", errOut)
	}
	matches, _ := filepath.Glob(filepath.Join(h.home, ".cache", "sdash", "crash-*.log"))
	if len(matches) != 1 {
		t.Fatalf("crash files = %v", matches)
	}
}

func TestProfileFlag(t *testing.T) {
	h, a, _ := fixtureApp(t)
	if code := runApp(h, a, "--profile", "nope", "status"); code != ExitUsage || !strings.Contains(h.stderr.String(), "no [profiles.NAME]") {
		t.Fatalf("unknown profile: %d %s", code, h.stderr.String())
	}
	cfgPath := filepath.Join(h.home, "cfg.toml")
	if err := os.WriteFile(cfgPath, []byte("[profiles.big]\nslurm_conf = \"/etc/big/slurm.conf\"\ncluster_name = \"bigiron\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runApp(h, a, "--config", cfgPath, "--profile", "other", "status"); code != ExitUsage || !strings.Contains(h.stderr.String(), "have: big") {
		t.Fatalf("wrong profile: %d %s", code, h.stderr.String())
	}
	if code := runApp(h, a, "--config", cfgPath, "--profile", "big", "status", "--json"); code != ExitOK || !strings.Contains(h.stdout.String(), `"cluster": "bigiron"`) {
		t.Fatalf("profile status: %d %s %s", code, h.stdout.String(), h.stderr.String())
	}
}

// TestDemoFlag: --demo works on every command and picks a scenario by name.
func TestDemoFlag(t *testing.T) {
	h := newHarness(t)
	if code := h.run("gpus", "--demo=hetero"); code != ExitOK {
		t.Fatalf("gpus: %d %s", code, h.stderr.String())
	}
	out := h.stdout.String()
	for _, want := range []string{"H200 NVL", "RTX PRO 6000 BW", "1g.33gb 2/4"} {
		if !strings.Contains(out, want) {
			t.Errorf("gpus --demo=hetero lacks %q:\n%s", want, out)
		}
	}
	if code := h.run("--demo", "status"); code != ExitOK || !strings.Contains(h.stdout.String(), "tundra") {
		t.Fatalf("--demo status: %d %q %q", code, h.stdout.String(), h.stderr.String())
	}
	if code := h.run("quota", "--demo=hetero", "--json"); code != ExitOK || !strings.Contains(h.stdout.String(), `"is_filesystem_total": true`) {
		t.Fatalf("quota: %d %q", code, h.stdout.String())
	}
	if code := h.run("status", "--demo=nope"); code != ExitUsage || !strings.Contains(h.stderr.String(), "default, hetero") {
		t.Fatalf("unknown scenario: %d %q", code, h.stderr.String())
	}
}

// TestSiteConfig: /etc/sdash/config.toml gives site defaults under the
// user's file, which $SDASH_CONFIG can point elsewhere.
func TestSiteConfig(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	site := filepath.Join(dir, "site.toml")
	user := filepath.Join(dir, "mine.toml")
	if err := os.WriteFile(site, []byte("cluster_name = \"site-name\"\ntheme = \"light\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user, []byte("theme = \"bogus\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.vars["SDASH_CONFIG"] = user
	a := &app{env: h.env(), siteConfig: site}
	if code := run(context.Background(), newRoot(a), a, []string{"config", "path", "--all"}); code != ExitOK ||
		!strings.Contains(h.stdout.String(), site+"  (read)") || !strings.Contains(h.stdout.String(), user+"  (read)") {
		t.Fatalf("config path --all: %d %q", code, h.stdout.String())
	}
	h.stdout.Reset()
	if code := run(context.Background(), newRoot(a), a, []string{"config", "validate"}); code != ExitError ||
		!strings.Contains(h.stdout.String(), user+":1:") || !strings.Contains(h.stdout.String(), site+": OK") {
		t.Fatalf("config validate: %d %q", code, h.stdout.String())
	}
	if cfg := a.loadConfig(); cfg.ClusterName != "site-name" || cfg.Theme != "light" {
		t.Fatalf("merged config: %q %q", cfg.ClusterName, cfg.Theme)
	}
}
