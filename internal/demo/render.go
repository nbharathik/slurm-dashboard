package demo

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// Runner returns a runner that answers from the simulation. It is a
// FakeRunner, so the execx policy (allowlist, mutation guard) applies
// exactly as with the real runner.
func (s *Sim) Runner() *execx.FakeRunner {
	f := execx.NewFake()
	f.Handler = s.Handle
	return f
}

// Handle answers one command.
func (s *Sim) Handle(ctx context.Context, argv []string) (execx.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	if len(argv) >= 2 && argv[0] == "sbatch" && argv[1] == "--test-only" {
		in, _ := execx.InputFrom(ctx)
		msg, err := s.testOnly(now, in.Stdin, argv[2:])
		res := execx.Result{Argv: argv, Stderr: []byte(msg + "\n"), Duration: 60 * time.Millisecond}
		if err != nil {
			res.ExitCode = 1
			return res, &execx.ExitError{Name: "sbatch", Code: 1, Stderr: msg}
		}
		return res, nil
	}
	out, err := s.dispatch(ctx, now, argv)
	res := execx.Result{Argv: argv, Stdout: []byte(out), Duration: 40 * time.Millisecond}
	if err != nil {
		res.ExitCode, res.Stderr = 1, []byte(err.Error()+"\n")
		return res, &execx.ExitError{Name: argv[0], Code: 1, Stderr: err.Error()}
	}
	return res, nil
}

func has(argv []string, flag string) bool { return slices.Contains(argv, flag) }

// after returns the value following flag.
func after(argv []string, flag string) string {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1]
		}
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			return v
		}
	}
	return ""
}

func (s *Sim) dispatch(ctx context.Context, now time.Time, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("empty command")
	}
	args := argv[1:]
	switch argv[0] {
	case "id":
		return s.sc.User + "\n", nil
	case "sinfo":
		switch {
		case has(args, "--version"):
			return "slurm " + s.sc.Version + "\n", nil
		case after(args, "-o") == "%N": // shell completion
			var b strings.Builder
			for _, n := range s.sc.Nodes {
				b.WriteString(n.Name + "\n")
			}
			return b.String(), nil
		case after(args, "-o") == "%P":
			var b strings.Builder
			for _, p := range s.sc.Partitions {
				b.WriteString(p.Name)
				if p.Default {
					b.WriteString("*")
				}
				b.WriteString("\n")
			}
			return b.String(), nil
		}
	case "sacct", "sshare", "sprio":
		if s.sc.NoAccounting {
			return "", fmt.Errorf("%s: error: Slurm accounting storage is disabled", argv[0])
		}
		if s.sc.Priority == "basic" && argv[0] != "sacct" {
			return "", fmt.Errorf("%s: error: This only works with the priority/multifactor plugin", argv[0])
		}
	case "sstat":
		if has(args, "--version") {
			return "slurm " + s.sc.Version + "\n", nil
		}
		if s.sc.NoUsageGather {
			return "", fmt.Errorf("sstat: error: no job accounting gather plugin is loaded")
		}
	}
	switch argv[0] {
	case "squeue":
		return s.squeue(now, args)
	case "scontrol":
		return s.scontrol(now, args)
	case "sstat":
		return s.sstat(now, after(args, "-j"))
	case "sacct":
		return s.sacct(now, args)
	case "sshare":
		f := s.sc.Fairshare
		if after(args, "-o") == "User" {
			return s.sc.User + "\n", nil
		}
		return fmt.Sprintf("%s|%s|%d|%.6f|%d|%.6f|%.6f\n", s.sc.Account, s.sc.User, f.RawShares, f.NormShares, f.RawUsage, f.EffectiveUsage, f.FairShare), nil
	case "sprio":
		return s.sprio(now, args)
	case "scancel":
		return s.scancel(ctx, now, args)
	case "srun":
		return s.srun(now, args)
	case "nice", "du":
		return s.du(argv)
	case "sbatch":
		if len(args) >= 1 && args[0] == "--parsable" {
			in, _ := execx.InputFrom(ctx)
			return s.sbatch(now, in, args[1:])
		}
	}
	return "", fmt.Errorf("%s: not simulated in demo mode", strings.Join(argv, " "))
}

// squeueDur renders a duration like squeue: M:SS, H:MM:SS, D-HH:MM:SS.
func squeueDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	t := int64(d.Seconds())
	days, h, m, sec := t/86400, t%86400/3600, t%3600/60, t%60
	switch {
	case days > 0:
		return fmt.Sprintf("%d-%02d:%02d:%02d", days, h, m, sec)
	case h > 0:
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

func ts(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Local().Format(units.TimeLayout)
}

func memStr(gb float64) string {
	if gb == math.Trunc(gb) {
		return strconv.Itoa(int(gb)) + "G"
	}
	return strconv.Itoa(int(gb*1024)) + "M"
}

func (s *Sim) gres(j *Job) string {
	switch {
	case j.GPUs == 0:
		return "N/A"
	case j.GPUType != "":
		return fmt.Sprintf("gres/gpu:%s:%d", j.GPUType, j.GPUs)
	}
	return fmt.Sprintf("gres/gpu:%d", j.GPUs)
}

// gpuType is the GPU type a job uses: its own, else its first node's.
func (s *Sim) gpuType(j *Job) string {
	if j.GPUType != "" {
		return j.GPUType
	}
	for _, n := range s.sc.Nodes {
		if len(j.Nodes) > 0 && n.Name == j.Nodes[0] && n.GPUType != "" {
			return n.GPUType
		}
	}
	return "gpu"
}

// nodeGres renders a node's Gres=, GresUsed= and the GPU parts of CfgTRES
// and AllocTRES, typed as a real cluster with AutoDetect prints them.
func nodeGres(n Node, u *nodeUse) (gres, used, cfg, alloc string) {
	type group struct {
		typ         string
		total, used int
	}
	var groups []group
	if n.GPUs > 0 {
		groups = append(groups, group{n.GPUType, n.GPUs, u.gpus})
	}
	for _, m := range n.MIG {
		groups = append(groups, group{m.Type, m.Count, u.mig[m.Type]})
	}
	if len(groups) == 0 {
		return "(null)", "gpu:0", "", ""
	}
	var g, us []string
	total, inUse := 0, 0
	idx := 0
	for i, gr := range groups {
		g = append(g, fmt.Sprintf("gpu:%s:%d(S:%d)", gr.typ, gr.total, min(i, 1)))
		ids := "N/A"
		if gr.used > 0 {
			ids = fmt.Sprintf("%d-%d", idx, idx+gr.used-1)
		}
		us = append(us, fmt.Sprintf("gpu:%s:%d(IDX:%s)", gr.typ, gr.used, ids))
		idx += gr.total
		total += gr.total
		inUse += gr.used
		cfg += fmt.Sprintf(",gres/gpu:%s=%d", gr.typ, gr.total)
		if gr.used > 0 {
			alloc += fmt.Sprintf(",gres/gpu:%s=%d", gr.typ, gr.used)
		}
	}
	cfg = fmt.Sprintf(",gres/gpu=%d", total) + cfg
	if inUse > 0 {
		alloc = fmt.Sprintf(",gres/gpu=%d", inUse) + alloc
	}
	return strings.Join(g, ","), strings.Join(us, ","), cfg, alloc
}

// hostlist compresses node names: gpu01,gpu02 -> gpu[01-02].
func hostlist(nodes []string) string {
	if len(nodes) <= 1 {
		return strings.Join(nodes, ",")
	}
	prefix := strings.TrimRight(nodes[0], "0123456789")
	width := len(nodes[0]) - len(prefix)
	var nums []int
	for _, n := range nodes {
		if !strings.HasPrefix(n, prefix) || len(n)-len(prefix) != width {
			return strings.Join(nodes, ",")
		}
		v, err := strconv.Atoi(n[len(prefix):])
		if err != nil {
			return strings.Join(nodes, ",")
		}
		nums = append(nums, v)
	}
	var parts []string
	for _, p := range strings.Split(compactRange(nums), ",") {
		lo, hi, ok := strings.Cut(p, "-")
		a, _ := strconv.Atoi(lo)
		s := fmt.Sprintf("%0*d", width, a)
		if ok {
			b, _ := strconv.Atoi(hi)
			s += fmt.Sprintf("-%0*d", width, b)
		}
		parts = append(parts, s)
	}
	return prefix + "[" + strings.Join(parts, ",") + "]"
}

func (s *Sim) squeue(now time.Time, args []string) (string, error) {
	rows, _ := s.snapshot(now)
	sortRows(rows)
	// PrivateData=jobs hides other users' jobs from everyone but admins.
	mine := has(args, "--me") || after(args, "-u") == s.sc.User || strings.Contains(s.sc.PrivateData, "jobs")
	state, only := after(args, "-t"), after(args, "-j")
	var parts []string
	if p := after(args, "-p"); p != "" {
		parts = strings.Split(p, ",")
	}
	var b strings.Builder
	for _, r := range rows {
		switch {
		case mine && r.job.User != s.sc.User:
			continue
		case state != "" && r.state != state:
			continue
		case len(parts) > 0 && !slices.Contains(parts, r.job.Partition):
			continue
		case only != "" && only != r.raw && only != strconv.Itoa(r.jobID):
			continue
		}
		switch {
		case after(args, "-o") == "%i": // shell completion
			b.WriteString(r.raw + "\n")
		case has(args, "-o"):
			b.WriteString(s.jobLine(now, r))
		case strings.HasPrefix(after(args, "-O"), "JobID:48,UserName"):
			tres := fmt.Sprintf("cpu=%d,mem=%s,node=%d,billing=%d", r.job.CPUs, memStr(r.job.MemGB), max(len(r.nodes), 1), r.job.CPUs)
			if r.job.MemGB == 0 {
				tres = fmt.Sprintf("cpu=%d,node=%d,billing=%d", r.job.CPUs, max(len(r.nodes), 1), r.job.CPUs)
			}
			if r.job.GPUs > 0 {
				tres += fmt.Sprintf(",gres/gpu=%d,gres/gpu:%s=%d", r.job.GPUs, s.gpuType(r.job), r.job.GPUs)
			}
			end := "N/A"
			if l := limitOf(r.limit); l > 0 {
				end = ts(r.start.Add(l))
			}
			fmt.Fprintf(&b, "%-48s%-48s%-64s%-8d%-1024s%-24s%-1024s\n", strconv.Itoa(r.jobID), r.job.User, r.job.Partition, max(len(r.nodes), 1), hostlist(r.nodes), end, tres)
		case has(args, "--start"):
			if r.state != "PENDING" {
				continue
			}
			est, nodes := "N/A", "(null)"
			if !r.est.IsZero() {
				est = ts(r.est)
				if len(r.job.Nodes) > 0 {
					nodes = hostlist(r.job.Nodes)
				}
			}
			fmt.Fprintf(&b, "%-48s%-24s%-256s%-64s\n", r.raw, est, nodes, r.reason)
		case strings.HasPrefix(after(args, "-O"), "JobID:48,Partition"):
			fmt.Fprintf(&b, "%-48s%-64s%-24d\n", strconv.Itoa(r.job.ID), r.job.Partition, r.priority)
		default:
			return "", fmt.Errorf("squeue: unexpected format in demo mode")
		}
	}
	return b.String(), nil
}

// jobLine renders one row in the jobs format (23 fields).
func (s *Sim) jobLine(now time.Time, r row) string {
	j := r.job
	used := time.Duration(0)
	if r.state == "RUNNING" {
		used = now.Sub(r.start)
	}
	limit := limitOf(r.limit)
	limitStr, left := "UNLIMITED", "UNLIMITED"
	if limit > 0 {
		limitStr, left = squeueDur(limit), squeueDur(limit-used)
	}
	task := "N/A"
	if r.task != "" {
		task = r.task
	}
	start, end := "N/A", "N/A"
	switch {
	case r.state == "RUNNING":
		start = ts(r.start)
		if limit > 0 {
			end = ts(r.start.Add(limit))
		}
	case !r.est.IsZero():
		start = ts(r.est)
		if limit > 0 {
			end = ts(r.est.Add(limit))
		}
	}
	dep := "(null)"
	fields := []string{
		r.raw, strconv.Itoa(j.ID), task, j.Partition, j.QOS, j.Account, r.state, squeueDur(used), limitStr, left,
		strconv.Itoa(max(len(j.Nodes), 1)), strconv.Itoa(j.CPUs), memStr(j.MemGB), s.gres(j), r.reason, hostlist(r.nodes),
		start, end, ts(r.submit), strconv.Itoa(r.priority), dep, j.User, j.Name,
	}
	return strings.Join(fields, parse.Sep) + "\n"
}

func (s *Sim) scontrol(now time.Time, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("scontrol: no command")
	}
	switch args[0] {
	case "show":
		if len(args) < 2 {
			break
		}
		switch args[1] {
		case "config":
			priv := "none"
			if s.sc.PrivateData != "" {
				priv = s.sc.PrivateData
			}
			return fmt.Sprintf("Configuration data as of %s\nAccountingStorageType   = %s\nAccountingStoreFlags    = %s\nClusterName             = %s\nJobAcctGatherType       = %s\nPriorityType            = %s\nPrivateData             = %s\nSLURM_VERSION           = %s\n",
				ts(now), s.storageType(), s.storeFlags(), s.sc.Cluster, s.gatherType(), s.priorityType(), priv, s.sc.Version), nil
		case "assoc_mgr":
			return s.assocMgr(now)
		case "node":
			return s.nodes(now), nil
		case "partition":
			return s.partitions(), nil
		case "reservation":
			return s.reservations(now), nil
		case "job":
			return s.jobDetail(now, args[len(args)-1])
		}
	case "write":
		if len(args) >= 3 && args[1] == "batch_script" {
			return s.script(now, args[2])
		}
	case "hold", "release", "requeue":
		if len(args) != 2 {
			break
		}
		return "", s.mutate(now, args[0], strings.Split(args[1], ","))
	}
	return "", fmt.Errorf("scontrol %s: not simulated in demo mode", strings.Join(args, " "))
}

// srun answers only the GPU sampling query (nvidia-smi inside a job).
func (s *Sim) srun(now time.Time, args []string) (string, error) {
	if !slices.Contains(args, "nvidia-smi") {
		return "", fmt.Errorf("srun: interactive steps are not simulated in demo mode")
	}
	id := ""
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--jobid="); ok {
			id = v
		}
	}
	rows, _ := s.snapshot(now)
	for _, r := range rows {
		if (r.raw != id && strconv.Itoa(r.jobID) != id) || r.state != "RUNNING" {
			continue
		}
		if r.job.User != s.sc.User {
			return "", fmt.Errorf("srun: error: Access/permission denied")
		}
		var b strings.Builder
		for g := range r.job.GPUs {
			util := int(math.Round(100 * math.Min(1, r.job.GPUUtil+0.04*math.Sin(float64(now.Unix()+int64(g))))))
			fmt.Fprintf(&b, "%d, NVIDIA H100 80GB HBM3, %d, %d, 81559\n", g, max(util, 0), int(20000+55000*r.job.GPUUtil)+g*311)
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("srun: error: Unable to confirm allocation for job %s: Invalid job id specified", id)
}

func (s *Sim) scancel(_ context.Context, now time.Time, ids []string) (string, error) {
	return "", s.mutate(now, "cancel", ids)
}

// mutate applies an action to jobs, with Slurm's error messages for jobs
// that do not exist or are not the user's.
func (s *Sim) mutate(now time.Time, action string, ids []string) error {
	rows, _ := s.snapshot(now)
	for _, id := range ids {
		keys, matched := s.resolve(id, rows)
		if len(matched) == 0 {
			return fmt.Errorf("error: Invalid job id specified for job %s", id)
		}
		if matched[0].job.User != s.sc.User {
			return fmt.Errorf("error: Access/permission denied for job %s", id)
		}
		for _, k := range keys {
			m := s.mod(k)
			switch action {
			case "cancel":
				m.cancelled = now
			case "hold":
				t := true
				m.held = &t
			case "release":
				f := false
				m.held = &f
			case "requeue":
				if matched[0].state != "RUNNING" {
					return fmt.Errorf("error: Requested operation is presently disabled for job %s", id)
				}
				m.requeued = now
			}
		}
	}
	return nil
}

func (s *Sim) nodes(now time.Time) string {
	rows, _ := s.snapshot(now)
	use := s.usage(rows)
	var b strings.Builder
	for _, n := range s.sc.Nodes {
		u := use[n.Name]
		if u == nil {
			u = &nodeUse{}
		}
		state := "IDLE"
		switch {
		case u.cpus >= n.CPUs || (n.GPUs > 0 && u.gpus >= n.GPUs):
			state = "ALLOCATED"
		case u.cpus > 0:
			state = "MIXED"
		}
		if n.Drain != "" {
			state += "+DRAIN"
		}
		gres, used, cfg, alloc := nodeGres(n, u)
		allocMem := u.memMB
		if s.sc.MemUntracked {
			allocMem = 0
		}
		switch {
		case u.cpus > 0 && allocMem > 0:
			alloc = fmt.Sprintf("cpu=%d,mem=%dM", u.cpus, allocMem) + alloc
		case u.cpus > 0:
			alloc = fmt.Sprintf("cpu=%d", u.cpus) + alloc
		}
		cfg = fmt.Sprintf("cpu=%d,mem=%dG,billing=%d", n.CPUs, n.MemGB, n.CPUs) + cfg
		factor := n.LoadFactor
		if factor == 0 {
			factor = 0.83
		}
		load := float64(u.cpus) * factor
		free := int64(n.MemGB)*1024 - u.memMB*7/10
		reason := ""
		if n.Drain != "" {
			reason = fmt.Sprintf(" Reason=%s [root@%s]", n.Drain, ts(s.t0.Add(-26*time.Hour)))
		}
		feat := strings.Join(n.Features, ",")
		fmt.Fprintf(&b, "NodeName=%s Arch=x86_64 CoresPerSocket=%d CPUAlloc=%d CPUEfctv=%d CPUTot=%d CPULoad=%.2f AvailableFeatures=%s ActiveFeatures=%s Gres=%s NodeAddr=%s NodeHostName=%s RealMemory=%d AllocMem=%d FreeMem=%d Sockets=2 Boards=1 State=%s ThreadsPerCore=1 TmpDisk=0 Weight=1 Owner=N/A MCS_label=N/A Partitions=%s BootTime=%s SlurmdStartTime=%s CfgTRES=%s AllocTRES=%s CapWatts=n/a CurrentWatts=0 AveWatts=0 GresUsed=%s%s\n",
			n.Name, n.CPUs/2, u.cpus, n.CPUs, n.CPUs, load, feat, feat, gres, n.Name, n.Name, n.MemGB*1024, allocMem, free, state,
			strings.Join(n.Partitions, ","), ts(s.t0.Add(-240*time.Hour)), ts(s.t0.Add(-240*time.Hour)), cfg, alloc, used, reason)
	}
	return b.String()
}

func (s *Sim) partitions() string {
	var b strings.Builder
	for _, p := range s.sc.Partitions {
		def := "NO"
		if p.Default {
			def = "YES"
		}
		cpus, gpus, count := 0, 0, 0
		nodes, _ := units.ExpandHostlist(p.Nodes)
		for _, n := range s.sc.Nodes {
			if slices.Contains(nodes, n.Name) {
				cpus, gpus, count = cpus+n.CPUs, gpus+n.GPUs, count+1
			}
		}
		tres := fmt.Sprintf("cpu=%d,node=%d,billing=%d", cpus, count, cpus)
		if gpus > 0 {
			tres += fmt.Sprintf(",gres/gpu=%d", gpus)
		}
		fmt.Fprintf(&b, "PartitionName=%s AllowGroups=ALL AllowAccounts=ALL AllowQos=ALL AllocNodes=ALL Default=%s QoS=N/A DefaultTime=NONE MaxNodes=UNLIMITED MaxTime=%s MinNodes=0 Nodes=%s State=UP TotalCPUs=%d TotalNodes=%d TRES=%s\n",
			p.Name, def, p.MaxTime, p.Nodes, cpus, count, tres)
	}
	return b.String()
}

func (s *Sim) reservations(now time.Time) string {
	_, start := s.loopPos(now)
	var b strings.Builder
	for _, r := range s.sc.Reservations {
		st := start.Add(r.StartIn.D())
		nodes, _ := units.ExpandHostlist(r.Nodes)
		state := "INACTIVE"
		if !st.After(now) {
			state = "ACTIVE"
		}
		fmt.Fprintf(&b, "ReservationName=%s StartTime=%s EndTime=%s Duration=%s Nodes=%s NodeCnt=%d CoreCnt=%d Features=(null) PartitionName=(null) Flags=%s TRES=cpu=%d Users=root Groups=(null) Accounts=(null) Licenses=(null) State=%s BurstBuffer=(null)\n",
			r.Name, ts(st), ts(st.Add(r.Duration.D())), squeueDur(r.Duration.D()), r.Nodes, len(nodes), len(nodes)*64, strings.Join(r.Flags, ","), len(nodes)*64, state)
	}
	return b.String()
}

// logPaths returns the StdOut and StdErr of a job as scontrol prints them.
func logPaths(j *Job, jobID int, task string) (string, string) {
	base := j.Name + "-" + strconv.Itoa(jobID)
	if task != "" && !strings.ContainsAny(task, "-,") {
		base = j.Name + "-" + strconv.Itoa(j.ID) + "_" + task
	}
	dir := j.WorkDir
	return dir + "/" + base + ".out", dir + "/" + base + ".err"
}

func (s *Sim) jobDetail(now time.Time, ref string) (string, error) {
	rows, ended := s.snapshot(now)
	var r *row
	for i := range rows {
		if rows[i].raw == ref || strconv.Itoa(rows[i].jobID) == ref {
			r = &rows[i]
			break
		}
	}
	if r == nil {
		for i := range rows {
			if strconv.Itoa(rows[i].job.ID) == strings.SplitN(ref, "_", 2)[0] {
				r = &rows[i]
				break
			}
		}
	}
	if r == nil {
		for _, d := range ended {
			if d.raw == ref && now.Sub(d.end) < 5*time.Minute {
				return s.detailLine(now, row{job: d.job, raw: d.raw, jobID: d.jobID, state: d.state, reason: "None", start: d.start, submit: d.submit, nodes: d.job.Nodes, limit: d.limit}, d.end), nil
			}
		}
		return "", fmt.Errorf("slurm_load_jobs error: Invalid job id specified")
	}
	return s.detailLine(now, *r, time.Time{}), nil
}

func (s *Sim) detailLine(now time.Time, r row, end time.Time) string {
	j := r.job
	limit := limitOf(r.limit)
	run := time.Duration(0)
	start, endStr := "Unknown", "Unknown"
	if !r.start.IsZero() {
		run = now.Sub(r.start)
		start = ts(r.start)
		if limit > 0 {
			endStr = ts(r.start.Add(limit))
		}
	} else if !r.est.IsZero() {
		start = ts(r.est)
	}
	if !end.IsZero() {
		run, endStr = end.Sub(r.start), ts(end)
	}
	limitStr := "UNLIMITED"
	if limit > 0 {
		limitStr = squeueDur(limit)
	}
	arr := ""
	if j.Array != "" {
		arr = fmt.Sprintf(" ArrayJobId=%d ArrayTaskId=%s", j.ID, r.task)
	}
	tres := fmt.Sprintf("cpu=%d,mem=%s,node=%d,billing=%d", j.CPUs, memStr(j.MemGB), max(len(j.Nodes), 1), j.CPUs)
	if j.GPUs > 0 {
		tres += fmt.Sprintf(",gres/gpu=%d", j.GPUs)
	}
	alloc := "(null)"
	nodes := hostlist(r.nodes)
	batch := ""
	if r.state == "RUNNING" {
		alloc = tres
		batch = " BatchHost=" + r.nodes[0]
	}
	out, errp := logPaths(j, r.jobID, r.task)
	perNode := ""
	if j.GPUs > 0 {
		perNode = fmt.Sprintf(" TresPerNode=gres/gpu:%d", j.GPUs)
	}
	return fmt.Sprintf("JobId=%d%s JobName=%s UserId=%s(%d) GroupId=%s(%d) MCS_label=N/A Priority=%d Nice=0 Account=%s QOS=%s JobState=%s Reason=%s Dependency=(null) Requeue=1 Restarts=0 BatchFlag=1 Reboot=0 ExitCode=0:0 RunTime=%s TimeLimit=%s TimeMin=N/A SubmitTime=%s EligibleTime=%s AccrueTime=%s StartTime=%s EndTime=%s Deadline=N/A SuspendTime=None SecsPreSuspend=0 LastSchedEval=%s Scheduler=Main Partition=%s AllocNode:Sid=login1:4242 ReqNodeList=(null) ExcNodeList=(null) NodeList=%s%s NumNodes=%d NumCPUs=%d NumTasks=1 CPUs/Task=%d ReqB:S:C:T=0:0:*:* ReqTRES=%s AllocTRES=%s Socks/Node=* NtasksPerN:B:S:C=0:0:*:* CoreSpec=* MinCPUsNode=%d MinMemoryNode=%s MinTmpDiskNode=0 Features=(null) DelayBoot=00:00:00 OverSubscribe=OK Contiguous=0 Licenses=(null) Network=(null) Command=%s/run.sh WorkDir=%s StdErr=%s StdIn=/dev/null StdOut=%s Power=%s\n",
		r.jobID, arr, j.Name, j.User, s.uidOf(j.User), j.User, s.uidOf(j.User), r.priority, j.Account, j.QOS, r.state, r.reason,
		squeueDur(run), limitStr, ts(r.submit), ts(r.submit), ts(r.submit), start, endStr, ts(now.Add(-20*time.Second)), j.Partition,
		nodes, batch, max(len(j.Nodes), 1), j.CPUs, j.CPUs, tres, alloc, j.CPUs, memStr(j.MemGB), j.WorkDir, j.WorkDir, errp, out, perNode)
}

func (s *Sim) uidOf(user string) int {
	if user == s.sc.User {
		return s.sc.UID
	}
	n := 2000
	for _, c := range user {
		n += int(c)
	}
	return n
}

func (s *Sim) script(now time.Time, ref string) (string, error) {
	rows, ended := s.snapshot(now)
	var j *Job
	for _, r := range rows {
		if r.raw == ref || strconv.Itoa(r.job.ID) == strings.SplitN(ref, "_", 2)[0] {
			j = r.job
		}
	}
	for _, d := range ended {
		if d.raw == ref {
			j = d.job
		}
	}
	if j == nil {
		return "", fmt.Errorf("error: job %s not found", ref)
	}
	if j.User != s.sc.User {
		return "", fmt.Errorf("error: Access/permission denied")
	}
	return batchScript(j), nil
}

func batchScript(j *Job) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	fmt.Fprintf(&b, "#SBATCH --job-name=%s\n#SBATCH --partition=%s\n#SBATCH --time=%s\n", j.Name, j.Partition, j.Limit)
	fmt.Fprintf(&b, "#SBATCH --cpus-per-task=%d\n#SBATCH --mem=%s\n", j.CPUs, memStr(j.MemGB))
	if j.GPUs > 0 {
		fmt.Fprintf(&b, "#SBATCH --gres=gpu:%d\n", j.GPUs)
	}
	if j.Array != "" {
		fmt.Fprintf(&b, "#SBATCH --array=%s\n", j.Array)
		b.WriteString("#SBATCH --output=%x-%A_%a.out\n#SBATCH --error=%x-%A_%a.err\n")
	} else {
		b.WriteString("#SBATCH --output=%x-%j.out\n#SBATCH --error=%x-%j.err\n")
	}
	b.WriteString("\nset -euo pipefail\nmodule load cuda/12.4 python/3.12\nsource ~/venvs/ml/bin/activate\n\n")
	fmt.Fprintf(&b, "srun python %s.py --config configs/%s.yaml\n", strings.ReplaceAll(j.Name, "-", "_"), j.Name)
	return b.String()
}

// sstat answers "sstat -j ID[,ID...]": one .batch row per running job that
// has steps; jobs without steps are left out, like the real command.
func (s *Sim) sstat(now time.Time, refs string) (string, error) {
	rows, _ := s.snapshot(now)
	var out strings.Builder
	for _, ref := range strings.Split(refs, ",") {
		for _, r := range rows {
			if (r.raw != ref && strconv.Itoa(r.jobID) != ref) || r.state != "RUNNING" {
				continue
			}
			j := r.job
			el := now.Sub(r.start)
			rss := int64(j.MemGB * 1024 * 1024 * j.MemFrac) // KiB
			// Memory ramps up over the first ten minutes.
			if ramp := el.Minutes() / 10; ramp < 1 {
				rss = int64(float64(rss) * max(ramp, 0.1))
			}
			cpu := time.Duration(float64(el) * j.CPUEff * float64(j.CPUs))
			tres := fmt.Sprintf("cpu=%s,energy=0,fs/disk=%d,mem=%dK,pages=0,vmem=%dK", squeueDur(cpu), rss*3, rss, rss*2)
			if j.GPUs > 0 && j.GPUUtil > 0 {
				tres += fmt.Sprintf(",gres/gpumem=%dM,gres/gpuutil=%d", int(60000*j.GPUUtil), int(j.GPUUtil*100))
			}
			fmt.Fprintf(&out, "%s.batch|1|%dK|%dK|%s|%s\n", r.raw, rss, rss*9/10, squeueDur(cpu), tres)
			break
		}
	}
	if out.Len() == 0 {
		return "", fmt.Errorf("sstat: error: no steps running for job %s", refs)
	}
	return out.String(), nil
}

func (s *Sim) sprio(now time.Time, args []string) (string, error) {
	if after(args, "-o") == "%i" {
		return "", nil
	}
	rows, _ := s.snapshot(now)
	var b strings.Builder
	for _, r := range rows {
		if r.job.User != s.sc.User || r.state != "PENDING" || r.reason == "JobHeldUser" {
			continue
		}
		p := r.priority
		age := min(p/10, 1000)
		fair := int(float64(p) * 0.6)
		size := p / 20
		part := 1000
		qos := p - age - fair - size - part
		fields := []string{strconv.Itoa(r.job.ID), strconv.Itoa(p), strconv.Itoa(age), strconv.Itoa(fair), strconv.Itoa(size), strconv.Itoa(part), strconv.Itoa(max(qos, 0))}
		b.WriteString(strings.Join(fields, parse.Sep) + "\n")
	}
	return b.String(), nil
}

// Quotas returns the scenario's storage usage (demo mode has no real
// filesystems to query).
func (s *Sim) Quotas() []model.Quota {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	const gb = int64(1) << 30
	var out []model.Quota
	for _, q := range s.sc.Storage {
		raw := fmt.Sprintf("Disk quotas for usr %s (demo):\n  Filesystem  used   quota  limit   files   quota    limit\n  %s  %dG  %dG  %dG  %d  %d  %d\n",
			s.sc.User, q.Path, q.UsedGB, q.SoftGB, q.HardGB, q.Files, q.FilesSoft, q.FilesHard)
		if q.SharedFS {
			raw = fmt.Sprintf("statfs %s (demo): %dG used of %dG, %d of %d files\n", q.Path, q.UsedGB, q.HardGB, q.Files, q.FilesHard)
		}
		out = append(out, model.Quota{
			Label: q.Label, Path: q.Path, FSType: q.FS, Backend: q.Backend,
			UsedBytes: q.UsedGB * gb, SoftBytes: q.SoftGB * gb, HardBytes: q.HardGB * gb,
			UsedFiles: q.Files, SoftFiles: q.FilesSoft, HardFiles: q.FilesHard,
			IsFilesystemTotal: q.SharedFS, Note: q.Note, At: now, Raw: raw,
		})
	}
	return out
}

// TrendSamples is a made-up 30 days of readings for every storage location
// that has a growth rate, ending at the scenario's usage, four a day.
func (s *Sim) TrendSamples(now time.Time) []insights.Sample {
	const gb = float64(int64(1) << 30)
	var out []insights.Sample
	for _, q := range s.sc.Storage {
		if q.UsedGB <= 0 {
			continue
		}
		limit := q.SoftGB
		if limit <= 0 {
			limit = q.HardGB
		}
		for i := 30 * 4; i >= 1; i-- {
			ago := float64(i) / 4 // days
			used := float64(q.UsedGB) - q.GrowthGB*ago
			used *= 1 + 0.004*math.Sin(float64(i)*1.7) // a little noise, the same every run
			if used <= 0 {
				continue
			}
			out = append(out, insights.Sample{
				T: now.Add(-time.Duration(ago * 24 * float64(time.Hour))), Key: q.Path,
				Used: int64(used * gb), Limit: limit * int64(gb),
			})
		}
	}
	return out
}

// The site settings a scenario reports in "scontrol show config".

func (s *Sim) storageType() string {
	if s.sc.NoAccounting {
		return "accounting_storage/none"
	}
	return "accounting_storage/slurmdbd"
}

func (s *Sim) storeFlags() string {
	if s.sc.NoAccounting || s.sc.NoJobScripts {
		return "(null)"
	}
	return "job_script"
}

func (s *Sim) gatherType() string {
	if s.sc.NoUsageGather {
		return "jobacct_gather/none"
	}
	return "jobacct_gather/linux"
}

func (s *Sim) priorityType() string {
	if s.sc.Priority == "basic" {
		return "priority/basic"
	}
	return "priority/multifactor"
}
