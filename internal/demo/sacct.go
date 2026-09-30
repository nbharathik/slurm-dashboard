package demo

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// acct is one job as sacct reports it.
type acct struct {
	id          string
	name, part  string
	state, exit string
	submit      time.Time
	start, end  time.Time // zero = Unknown
	elapsed     time.Duration
	limit       time.Duration
	cpus        int
	memGB       float64
	gpus        int
	gpuType     string
	peakFrac    float64
	cpuEff      float64
	gpuUtil     float64
	nodes       []string
	workDir     string
	cancelledBy int
	job         *Job
}

// accounting returns the user's jobs known to accounting at now.
func (s *Sim) accounting(now time.Time) []acct {
	rows, ended := s.snapshot(now)
	_, start := s.loopPos(now)
	var out []acct
	for i := range s.sc.History {
		p := &s.sc.History[i]
		end := start.Add(-p.EndAgo.D())
		st := end.Add(-p.Elapsed.D())
		exit := p.Exit
		if exit == "" {
			exit = "0:0"
		}
		out = append(out, acct{
			id: strconv.Itoa(p.ID), name: p.Name, part: p.Partition, state: p.State, exit: exit,
			submit: st.Add(-7 * time.Minute), start: st, end: end, elapsed: p.Elapsed.D(), limit: limitOf(p.Limit),
			cpus: p.CPUs, memGB: p.MemGB, gpus: p.GPUs, gpuType: p.GPUType, peakFrac: p.PeakFrac, cpuEff: p.CPUEff, gpuUtil: p.GPUUtil,
			nodes: p.Nodes, workDir: p.WorkDir, cancelledBy: p.CancelledBy,
		})
	}
	for _, d := range ended {
		if d.job.User != s.sc.User {
			continue
		}
		peak := d.job.PeakFrac
		if peak == 0 {
			peak = d.job.MemFrac
		}
		out = append(out, acct{
			id: d.raw, name: d.job.Name, part: d.job.Partition, state: d.state, exit: d.exit, submit: d.submit,
			start: d.start, end: d.end, elapsed: d.end.Sub(d.start), limit: limitOf(d.limit), cpus: d.job.CPUs,
			memGB: d.job.MemGB, gpus: d.job.GPUs, gpuType: d.job.GPUType, peakFrac: peak, cpuEff: d.job.CPUEff, gpuUtil: d.job.GPUUtil,
			nodes: d.job.Nodes, workDir: d.job.WorkDir, cancelledBy: d.cancelledBy, job: d.job,
		})
	}
	for _, r := range rows {
		if r.job.User != s.sc.User {
			continue
		}
		a := acct{
			id: r.raw, name: r.job.Name, part: r.job.Partition, state: r.state, exit: "0:0", submit: r.submit,
			start: r.start, limit: limitOf(r.limit), cpus: r.job.CPUs, memGB: r.job.MemGB, gpus: r.job.GPUs, gpuType: r.job.GPUType,
			nodes: r.nodes, workDir: r.job.WorkDir, job: r.job,
		}
		if r.state == "RUNNING" {
			a.elapsed = now.Sub(r.start)
		}
		out = append(out, a)
	}
	slices.SortStableFunc(out, func(a, b acct) int { return a.submit.Compare(b.submit) })
	return out
}

func (s *Sim) sacct(now time.Time, args []string) (string, error) {
	if has(args, "--batch-script") {
		if s.sc.NoJobScripts {
			return "", fmt.Errorf("sacct: error: job scripts are not stored (AccountingStoreFlags lacks job_script)")
		}
		// The demo site stores job scripts in accounting
		// (AccountingStoreFlags=job_script).
		id := after(args, "-j")
		for _, p := range s.sc.History {
			if strconv.Itoa(p.ID) == id {
				j := &Job{ID: p.ID, Name: p.Name, Partition: p.Partition, Limit: p.Limit, CPUs: p.CPUs, MemGB: p.MemGB, GPUs: p.GPUs}
				return fmt.Sprintf("Batch Script for %d\n%s\n%s", p.ID, strings.Repeat("-", 80), batchScript(j)), nil
			}
		}
		return "", fmt.Errorf("sacct: error: no batch script stored for job %s", id)
	}
	fields := after(args, "-o")
	all := s.accounting(now)
	switch {
	case fields == "JobID": // capability probe
		return "", nil
	case fields == "SubmitLine,WorkDir":
		// Slurm stores the command line without quotes.
		id := after(args, "-j")
		for _, a := range all {
			if a.id == id {
				return fmt.Sprintf("sbatch --account=%s %s.sh\x1f%s\n", s.sc.Account, a.name, a.workDir), nil
			}
		}
		return "", nil
	case fields == "JobID,State,ExitCode,ElapsedRaw":
		ids := strings.Split(after(args, "-j"), ",")
		var b strings.Builder
		for _, a := range all {
			if slices.Contains(ids, a.id) {
				fmt.Fprintf(&b, "%s|%s|%s|%d\n", a.id, stateText(a), a.exit, int(a.elapsed.Seconds()))
			}
		}
		return b.String(), nil
	case strings.HasPrefix(fields, "JobID,JobIDRaw"):
		var b strings.Builder
		id := after(args, "-j")
		from := time.Time{}
		if v := after(args, "-S"); strings.HasPrefix(v, "now-") {
			days, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(v, "now-"), "days"))
			from = now.Add(-time.Duration(days) * 24 * time.Hour)
		}
		for _, a := range all {
			switch {
			case id != "" && a.id != id:
				continue
			case id == "" && !a.end.IsZero() && a.end.Before(from):
				continue
			}
			s.writeAcct(&b, a)
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("sacct %s: not simulated in demo mode", strings.Join(args, " "))
}

func stateText(a acct) string {
	if a.state == "CANCELLED" && a.cancelledBy > 0 {
		return fmt.Sprintf("CANCELLED by %d", a.cancelledBy)
	}
	return a.state
}

// cpuTime renders sacct's TotalCPU: MM:SS.mmm, HH:MM:SS or D-HH:MM:SS.
func cpuTime(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%02d:%02d.%03d", int(d.Minutes()), int(d.Seconds())%60, d.Milliseconds()%1000)
	}
	return squeueDurPadded(d)
}

func squeueDurPadded(d time.Duration) string {
	t := int64(d.Seconds())
	days, h, m, sec := t/86400, t%86400/3600, t%3600/60, t%60
	if days > 0 {
		return fmt.Sprintf("%d-%02d:%02d:%02d", days, h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, sec)
}

func unknown(t time.Time) string {
	if t.IsZero() {
		return "Unknown"
	}
	return ts(t)
}

func (s *Sim) writeAcct(b *strings.Builder, a acct) {
	nodes := hostlist(a.nodes)
	nn := strconv.Itoa(max(len(a.nodes), 1))
	if a.state == "PENDING" {
		nodes = "None assigned"
	}
	limit := "UNLIMITED"
	if a.limit > 0 {
		limit = strconv.Itoa(int(a.limit.Minutes()))
	}
	alloc := ""
	if a.state != "PENDING" {
		alloc = fmt.Sprintf("billing=%d,cpu=%d,mem=%s,node=%s", a.cpus, a.cpus, memStr(a.memGB), nn)
		if a.gpus > 0 {
			gres := fmt.Sprintf("gres/gpu=%d", a.gpus)
			if a.gpuType != "" {
				gres += fmt.Sprintf(",gres/gpu:%s=%d", a.gpuType, a.gpus)
			}
			alloc = fmt.Sprintf("billing=%d,cpu=%d,%s,mem=%s,node=%s", a.cpus, a.cpus, gres, memStr(a.memGB), nn)
		}
	}
	total := time.Duration(float64(a.elapsed) * a.cpuEff * float64(a.cpus))
	line := []string{
		a.id, a.id, a.name, a.part, stateText(a), a.exit, ts(a.submit), unknown(a.start), unknown(a.end),
		strconv.Itoa(int(a.elapsed.Seconds())), limit, strconv.Itoa(a.cpus), cpuTime(total), memStr(a.memGB), "", "", alloc, nn, nodes, a.workDir,
	}
	b.WriteString(strings.Join(line, parse.Sep) + "\n")
	if a.start.IsZero() || a.state == "RUNNING" {
		return
	}
	rss := int64(a.memGB * 1024 * 1024 * a.peakFrac)
	usage := fmt.Sprintf("cpu=%s,energy=0,fs/disk=%d,mem=%dK,pages=0,vmem=%dK", squeueDurPadded(total), rss*2, rss, rss*2)
	if a.gpus > 0 && a.gpuUtil > 0 {
		usage += fmt.Sprintf(",gres/gpumem=%dM,gres/gpuutil=%d", int(70000*a.gpuUtil), int(a.gpuUtil*100))
	}
	stepAlloc := strings.TrimPrefix(alloc, fmt.Sprintf("billing=%d,", a.cpus))
	step := []string{
		a.id + ".batch", a.id + ".batch", "batch", "", a.state, a.exit, ts(a.start), ts(a.start), ts(a.end),
		strconv.Itoa(int(a.elapsed.Seconds())), "", strconv.Itoa(a.cpus), cpuTime(total), "", strconv.FormatInt(rss, 10) + "K", usage, stepAlloc, "1", firstOr(a.nodes), "",
	}
	if strings.HasPrefix(step[4], "CANCELLED") {
		step[4] = "CANCELLED"
	}
	b.WriteString(strings.Join(step, parse.Sep) + "\n")
}

func firstOr(nodes []string) string {
	if len(nodes) == 0 {
		return ""
	}
	return nodes[0]
}
