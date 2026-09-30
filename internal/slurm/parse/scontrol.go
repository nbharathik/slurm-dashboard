package parse

import (
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// Nodes parses "scontrol show node -o".
func Nodes(raw []byte) (nodes []model.Node, warns []model.ParseWarning) {
	w := &warnings{source: "nodes"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		kv, _ := KV(line)
		name := kv["NodeName"]
		if name == "" {
			if isNoRecords(line) {
				return
			}
			w.add(n, line, "no NodeName")
			return
		}
		var fe fieldErr
		base, flags := units.ParseNodeState(kv["State"])
		node := model.Node{
			Name:       name,
			State:      base,
			Flags:      flags,
			Reason:     nullToEmpty(kv["Reason"]),
			Partitions: splitList(kv["Partitions"]),
			CPUTotal:   fe.atoi(kv["CPUTot"]),
			CPUAlloc:   fe.atoi(kv["CPUAlloc"]),
			MemTotalMB: fe.atoi64(kv["RealMemory"]),
			MemAllocMB: fe.atoi64(kv["AllocMem"]),
			MemFreeMB:  -1,
			Features:   splitList(kv["AvailableFeatures"]),
		}
		if load := kv["CPULoad"]; load != "" && load != "N/A" {
			node.CPULoad = fe.atof(load)
		}
		if free := kv["FreeMem"]; free != "" && free != "N/A" {
			node.MemFreeMB = fe.atoi64(free)
		}
		setGPUs(&node, kv["Gres"], kv["GresUsed"], kv["CfgTRES"], kv["AllocTRES"])
		if boot := kv["BootTime"]; boot != "" {
			node.BootTime, _ = units.ParseTimestamp(boot, nil) // unknown boot times stay zero
		}
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		nodes = append(nodes, node)
	})
	return nodes, w.list
}

// Partitions parses "scontrol show partition -o".
func Partitions(raw []byte) (parts []model.Partition, warns []model.ParseWarning) {
	w := &warnings{source: "partitions"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		kv, _ := KV(line)
		name := kv["PartitionName"]
		if name == "" {
			if isNoRecords(line) {
				return
			}
			w.add(n, line, "no PartitionName")
			return
		}
		var fe fieldErr
		nodes, err := units.ExpandHostlist(kv["Nodes"])
		fe.set(err)
		maxTime, err := units.ParseLimit(kv["MaxTime"])
		fe.set(err)
		gpus, _ := units.GPUsFromTRES(units.ParseTRES(kv["TRES"]))
		p := model.Partition{
			Name:      name,
			State:     kv["State"],
			Default:   kv["Default"] == "YES",
			MaxTime:   maxTime,
			Nodes:     nodes,
			TotalCPUs: fe.atoi(kv["TotalCPUs"]),
			TotalGPUs: gpus,
		}
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		parts = append(parts, p)
	})
	return parts, w.list
}

// Reservations parses "scontrol show reservation -o". "No reservations in
// the system" is an empty list.
func Reservations(raw []byte) (res []model.Reservation, warns []model.ParseWarning) {
	w := &warnings{source: "reservations"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		if isNoRecords(line) {
			return
		}
		kv, _ := KV(line)
		name := kv["ReservationName"]
		if name == "" {
			w.add(n, line, "no ReservationName")
			return
		}
		var fe fieldErr
		nodes, err := units.ExpandHostlist(kv["Nodes"])
		fe.set(err)
		r := model.Reservation{
			Name:     name,
			Start:    fe.time(kv["StartTime"], units.ParseTimestamp),
			End:      fe.time(kv["EndTime"], units.ParseTimestamp),
			Nodes:    nodes,
			Flags:    splitList(kv["Flags"]),
			Users:    splitList(kv["Users"]),
			Accounts: splitList(kv["Accounts"]),
			State:    kv["State"],
		}
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		res = append(res, r)
	})
	return res, w.list
}

// JobDetails parses "scontrol show job -o <id>". An array job can print
// one line per task, so it returns a slice.
func JobDetails(raw []byte) (jobs []model.JobDetail, warns []model.ParseWarning) {
	w := &warnings{source: "jobdetail"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		kv, order := KV(line)
		if kv["JobId"] == "" {
			if isNoRecords(line) {
				return
			}
			w.add(n, line, "no JobId")
			return
		}
		d, err := jobDetail(kv)
		if err != nil {
			w.add(n, line, "%v", err)
			return
		}
		d.Raw, d.RawOrder = kv, order
		jobs = append(jobs, d)
	})
	return jobs, w.list
}

func jobDetail(kv map[string]string) (model.JobDetail, error) {
	var fe fieldErr
	rawID := kv["JobId"]
	switch {
	case kv["ArrayTaskId"] != "" && kv["ArrayJobId"] != "":
		task := kv["ArrayTaskId"]
		if strings.ContainsAny(task, ",-%") {
			task = "[" + task + "]"
		}
		rawID = kv["ArrayJobId"] + "_" + task
	case kv["HetJobId"] != "" && kv["HetJobOffset"] != "":
		rawID = kv["HetJobId"] + "+" + kv["HetJobOffset"]
	}
	id, err := units.ParseJobID(rawID)
	fe.set(err)
	state, _ := units.ParseJobState(kv["JobState"])
	user := kv["UserId"]
	if i := strings.IndexByte(user, '('); i >= 0 {
		user = user[:i]
	}

	d := model.JobDetail{
		Job: model.Job{
			ID:         id,
			Name:       kv["JobName"],
			User:       user,
			Account:    kv["Account"],
			Partition:  kv["Partition"],
			QOS:        kv["QOS"],
			State:      state,
			Reason:     kv["Reason"],
			Dependency: nullToEmpty(kv["Dependency"]),
			SubmitTime: fe.time(kv["SubmitTime"], units.ParseTimestamp),
			StartTime:  fe.time(kv["StartTime"], units.ParseTimestamp),
			EndTime:    fe.time(kv["EndTime"], units.ParseTimestamp),
			Nodes:      fe.atoi(kv["NumNodes"]),
			CPUs:       fe.atoi(kv["NumCPUs"]),
			Priority:   fe.atoi64(kv["Priority"]),
		},
		WorkDir:   nullToEmpty(kv["WorkDir"]),
		StdOut:    nullToEmpty(kv["StdOut"]),
		StdErr:    nullToEmpty(kv["StdErr"]),
		Command:   nullToEmpty(kv["Command"]),
		ExitCode:  kv["ExitCode"],
		AllocTRES: units.ParseTRES(kv["AllocTRES"]),
		ReqTRES:   units.ParseTRES(kv["ReqTRES"]),
	}
	used, _, err := units.ParseDuration(kv["RunTime"])
	fe.set(err)
	d.TimeUsed = used
	d.TimeLimit, err = units.ParseLimit(kv["TimeLimit"])
	fe.set(err)
	d.NodeList, err = units.ExpandHostlist(kv["NodeList"])
	fe.set(err)

	switch {
	case kv["MinMemoryNode"] != "":
		d.MemPerNodeMB, err = units.MemMB(kv["MinMemoryNode"], 0)
	case kv["MinMemoryCPU"] != "":
		d.MemPerNodeMB, err = units.MemMB(kv["MinMemoryCPU"], 0)
		if cpn := fe.atoi(kv["MinCPUsNode"]); cpn > 0 {
			d.MemPerNodeMB *= int64(cpn)
		}
	}
	fe.set(err)

	if g, typ := units.GPUsFromTRES(d.AllocTRES); g > 0 {
		d.GPUs, d.GPUType = g, typ
	} else if g, typ := units.GPUsFromTRES(d.ReqTRES); g > 0 {
		d.GPUs, d.GPUType = g, typ
	} else if g, typ := units.ParseGRES(kv["TresPerNode"]); g > 0 {
		d.GPUs, d.GPUType = g*max(d.Nodes, 1), typ
	}
	if d.GPUType == "" {
		_, d.GPUType = units.ParseGRES(kv["TresPerNode"])
	}
	return d, fe.err
}

// isNoRecords recognises Slurm's "nothing here" messages, such as
// "No reservations in the system".
func isNoRecords(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(l, "no ") && strings.Contains(l, "in the system")
}
