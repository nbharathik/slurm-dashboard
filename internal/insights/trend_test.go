package insights

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

var trendEpoch = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

const gb = int64(1) << 30

func quota(path string, usedGB, softGB int64) model.Quota {
	return model.Quota{Label: "Home", Path: path, UsedBytes: usedGB * gb, SoftBytes: softGB * gb}
}

// grow returns daily samples of a location filling at perDay GB per day.
func grow(key string, days int, startGB, perDay, limitGB int64) []Sample {
	var out []Sample
	for d := days; d >= 0; d-- {
		out = append(out, Sample{
			T: trendEpoch.Add(-time.Duration(d) * 24 * time.Hour), Key: key,
			Used: (startGB + perDay*int64(days-d)) * gb, Limit: limitGB * gb,
		})
	}
	return out
}

func TestAddSamplesOncePerHour(t *testing.T) {
	l := New(nil, trendEpoch)
	q := []model.Quota{quota("/home/u", 10, 50), {Label: "Empty", Path: "/e"}, {Label: "Stale", Path: "/s", UsedBytes: 5, Err: "timeout"}}
	if got := l.Add(q, trendEpoch); len(got) != 1 || got[0].Key != "/home/u" || got[0].Limit != 50*gb {
		t.Fatalf("first add = %+v (locations without a reading are skipped)", got)
	}
	if got := l.Add(q, trendEpoch.Add(59*time.Minute)); len(got) != 0 {
		t.Errorf("within the hour: %+v", got)
	}
	if got := l.Add(q, trendEpoch.Add(time.Hour)); len(got) != 1 {
		t.Errorf("after the hour: %+v", got)
	}
	if n := len(l.Series("/home/u")); n != 2 {
		t.Errorf("series has %d samples", n)
	}
	var nilLog *Log
	if nilLog.Series("x") != nil || nilLog.Add(q, trendEpoch) != nil {
		t.Error("a nil Log must read as empty")
	}
}

func TestLimitFallsBackToHard(t *testing.T) {
	q := model.Quota{Path: "/p", UsedBytes: gb, HardBytes: 9 * gb}
	if s, ok := SampleOf(q, trendEpoch); !ok || s.Limit != 9*gb {
		t.Errorf("sample = %+v %v", s, ok)
	}
}

func TestNewDropsOldSamples(t *testing.T) {
	old := Sample{T: trendEpoch.Add(-Keep - time.Hour), Key: "/a", Used: 1}
	fresh := Sample{T: trendEpoch.Add(-time.Hour), Key: "/a", Used: 2}
	if got := New([]Sample{fresh, old}, trendEpoch).Series("/a"); len(got) != 1 || got[0].Used != 2 {
		t.Errorf("series = %+v", got)
	}
}

func TestPredict(t *testing.T) {
	cases := []struct {
		name    string
		series  []Sample
		ok      bool
		filling bool
		flat    bool
		days    float64
	}{
		{"growing 2 GB a day to a 100 GB limit", grow("/a", 20, 10, 2, 100), true, true, false, 25},
		{"flat", grow("/a", 20, 40, 0, 100), true, false, true, 0},
		{"shrinking", grow("/a", 20, 60, -2, 100), true, false, false, 0},
		{"growing without a limit", grow("/a", 20, 10, 2, 0), true, false, false, 0},
		{"too few samples", grow("/a", 2, 10, 2, 100)[:3], false, false, false, 0},
		{"too short a span", []Sample{
			{T: trendEpoch.Add(-3 * time.Hour), Key: "/a", Used: gb},
			{T: trendEpoch.Add(-2 * time.Hour), Key: "/a", Used: gb},
			{T: trendEpoch.Add(-time.Hour), Key: "/a", Used: gb},
			{T: trendEpoch, Key: "/a", Used: gb},
		}, false, false, false, 0},
	}
	for _, c := range cases {
		f := Predict(c.series, trendEpoch)
		if f.OK != c.ok || f.Filling != c.filling || f.Flat != c.flat || math.Abs(f.Days-c.days) > 0.5 {
			t.Errorf("%s: %+v, want ok=%v filling=%v flat=%v days=%v", c.name, f, c.ok, c.filling, c.flat, c.days)
		}
	}
	if f := Predict(grow("/a", 20, 60, -2, 100), trendEpoch); f.PerDay >= 0 {
		t.Errorf("shrinking slope = %v", f.PerDay)
	}
	// Samples older than the window do not count.
	old := grow("/a", 60, 0, 1, 100)
	if f := Predict(old[:20], trendEpoch); f.OK {
		t.Errorf("only old samples: %+v", f)
	}
}

func TestPoints(t *testing.T) {
	series := []Sample{
		{T: trendEpoch.Add(-20 * 24 * time.Hour), Used: 10}, {T: trendEpoch.Add(-10 * 24 * time.Hour), Used: 20}, {T: trendEpoch.Add(-time.Hour), Used: 30},
	}
	got := Points(series, trendEpoch, 3)
	if !math.IsNaN(got[0]) && got[0] != 10 {
		t.Errorf("first slot = %v", got[0])
	}
	if got[1] != 20 || got[2] != 30 {
		t.Errorf("points = %v (a slot holds the last reading up to its end)", got)
	}
	for _, v := range Points(nil, trendEpoch, 4) {
		if !math.IsNaN(v) {
			t.Error("no samples must give only NaN")
		}
	}
	if len(Points(series, trendEpoch, 0)) != 0 {
		t.Error("n=0")
	}
}

func TestFileRoundTripAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, FileName)
	l := Open(path, trendEpoch)
	added := l.Add([]model.Quota{quota("/home/u", 10, 50)}, trendEpoch)
	if err := l.Persist(added); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v", fi.Mode(), err)
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v", di.Mode())
	}
	back := Open(path, trendEpoch.Add(time.Hour))
	if s := back.Series("/home/u"); len(s) != 1 || s[0].Used != 10*gb || !s[0].T.Equal(trendEpoch) {
		t.Errorf("reloaded = %+v", s)
	}
}

func TestOpenDropsBadAndOldLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	old := trendEpoch.Add(-Keep - 24*time.Hour).Format(time.RFC3339)
	body := strings.Join([]string{
		`{"t":"` + trendEpoch.Add(-time.Hour).Format(time.RFC3339) + `","loc":"/a","used":5,"limit":9}`,
		`not json`,
		`{"t":"` + old + `","loc":"/a","used":1,"limit":9}`,
		`{"t":"` + trendEpoch.Format(time.RFC3339) + `","loc":"","used":1}`,
		`{"t":"` + trendEpoch.Format(time.RFC3339) + `","loc":"/b","used":-4}`,
		``,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	l := Open(path, trendEpoch)
	if s := l.Series("/a"); len(s) != 1 || s[0].Used != 5 {
		t.Errorf("series = %+v", s)
	}
	if l.Series("/b") != nil {
		t.Error("a negative reading must be dropped")
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "not json") || strings.Contains(string(raw), old) {
		t.Errorf("file was not cleaned:\n%s", raw)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("rewritten mode = %v", fi.Mode())
	}
}

func TestMissingAndMemoryOnlyLogs(t *testing.T) {
	if l := Open(filepath.Join(t.TempDir(), "none", FileName), trendEpoch); l.Series("x") != nil {
		t.Error("a missing file is an empty log")
	}
	mem := Open("", trendEpoch)
	added := mem.Add([]model.Quota{quota("/a", 1, 5)}, trendEpoch)
	if err := mem.Persist(added); err != nil || len(mem.Series("/a")) != 1 {
		t.Errorf("memory log: %v, %v", err, mem.Series("/a"))
	}
}
