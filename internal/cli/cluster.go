package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// loadCluster fills nodes, running jobs and partitions; with pending it
// also lists everyone's jobs, for pending counts per partition.
func (rt *slurmRuntime) loadCluster(ctx context.Context, pending bool) {
	rt.loadNodes(ctx)
	st := rt.store
	var err error
	st.Partitions.Data, err = rt.sources.Partitions(ctx)
	st.Partitions.Has = rt.fail("partitions", err)
	if pending {
		st.AllJobs.Data, err = rt.sources.AllJobs(ctx)
		st.AllJobs.Has = rt.fail("alljobs", err)
	}
}

// scopedUsage is node usage within cluster.partitions and outside
// nodes.hide_partitions.
func (rt *slurmRuntime) scopedUsage() []state.NodeUsage {
	st := rt.store
	var out []state.NodeUsage
	for _, u := range state.NodeGPUUsage(st.Nodes.Data, st.Cluster.Data.Jobs, st.User) {
		if !state.Hidden(rt.cfg.HidePartitions, u.Node.Partitions) {
			out = append(out, u)
		}
	}
	return out
}

func (rt *slurmRuntime) scopedPartitions() []model.Partition {
	var out []model.Partition
	for _, p := range rt.store.Partitions.Data {
		if !slices.Contains(rt.cfg.HidePartitions, p.Name) {
			out = append(out, p)
		}
	}
	return out
}

func newNodesCmd(a *app) *cobra.Command {
	var (
		asJSON, gpu, cpu, partitions bool
		part, stateFilter, by        string
	)
	cmd := &cobra.Command{
		Use:   "nodes [NAME]",
		Short: "Every node: state, CPUs, memory, GPUs and who runs there",
		Long: `List every node with its state, CPU use and load, memory ("not tracked"
when Slurm does not account it), GPUs by type with MIG slices apart, and
the users of its running jobs. With a node name, show that node in detail.`,
		Example: `  sdash nodes
  sdash nodes --gpu -p gpu
  sdash nodes --state drained
  sdash nodes --partitions
  sdash nodes h200-01
  sdash nodes --json | jq '.nodes[] | select(.gpu_free > 0) | .name'`,
		Args:              usageArgs(cobra.MaximumNArgs(1)),
		ValidArgsFunction: a.completeNodes,
		RunE: func(cmd *cobra.Command, args []string) error {
			if partitions {
				if len(args) > 0 || gpu || cpu || part != "" || stateFilter != "" {
					return usageError{fmt.Errorf("--partitions lists partitions; it takes no node name or node filters")}
				}
				return a.runPartitions(cmd, asJSON)
			}
			if gpu && cpu {
				return usageError{fmt.Errorf("--gpu and --cpu exclude each other")}
			}
			if !slices.Contains(state.NodeSorts, by) {
				return usageError{fmt.Errorf("--sort must be one of %s", strings.Join(state.NodeSorts, ", "))}
			}
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			rt.loadNodes(ctx)
			var list []state.NodeUsage
			for _, u := range rt.scopedUsage() {
				n := u.Node
				switch {
				case len(args) == 1 && n.Name != args[0]:
				case gpu && !n.HasGPUs(), cpu && n.HasGPUs():
				case part != "" && !slices.Contains(n.Partitions, part):
				case stateFilter != "" && !stateMatches(n, stateFilter):
				default:
					list = append(list, u)
				}
			}
			state.SortNodes(list, by, false)
			doc := report.Nodes(report.NewHeader(rt.store, a.clock()), list, rt.store.User, rt.cfg.GPUNames)
			out := cmd.OutOrStdout()
			switch {
			case asJSON:
				err = writeJSON(out, doc)
			case len(args) == 1 && len(list) == 1:
				err = renderNode(out, list[0], rt.store.User, a.clock(), rt.cfg.GPUNames)
			case len(args) == 1:
				return fmt.Errorf("no node %q", args[0])
			default:
				err = renderNodes(out, doc)
			}
			if err != nil {
				return err
			}
			if !rt.store.Nodes.Has {
				return rt.hardFail()
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&asJSON, "json", false, "print JSON")
	fl.BoolVar(&gpu, "gpu", false, "only nodes with GPUs or MIG slices")
	fl.BoolVar(&cpu, "cpu", false, "only nodes without GPUs")
	fl.BoolVar(&partitions, "partitions", false, "list partitions instead of nodes: time limit, node states, free CPUs and GPUs, pending jobs")
	fl.StringVarP(&part, "partition", "p", "", "only nodes in this partition")
	fl.StringVar(&stateFilter, "state", "", "only nodes in this state: idle, mixed, alloc, drain, down, ...")
	fl.StringVar(&by, "sort", "name", "sort by "+strings.Join(state.NodeSorts, ", "))
	_ = cmd.RegisterFlagCompletionFunc("partition", a.completePartitions)
	_ = cmd.RegisterFlagCompletionFunc("sort", fixedCompletions(state.NodeSorts...))
	return cmd
}

func stateMatches(n model.Node, want string) bool {
	want = strings.ToLower(want)
	label := state.NodeStateLabel(n)
	return label == want || strings.TrimSuffix(label, "*") == want || strings.EqualFold(n.State, want) ||
		(want == "drain" && n.HasFlag("DRAIN")) || (want == "alloc" && n.State == "ALLOCATED")
}

func renderNodes(w io.Writer, doc report.NodesDoc) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NODE\tSTATE\tCPUS\tLOAD\tMEMORY\tGPUS\tTYPE\tMIG\tJOBS")
	for _, n := range doc.Nodes {
		mem := "-"
		switch {
		case n.MemAllocMB == nil && n.MemTotalMB > 0:
			mem = "not tracked/" + units.FormatMB(float64(n.MemTotalMB))
		case n.MemTotalMB > 0 && *n.MemAllocMB == 0:
			mem = "0/" + units.FormatMB(float64(n.MemTotalMB))
		case n.MemTotalMB > 0:
			mem = units.FormatMB(float64(*n.MemAllocMB)) + "/" + units.FormatMB(float64(n.MemTotalMB))
		}
		gpus, mig := "-", "-"
		if n.GPUTotal > 0 {
			gpus = fmt.Sprintf("%d/%d", n.GPUAlloc, n.GPUTotal)
		}
		if n.MIGTotal > 0 {
			mig = fmt.Sprintf("%d/%d", n.MIGAlloc, n.MIGTotal)
		}
		jobs := usersText(n.Users)
		if len(n.Users) == 0 && n.CPUAlloc > 0 {
			jobs = "busy (jobs not visible)"
		}
		if n.Reason != "" {
			jobs = strconv.Quote(n.Reason)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d/%d\t%.1f\t%s\t%s\t%s\t%s\t%s\n", n.Name, n.State, n.CPUAlloc, n.CPUTotal, n.CPULoad,
			mem, gpus, orDash(n.GPUDisplay), mig, jobs)
	}
	if len(doc.Nodes) == 0 {
		_, _ = fmt.Fprintln(tw, "(no nodes)")
	}
	return tw.Flush()
}

// usersText renders {"you": 1, "carol": 2} as "you 1 · carol 2".
func usersText(users map[string]int) string {
	if len(users) == 0 {
		return "idle"
	}
	names := make([]string, 0, len(users))
	for u := range users {
		names = append(names, u)
	}
	slices.SortFunc(names, func(a, b string) int {
		if (a == "you") != (b == "you") {
			if a == "you" {
				return -1
			}
			return 1
		}
		if users[a] != users[b] {
			return users[b] - users[a]
		}
		return strings.Compare(a, b)
	})
	parts := make([]string, len(names))
	for i, u := range names {
		parts[i] = fmt.Sprintf("%s %d", u, users[u])
	}
	return strings.Join(parts, " · ")
}

// renderNode prints one node in detail, with every running job.
func renderNode(w io.Writer, u state.NodeUsage, me string, now time.Time, aliases map[string]string) error {
	n := u.Node
	var b strings.Builder
	row := func(k, v string) { fmt.Fprintf(&b, "%-11s %s\n", k, v) }
	stateLabel := n.State
	if len(n.Flags) > 0 {
		stateLabel += "+" + strings.Join(n.Flags, "+")
	}
	row("Node", n.Name)
	row("State", stateLabel)
	if n.Reason != "" {
		row("Reason", n.Reason)
	}
	row("Partitions", strings.Join(n.Partitions, ", "))
	row("CPUs", fmt.Sprintf("%d of %d allocated, load %.1f", n.CPUAlloc, n.CPUTotal, n.CPULoad))
	switch {
	case n.MemTotalMB <= 0:
	case !n.MemTracked():
		row("Memory", "not tracked by Slurm, "+units.FormatMB(float64(n.MemTotalMB))+" total")
	default:
		row("Memory", units.FormatMB(float64(n.MemAllocMB))+" of "+units.FormatMB(float64(n.MemTotalMB))+" allocated")
	}
	if n.MemFreeMB >= 0 && n.MemTotalMB > 0 {
		row("", "the OS reports "+units.FormatMB(float64(n.MemFreeMB))+" free")
	}
	for i, g := range n.GPUs {
		label := ""
		if i == 0 {
			label = "GPUs"
		}
		kind := units.GPUDisplayName(g.Type, aliases)
		if kind != g.Type && g.Type != "" && !g.MIG {
			kind += " (" + g.Type + ")"
		}
		row(label, fmt.Sprintf("%d of %d in use  %s", g.Alloc, g.Total, kind))
	}
	if len(n.Features) > 0 {
		row("Features", strings.Join(n.Features, ", "))
	}
	if !n.BootTime.IsZero() {
		row("Booted", n.BootTime.Local().Format("2006-01-02 15:04")+" ("+units.FormatShort(now.Sub(n.BootTime))+" ago)")
	}
	if len(u.Jobs) == 0 {
		b.WriteString("\nNo running jobs visible.\n")
	} else {
		b.WriteString("\nRunning jobs\n")
	}
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, j := range u.Jobs {
		who := j.User
		if who == me {
			who = "you"
		}
		end := "no end time"
		if !j.EndTime.IsZero() {
			end = "ends " + j.EndTime.Local().Format("Jan 2 15:04") + " (in " + units.FormatShort(max(j.EndTime.Sub(now), 0)) + ")"
		}
		res := fmt.Sprintf("%d CPU", j.CPUs/max(len(j.NodeList), 1))
		if g := state.GPUShare(j); g > 0 {
			kind := "GPU"
			if j.MIGSlices > 0 || n.GPUTotal == 0 {
				kind = "MIG"
			}
			res += fmt.Sprintf(", %d %s", g, kind)
		}
		_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", j.ID.Raw, who, res, end)
	}
	_ = tw.Flush()
	_, err := io.WriteString(w, b.String())
	return err
}

// newPartitionsCmd is the old name of "nodes --partitions"; it stays as a
// hidden alias.
func newPartitionsCmd(a *app) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:    "partitions",
		Hidden: true,
		Short:  "Each partition: time limit, node states, free CPUs and GPUs, pending jobs (nodes --partitions)",
		Args:   usageArgs(cobra.NoArgs),
		RunE:   func(cmd *cobra.Command, _ []string) error { return a.runPartitions(cmd, asJSON) },
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

// runPartitions prints each partition: time limit, node states, free CPUs
// and GPUs, pending jobs.
func (a *app) runPartitions(cmd *cobra.Command, asJSON bool) error {
	ctx := cmd.Context()
	rt, err := a.slurmRuntime(ctx)
	if err != nil {
		return err
	}
	rt.loadCluster(ctx, true)
	pending, known := state.PendingByPartition(rt.store)
	sums := state.PartitionSummaries(rt.scopedPartitions(), rt.scopedUsage(), pending, known)
	doc := report.Partitions(report.NewHeader(rt.store, a.clock()), sums, rt.cfg.GPUNames)
	if asJSON {
		err = writeJSON(cmd.OutOrStdout(), doc)
	} else {
		err = renderPartitions(cmd.OutOrStdout(), doc)
	}
	if err != nil {
		return err
	}
	if !rt.store.Partitions.Has {
		return rt.hardFail()
	}
	return nil
}

func renderPartitions(w io.Writer, doc report.PartitionsDoc) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PARTITION\tSTATE\tLIMIT\tNODES (idle/mix/alloc/down)\tCPUS FREE\tGPUS FREE\tPENDING")
	for _, p := range doc.Partitions {
		name := p.Name
		if p.Default {
			name += "*"
		}
		limit := "unlimited"
		if p.MaxTimeS != nil {
			limit = units.FormatLimit(time.Duration(*p.MaxTimeS) * time.Second)
		}
		var gpus []string
		for _, t := range p.GPUTypes {
			gpus = append(gpus, fmt.Sprintf("%d/%d %s", t.Free, t.Total, t.Display))
		}
		if p.MIGTotal > 0 {
			gpus = append(gpus, fmt.Sprintf("%d/%d MIG", p.MIGFree, p.MIGTotal))
		}
		pending := "?"
		if p.Pending != nil {
			pending = strconv.Itoa(*p.Pending)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d/%d/%d/%d\t%d/%d\t%s\t%s\n", name, strings.ToLower(p.State), limit,
			p.Idle, p.Mixed, p.Alloc, p.Down, p.CPUFree, p.CPUTotal, orDash(strings.Join(gpus, ", ")), pending)
	}
	if len(doc.Partitions) == 0 {
		_, _ = fmt.Fprintln(tw, "(no partitions)")
	}
	return tw.Flush()
}
