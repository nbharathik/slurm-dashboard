package storage

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
)

const mounts = `sysfs /sys sysfs rw 0 0
/dev/sda1 / ext4 rw 0 0
server:/export/home /home nfs4 rw 0 0
10.0.0.1@o2ib:/scratch /lustre/scratch lustre rw 0 0
gpfs0 /gpfs/project gpfs rw 0 0
beegfs_nodev /mnt/beegfs beegfs rw 0 0
/dev/sdb1 /data\040disk xfs rw 0 0
`

func TestParseMountsAndFind(t *testing.T) {
	ms := ParseMounts([]byte(mounts))
	if len(ms) != 7 || ms[6].Point != "/data disk" {
		t.Fatalf("mounts = %+v", ms)
	}
	for path, want := range map[string]string{
		"/home/alice": "/home", "/home": "/home", "/homework": "/", "/lustre/scratch/alice/x": "/lustre/scratch",
		"/data disk/a": "/data disk", "/tmp": "/",
	} {
		if m, ok := FindMount(ms, path); !ok || m.Point != want {
			t.Errorf("FindMount(%q) = %+v", path, m)
		}
	}
	if _, ok := FindMount(nil, "/x"); ok {
		t.Fatal("found a mount in an empty table")
	}
}

func env(vars map[string]string, dirs ...string) Env {
	return Env{
		Getenv: func(k string) string { return vars[k] },
		User:   "alice",
		Mounts: ParseMounts([]byte(mounts)),
		EvalSymlinks: func(p string) (string, error) {
			if p == "/scratch/alice" {
				return "/lustre/scratch/alice", nil
			}
			return p, nil
		},
		IsDir: func(p string) bool {
			for _, d := range dirs {
				if d == p {
					return true
				}
			}
			return false
		},
	}
}

func TestLocateDetects(t *testing.T) {
	e := env(map[string]string{"HOME": "/home/alice", "SCRATCH": "/lustre/scratch/alice", "WORK": "/gpfs/project/lab", "DATA": "relative"},
		"/home/alice", "/lustre/scratch/alice", "/gpfs/project/lab", "/scratch/alice")
	locs := Locate(nil, e)
	var got []string
	for _, l := range locs {
		got = append(got, l.Label+"="+l.Backend+"@"+l.Mount.Point)
	}
	// /scratch/alice resolves to the same Lustre mount as $SCRATCH and is
	// dropped; $DATA is relative and ignored.
	want := "Home=quota@/home Scratch=lustre@/lustre/scratch Work=gpfs@/gpfs/project"
	if strings.Join(got, " ") != want {
		t.Fatalf("Locate = %v", got)
	}
}

func TestLocateConfigured(t *testing.T) {
	entries := []config.StorageEntry{
		{Path: "/mnt/beegfs/alice"},
		{Label: "Site", Path: "/home/alice", Backend: "command", Format: "json", Command: config.CommandSpec{Argv: []string{"myquota", "--json"}}},
	}
	locs := Locate(entries, env(nil))
	if len(locs) != 2 || locs[0].Label != "alice" || locs[0].Backend != "beegfs" || locs[1].Backend != "command" {
		t.Fatalf("Locate = %+v", locs)
	}
}

func TestParsers(t *testing.T) {
	u, err := parseLustre([]byte("   /lustre/scratch  1024*  1000  2000  6d23h59m  12  100  200  -\n"))
	if err != nil || u.usedB != 1024*1024 || u.softB != 1000*1024 || u.hardF != 200 || u.grace != "6d23h59m" {
		t.Fatalf("lustre = %+v %v", u, err)
	}
	u, err = parseLustre([]byte("/lustre/a/very/long/filesystem/name\n      10      0      0      -      5      0      0      -\n"))
	if err != nil || u.usedB != 10240 || u.softB != 0 || u.grace != "" {
		t.Fatalf("wrapped lustre = %+v %v", u, err)
	}
	if _, err := parseLustre([]byte("lfs: error")); err == nil {
		t.Fatal("bad lustre accepted")
	}
	if _, err := parseLustre([]byte("/x a b c d e f g h")); err == nil {
		t.Fatal("non-numeric lustre accepted")
	}

	gpfs := []byte(`mmlsquota::HEADER:version:reserved:reserved:filesystemName:quotaType:id:name:blockUsage:blockQuota:blockLimit:blockInDoubt:blockGrace:filesUsage:filesQuota:filesLimit:filesInDoubt:filesGrace:remarks:quota:defQuota:fid:filesetname:
mmlsquota::0:1:::gpfs0:GRP:100:lab:1:2:3:0:none:1:0:0:0:none:e:on:off:::
mmlsquota::0:1:::gpfs0:USR:1001:alice:1048576:2097152:3145728:0:none:1000:0:0:0:none:e:on:off:::
mmlsquota::0:1:::gpfs1:USR:1001:alice:10:20:30:0:2 days:1:0:0:0:none:e:on:off:::
`)
	u, err = parseGPFS(gpfs, "gpfs1")
	if err != nil || u.usedB != 10*1024 || u.hardB != 30*1024 || u.grace != "2 days" {
		t.Fatalf("gpfs1 = %+v %v", u, err)
	}
	if u, err = parseGPFS(gpfs, "/dev/gpfs0"); err != nil || u.usedB != 1<<30 || u.usedF != 1000 {
		t.Fatalf("gpfs0 = %+v %v", u, err)
	}
	if _, err := parseGPFS(gpfs, "nope"); err == nil {
		t.Fatal("unmatched gpfs accepted")
	}
	if _, err := parseGPFS([]byte("garbage"), "x"); err == nil {
		t.Fatal("no header accepted")
	}

	quota := []byte(`Disk quotas for user alice (uid 1001): 
     Filesystem  blocks   quota   limit   grace   files   quota   limit   grace
      /dev/sda1  4096*  4000  5000   6days    1000       0       0        
server:/export/home  2000  3000  4000  10  0  0
`)
	u, err = parseQuota(quota, "server:/export/home")
	if err != nil || u.usedB != 2000*1024 || u.softB != 3000*1024 || u.usedF != 10 || u.grace != "" {
		t.Fatalf("quota nfs = %+v %v", u, err)
	}
	u, err = parseQuota(quota, "/dev/sda1")
	if err != nil || u.usedB != 4096*1024 || u.grace != "6days" || u.usedF != 1000 {
		t.Fatalf("quota sda1 = %+v %v", u, err)
	}
	if _, err := parseQuota([]byte("Disk quotas for user alice (uid 1001): none\n"), ""); !errors.Is(err, errNoQuota) {
		t.Fatalf("none = %v", err)
	}
	if _, err := parseQuota([]byte("x y z a b c d\n"), ""); err == nil {
		t.Fatal("bad quota accepted")
	}

	u, err = parseBeeGFS([]byte("name,id,size,hard,files,hard\nalice,1001,1073741824,10737418240,1000,unlimited\n"))
	if err != nil || u.usedB != 1<<30 || u.hardB != 10<<30 || u.hardF != 0 {
		t.Fatalf("beegfs = %+v %v", u, err)
	}
	if _, err := parseBeeGFS([]byte("name,id\n")); err == nil {
		t.Fatal("bad beegfs accepted")
	}

	u, err = parseJSON([]byte(`{"used_bytes": 5, "hard_bytes": 10, "used_files": 2, "grace": "none"}`))
	if err != nil || u.usedB != 5 || u.hardB != 10 || u.grace != "" {
		t.Fatalf("json = %+v %v", u, err)
	}
	if _, err := parseJSON([]byte("{")); err == nil {
		t.Fatal("bad json accepted")
	}
}

func checker(f *execx.FakeRunner) *Checker {
	return &Checker{
		Runner: f, User: "alice",
		StatFS: func(string) (FSStat, error) {
			return FSStat{Total: 1000, Free: 250, Avail: 200, Files: 100, FreeFiles: 60}, nil
		},
		Shell: func(context.Context, string) (execx.Result, error) {
			return execx.Result{Stdout: []byte(`{"used_bytes": 7, "hard_bytes": 70}`)}, nil
		},
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Now:      func() time.Time { return time.Unix(1000, 0) },
	}
}

func TestCheckBackends(t *testing.T) {
	f := execx.NewFake("myquota")
	f.Set([]string{"lfs", "quota", "-q", "-u", "alice", "/lustre/scratch"}, execx.FakeResponse{Stdout: []byte("/lustre/scratch 10 20 30 - 1 2 3 -\n")})
	f.Set([]string{"quota", "-w", "-u", "alice"}, execx.FakeResponse{ExitCode: 1})
	f.Set([]string{"/usr/lpp/mmfs/bin/mmlsquota", "-u", "alice", "-Y", "--block-size", "1K"}, execx.FakeResponse{ExitCode: 1, Stderr: []byte("mmlsquota: not ready\n")})
	f.Set([]string{"beegfs-ctl", "--getquota", "--uid", "alice", "--csv"}, execx.FakeResponse{ExitCode: 1, Stderr: []byte("unknown option\n")})
	f.Set([]string{"myquota", "--json"}, execx.FakeResponse{Stdout: []byte(`{"used_bytes": 1, "soft_bytes": 2}`)})
	e := env(nil)
	locs := Locate([]config.StorageEntry{
		{Label: "Scratch", Path: "/lustre/scratch/alice"},
		{Label: "Home", Path: "/home/alice"},
		{Label: "Project", Path: "/gpfs/project/lab"},
		{Label: "Bee", Path: "/mnt/beegfs/alice"},
		{Label: "Site", Path: "/home/alice", Backend: "command", Format: "json", Command: config.CommandSpec{Argv: []string{"myquota", "--json"}}},
		{Label: "Shell", Path: "/home/alice", Backend: "command", Format: "json", Command: config.CommandSpec{Shell: "myquota | jq ."}},
		{Label: "Raw", Path: "/home/alice", Backend: "command", Format: "raw", Command: config.CommandSpec{Argv: []string{"myquota", "--json"}}},
		{Label: "Tmp", Path: "/sys/fs"},
	}, e)
	c := checker(f)
	qs := c.Check(context.Background(), locs)
	by := map[string]int{}
	for i, q := range qs {
		by[q.Label] = i
	}
	sc := qs[by["Scratch"]]
	if sc.UsedBytes != 10*1024 || sc.HardBytes != 30*1024 || sc.Err != "" || sc.Backend != "lustre" || sc.At.IsZero() {
		t.Fatalf("scratch = %+v", sc)
	}
	home := qs[by["Home"]]
	if !home.IsFilesystemTotal || home.UsedBytes != 750 || home.HardBytes != 950 || home.Backend != "quota+statfs" || strings.Contains(home.Note, "failed") {
		t.Fatalf("home (no quota) = %+v", home)
	}
	if p := qs[by["Project"]]; p.Err == "" || !strings.Contains(p.Raw, "not ready") {
		t.Fatalf("gpfs failure = %+v", p)
	}
	if b := qs[by["Bee"]]; !b.IsFilesystemTotal || !strings.Contains(b.Note, "beegfs failed") {
		t.Fatalf("beegfs fallback = %+v", b)
	}
	if s := qs[by["Site"]]; s.UsedBytes != 1 || s.SoftBytes != 2 {
		t.Fatalf("command argv = %+v", s)
	}
	if s := qs[by["Shell"]]; s.UsedBytes != 7 || s.HardBytes != 70 {
		t.Fatalf("command shell = %+v", s)
	}
	if r := qs[by["Raw"]]; r.UsedBytes != 0 || !strings.Contains(r.Raw, "used_bytes") {
		t.Fatalf("command raw = %+v", r)
	}
	if tmp := qs[by["Tmp"]]; tmp.Backend != "statfs" || !tmp.IsFilesystemTotal {
		t.Fatalf("tmp = %+v", tmp)
	}

	// A failure after a success keeps the last value, marked stale.
	f.Set([]string{"lfs", "quota", "-q", "-u", "alice", "/lustre/scratch"}, execx.FakeResponse{ExitCode: 1, Stderr: []byte("lfs: timeout\n")})
	qs = c.Check(context.Background(), locs[:1])
	if qs[0].UsedBytes != 10*1024 || qs[0].Err == "" {
		t.Fatalf("stale = %+v", qs[0])
	}
}

func TestSysStatFS(t *testing.T) {
	st, err := SysStatFS(os.TempDir())
	if err != nil || st.Total == 0 || st.Free > st.Total || st.Avail > st.Free {
		t.Fatalf("statfs = %+v %v", st, err)
	}
	if _, err := SysStatFS("/no/such/dir"); err == nil {
		t.Fatal("missing path accepted")
	}
}

func TestNewRunnerAllowsSiteCommand(t *testing.T) {
	r := NewRunner([]Location{{Backend: "command", Command: config.CommandSpec{Argv: []string{"/opt/site/bin/myquota"}}}}, execx.Options{})
	if _, err := r.Run(context.Background(), "/opt/site/bin/myquota"); errors.Is(err, execx.ErrNotAllowed) {
		t.Fatalf("site command refused: %v", err)
	}
	if _, err := r.Run(context.Background(), "rm", "-rf", "/"); !errors.Is(err, execx.ErrNotAllowed) {
		t.Fatalf("rm allowed: %v", err)
	}
}

func TestDu(t *testing.T) {
	argv, err := DuArgv("/scratch/alice", true)
	if err != nil || strings.Join(argv, " ") != "nice -n 19 ionice -c3 du -x -d1 -k -- /scratch/alice" {
		t.Fatalf("argv = %v %v", argv, err)
	}
	if execx.Classify(argv) != execx.ReadOnly {
		t.Fatal("the du chain must be read-only")
	}
	if a, _ := DuArgv("/x", false); strings.Contains(strings.Join(a, " "), "ionice") {
		t.Fatal("ionice without ionice")
	}
	for _, bad := range []string{"relative", "/a/../b", ""} {
		if _, err := DuArgv(bad, true); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	u, err := ParseDu([]byte("10\t/s/a\n300\t/s/b\nbad line\n20\t/s/c\n330\t/s\n"), "/s")
	if err != nil || u.Total != 330*1024 || len(u.Entries) != 3 || u.Entries[0].Path != "/s/b" || u.Entries[2].Path != "/s/a" {
		t.Fatalf("ParseDu = %+v %v", u, err)
	}
	if _, err := ParseDu(nil, "/s"); err == nil {
		t.Fatal("empty output accepted")
	}
	if !NeedsConfirm("lustre") || NeedsConfirm("ext4") {
		t.Fatal("NeedsConfirm")
	}
	// The real du, through a real runner, on a temp directory.
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/big", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/big/f", make([]byte, 64<<10), 0o600); err != nil {
		t.Fatal(err)
	}
	argv, _ = DuArgv(dir, false)
	res, err := execx.NewReal(execx.Options{}).Run(context.Background(), argv...)
	if err != nil {
		t.Fatalf("du: %v", err)
	}
	u, err = ParseDu(res.Stdout, dir)
	if err != nil || len(u.Entries) != 1 || u.Entries[0].Bytes < 64<<10 {
		t.Fatalf("real du = %+v %v", u, err)
	}
}
