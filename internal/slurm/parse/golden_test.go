package parse

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestMain(m *testing.M) {
	// Fixtures carry local timestamps; pin the zone so goldens are stable.
	time.Local = time.UTC
	os.Exit(m.Run())
}

// parsers maps a fixture's source name (the part of the file name before
// the first '-' or '.') to its parser.
var parsers = map[string]func([]byte) (any, []model.ParseWarning){
	"myjobs":       wrap(MyJobs),
	"alljobs":      wrap(MyJobs),
	"cluster":      wrap(Running),
	"queuerank":    wrap(QueueRank),
	"nodes":        wrap(Nodes),
	"partitions":   wrap(Partitions),
	"reservations": wrap(Reservations),
	"jobdetail":    wrap(JobDetails),
	"history":      wrap(History),
	"fairshare":    wrap(Shares),
	"sprio":        wrap(Priorities),
	"final":        wrap(FinalStates),
	"jobstat": func(b []byte) (any, []model.ParseWarning) {
		return Sstat(b)
	},
	"jobstats": func(b []byte) (any, []model.ParseWarning) {
		return SstatJobs(b)
	},
	"version": func(b []byte) (any, []model.ParseWarning) {
		c, err := Version(b)
		return errOr(c, err)
	},
	"clustername": func(b []byte) (any, []model.ParseWarning) {
		c, err := ClusterName(b)
		return errOr(c, err)
	},
	"clusterconfig": func(b []byte) (any, []model.ParseWarning) {
		c, err := ClusterConfig(b)
		return errOr(c, err)
	},
	"start": wrap(StartEstimates),
	"assocmgr": func(b []byte) (any, []model.ParseWarning) {
		return AssocMgr(b, "root")
	},
	"submitline": func(b []byte) (any, []model.ParseWarning) {
		line, dir := SubmitRecord(b)
		parsed, err := SubmitLine(line, "")
		return errOr(struct {
			Line, WorkDir string
			Parsed        SbatchLine
		}{line, dir, parsed}, err)
	},
}

func wrap[T any](fn func([]byte) ([]T, []model.ParseWarning)) func([]byte) (any, []model.ParseWarning) {
	return func(b []byte) (any, []model.ParseWarning) {
		items, warns := fn(b)
		return items, warns
	}
}

func errOr(v any, err error) (any, []model.ParseWarning) {
	if err != nil {
		return nil, []model.ParseWarning{{Msg: err.Error()}}
	}
	return v, nil
}

func sourceOf(file string) string {
	base := strings.TrimSuffix(filepath.Base(file), ".txt")
	if i := strings.IndexAny(base, "-."); i > 0 {
		return base[:i]
	}
	return base
}

func TestGoldenFixtures(t *testing.T) {
	files, err := filepath.Glob("../../../testdata/fixtures/*/*.txt")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		src := sourceOf(file)
		parse, ok := parsers[src]
		if !ok {
			continue
		}
		seen[src] = true
		t.Run(strings.TrimPrefix(file, "../../../testdata/fixtures/"), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			items, warns := parse(raw)
			for i := range warns {
				// Stack traces would make goldens unstable; a parser bug
				// must never happen anyway.
				if strings.Contains(warns[i].Msg, "parser bug") {
					t.Fatalf("parser panicked on %s: %s", file, warns[i].Msg)
				}
			}
			got, err := json.MarshalIndent(map[string]any{"items": items, "warnings": warns}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			golden := strings.TrimSuffix(file, ".txt") + ".golden.json"
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden file (run go test ./internal/slurm/parse -update): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from %s; review and run with -update if the change is intended\n%s",
					file, golden, firstDiff(string(want), string(got)))
			}
		})
	}
	for src := range parsers {
		if !seen[src] && src != "alljobs" && src != "queuerank" {
			t.Errorf("no fixture exercises the %q parser", src)
		}
	}
}

func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return "line " + itoa(i+1) + ":\n- " + a + "\n+ " + b
		}
	}
	return ""
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
