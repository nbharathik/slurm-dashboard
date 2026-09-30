package parse

import (
	"reflect"
	"testing"
)

func TestSplitWords(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{`a b  c`, []string{"a", "b", "c"}},
		{`-J "my job" -o 'a b.out'`, []string{"-J", "my job", "-o", "a b.out"}},
		{`x\ y "a\"b" 'c\d'`, []string{"x y", `a"b`, `c\d`}},
		{`--wrap="echo $HOME"`, []string{"--wrap=echo $HOME"}},
		{`""`, []string{""}},
	} {
		got, err := SplitWords(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitWords(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{`"open`, `'open`} {
		if _, err := SplitWords(bad); err == nil {
			t.Errorf("SplitWords(%q) accepted", bad)
		}
	}
}

func TestSubmitLine(t *testing.T) {
	names := func(l SbatchLine) []string {
		var out []string
		for _, o := range l.Opts {
			out = append(out, o.String())
		}
		return out
	}
	for _, c := range []struct {
		line, job string
		opts      []string
		unread    []string
		script    string
	}{
		{"sbatch -p gpu --mem 32G -c8 -t 1:00:00 run.sh a b", "", []string{"--partition=gpu", "--mem=32G", "--cpus-per-task=8", "--time=1:00:00"}, nil, "run.sh"},
		{"/usr/bin/sbatch -H --gres=gpu:h200:1 run.sh", "", []string{"--hold", "--gres=gpu:h200:1"}, nil, "run.sh"},
		// A free-text value followed by the last word: that word is the script.
		{"sbatch -p cpu -o out.txt run.sh", "", []string{"--partition=cpu", "--output=out.txt"}, nil, "run.sh"},
		// A free-text value followed by another option.
		{"sbatch -J train -p cpu run.sh x", "", []string{"--job-name=train", "--partition=cpu"}, nil, "run.sh"},
		// Unquoted spaces: nothing after the ambiguous value is read.
		{"sbatch -p cpu --comment x y -t 5 run.sh", "", []string{"--partition=cpu"}, []string{"--comment", "x", "y", "-t", "5", "run.sh"}, ""},
		// A job name that matches the recorded one is trusted.
		{"sbatch -J train -p cpu run.sh x", "train", []string{"--job-name=train", "--partition=cpu"}, nil, "run.sh"},
		// A name with a space cannot be read back; nothing is guessed.
		{"sbatch -J my job -p cpu run.sh", "my job", nil, []string{"-J", "my", "job", "-p", "cpu", "run.sh"}, ""},
		// --wrap ends the line.
		{"sbatch -p short --wrap echo hi there", "", []string{"--partition=short"}, nil, ""},
	} {
		got, err := SubmitLine(c.line, c.job)
		if err != nil {
			t.Fatalf("%q: %v", c.line, err)
		}
		if !reflect.DeepEqual(names(got), c.opts) || !reflect.DeepEqual(got.Unread, c.unread) || got.Script != c.script {
			t.Errorf("SubmitLine(%q) = %q unread %q script %q", c.line, names(got), got.Unread, got.Script)
		}
	}
	if _, err := SubmitLine("srun hostname", ""); err != ErrNotSbatch {
		t.Errorf("srun line: %v", err)
	}
}

func TestScriptDirectives(t *testing.T) {
	script := "#!/bin/bash\n#SBATCH -J a\n#SBATCH --mem=4G   # memory\n\n# plain comment\n#SBATCHX ignored\n#SBATCH -p gpu -c 2\necho hi\n#SBATCH -t 5\n"
	got := ScriptDirectives(script)
	var s []string
	for _, o := range got {
		s = append(s, o.String())
	}
	want := []string{"--job-name=a", "--mem=4G", "--partition=gpu", "--cpus-per-task=2"}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("ScriptDirectives = %q, want %q", s, want)
	}
	if v, ok := Last(append(got, SbatchOpt{Name: "mem", Value: "8G"}), "mem"); !ok || v != "8G" {
		t.Errorf("Last = %q %v", v, ok)
	}
}
