package slurm

import (
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestLogPath(t *testing.T) {
	d := &model.JobDetail{
		Job:     model.Job{ID: model.JobID{Raw: "13_1"}, Name: "sweep", User: "alice", NodeList: []string{"n1"}},
		StdOut:  "logs/%x-%A_%a.out",
		WorkDir: "/home/alice/w",
		Raw:     map[string]string{"JobId": "14", "ArrayJobId": "13", "ArrayTaskId": "1"},
	}
	if got := LogPath(d, false); got != "/home/alice/w/logs/sweep-13_1.out" {
		t.Fatal(got)
	}
	if got := LogPath(d, true); got != "/home/alice/w/logs/sweep-13_1.out" {
		t.Fatalf("stderr defaults to stdout: %s", got)
	}
	d.StdErr = "/tmp/%j.err"
	if got := LogPath(d, true); got != "/tmp/14.err" {
		t.Fatal(got)
	}
	if LogPath(&model.JobDetail{}, false) != "" {
		t.Fatal("empty")
	}
}
