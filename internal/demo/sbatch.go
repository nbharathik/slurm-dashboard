package demo

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// directives reads the #SBATCH options of a script.
func directives(script string) map[string]string {
	out := map[string]string{}
	short := map[string]string{"J": "job-name", "p": "partition", "t": "time", "c": "cpus-per-task", "a": "array", "N": "nodes"}
	for _, line := range strings.Split(script, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "#SBATCH")
		if !ok {
			continue
		}
		opt := strings.TrimSpace(rest)
		switch {
		case strings.HasPrefix(opt, "--"):
			k, v, _ := strings.Cut(strings.TrimPrefix(opt, "--"), "=")
			out[k] = strings.TrimSpace(v)
		case strings.HasPrefix(opt, "-") && len(opt) > 1:
			if long, ok := short[opt[1:2]]; ok {
				out[long] = strings.TrimSpace(opt[2:])
			}
		}
	}
	return out
}

// jobFromScript builds a job from a submitted script and the --name=value
// options on the command line, which win over the script.
func (s *Sim) jobFromScript(script []byte, dir string, opts []string) (Job, error) {
	d := directives(string(script))
	for _, o := range opts {
		if k, v, ok := strings.Cut(strings.TrimPrefix(o, "--"), "="); ok {
			d[k] = v
		}
	}
	j := Job{
		User: s.sc.User, Name: d["job-name"], Partition: d["partition"], Limit: d["time"], Account: s.sc.Account,
		QOS: "normal", CPUs: 1, MemGB: 4, Reason: "Priority", Priority: 5000, WorkDir: dir,
	}
	if j.Name == "" {
		j.Name = "sbatch"
	}
	if j.Partition == "" {
		for _, p := range s.sc.Partitions {
			if p.Default {
				j.Partition = p.Name
			}
		}
	}
	if j.Limit == "" {
		j.Limit = "1:00:00"
	}
	if limitOf(j.Limit) == 0 {
		return j, fmt.Errorf("sbatch: error: Invalid time limit specification")
	}
	if c, err := strconv.Atoi(d["cpus-per-task"]); err == nil && c > 0 {
		j.CPUs = c
	}
	if g, ok := strings.CutPrefix(d["gres"], "gpu:"); ok {
		parts := strings.Split(g, ":")
		if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			j.GPUs, j.Log = n, "train"
		}
		if len(parts) == 2 {
			j.GPUType = parts[0]
		}
	}
	var part *Partition
	for i := range s.sc.Partitions {
		if s.sc.Partitions[i].Name == j.Partition {
			part = &s.sc.Partitions[i]
		}
	}
	if part == nil {
		return j, fmt.Errorf("sbatch: error: invalid partition specified: %s", j.Partition)
	}
	if maxTime := limitOf(part.MaxTime); maxTime > 0 && limitOf(j.Limit) > maxTime {
		j.Reason = "PartitionTimeLimit"
	}
	nodes, _ := units.ExpandHostlist(part.Nodes)
	for _, name := range nodes {
		if j.GPUs == 0 || s.hasGPUs(name, j.GPUType, j.GPUs) {
			j.Nodes = []string{name}
			break
		}
	}
	if len(j.Nodes) == 0 && j.GPUs > 0 {
		return j, fmt.Errorf("sbatch: error: Batch job submission failed: Requested node configuration is not available")
	}
	return j, nil
}

// hasGPUs reports whether a node has at least n GPUs of type (any type
// when empty; MIG profiles count as types).
func (s *Sim) hasGPUs(node, typ string, n int) bool {
	for _, nd := range s.sc.Nodes {
		if nd.Name != node {
			continue
		}
		if nd.GPUs >= n && (typ == "" || typ == nd.GPUType) {
			return true
		}
		for _, m := range nd.MIG {
			if m.Count >= n && (typ == "" || typ == m.Type) {
				return true
			}
		}
	}
	return false
}

// testOnly answers "sbatch --test-only" the way Slurm does, on stderr.
func (s *Sim) testOnly(now time.Time, script []byte, opts []string) (string, error) {
	j, err := s.jobFromScript(script, "/", opts)
	if err != nil {
		return err.Error(), err
	}
	start := now.Add(submitDelay)
	if j.GPUs > 0 {
		start = now.Add(38 * time.Minute)
	}
	return fmt.Sprintf("sbatch: Job %d to start at %s using %d processors on nodes %s in partition %s",
		s.nextID, start.Local().Format(units.TimeLayout), j.CPUs, strings.Join(j.Nodes, ","), j.Partition), nil
}

// sbatch submits a script: the job pends for 20 s, then runs.
func (s *Sim) sbatch(now time.Time, in execx.RunOpts, opts []string) (string, error) {
	if len(in.Stdin) == 0 {
		return "", fmt.Errorf("sbatch: error: Batch script is empty")
	}
	j, err := s.jobFromScript(in.Stdin, in.Dir, opts)
	if err != nil {
		return "", err
	}
	j.ID = s.nextID
	s.nextID++
	j.SubmittedAt = now
	s.submitted = append(s.submitted, j)
	return fmt.Sprintf("%d\n", j.ID), nil
}

// du answers the disk-usage analyser for the scenario's storage paths.
func (s *Sim) du(argv []string) (string, error) {
	path := argv[len(argv)-1]
	for _, q := range s.sc.Storage {
		if q.Path != path {
			continue
		}
		names := []string{"runs", "data", "checkpoints", "envs", "code", ".cache", "logs"}
		shares := []float64{0.34, 0.27, 0.18, 0.09, 0.05, 0.04, 0.03}
		total := q.UsedGB << 20 // KB
		var b strings.Builder
		for i, n := range names {
			fmt.Fprintf(&b, "%d\t%s/%s\n", int64(float64(total)*shares[i]), path, n)
		}
		fmt.Fprintf(&b, "%d\t%s\n", total, path)
		return b.String(), nil
	}
	return "", fmt.Errorf("du: cannot access '%s': No such file or directory", path)
}
