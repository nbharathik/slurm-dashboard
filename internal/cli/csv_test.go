package cli

import (
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

func TestCSVCell(t *testing.T) {
	for in, want := range map[string]string{
		"train": "train", "=1+1": "'=1+1", "+x": "'+x", "@SUM(A1)": "'@SUM(A1)", "-5": "-5", "-2.5": "-2.5", "-x": "'-x", "": "",
	} {
		if got := textsafe.CSVCell(in); got != want {
			t.Errorf("CSVCell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteCSV(t *testing.T) {
	start := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	limit := int64(3600)
	jobs := []report.Job{
		{ID: "812", Name: "=HYPERLINK(\"x\")", State: "RUNNING", Partition: "gpu", NodeList: []string{"gpu01", "gpu02"}, Start: &start, TimeLimitS: &limit, GPUs: 2},
		{ID: "815_[3-9]", Task: "[3-9]", Name: "sweep, v2", State: "PENDING", NodeList: []string{}},
	}
	var b strings.Builder
	if err := writeCSV(&b, jobs); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(strings.NewReader(b.String())).ReadAll()
	if err != nil {
		t.Fatalf("not valid CSV: %v\n%s", err, b.String())
	}
	if len(recs) != 3 {
		t.Fatalf("%d records", len(recs))
	}
	col := map[string]int{}
	for i, h := range recs[0] {
		col[h] = i
	}
	for _, want := range []string{"id", "name", "state", "node_list", "start_time", "time_limit_s", "gpus", "array_task"} {
		if _, ok := col[want]; !ok {
			t.Fatalf("no column %q in %v", want, recs[0])
		}
	}
	row := recs[1]
	if row[col["name"]] != "'=HYPERLINK(\"x\")" {
		t.Errorf("formula not neutralised: %q", row[col["name"]])
	}
	if row[col["node_list"]] != "gpu01 gpu02" || row[col["start_time"]] != "2026-09-28T12:00:00Z" || row[col["time_limit_s"]] != "3600" {
		t.Errorf("row = %v", row)
	}
	if recs[2][col["name"]] != "sweep, v2" || recs[2][col["start_time"]] != "" || recs[2][col["time_limit_s"]] != "" {
		t.Errorf("row 2 = %v", recs[2])
	}
}
