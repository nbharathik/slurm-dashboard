package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/actions"
	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

func newFreeCmd(a *app) *cobra.Command {
	var (
		asJSON, test     bool
		req              state.Request
		mem, limit, part string
	)
	cmd := &cobra.Command{
		Use:   "free",
		Short: "Where can a job run now, or soonest?",
		Long: `Find the nodes that could start a job of this size now, from what is
allocated and free. When none can, name the node that frees up first by
the end times of its running jobs. Jobs waiting ahead of yours are not
counted; --test also asks the scheduler (sbatch --test-only), which
submits nothing.`,
		Example: `  sdash free --gpus 1 --gpu-type h200
  sdash free --cpus 32 --mem 64G --time 12h -p cpu
  sdash free --gpus 1 --gpu-type 1g.16gb --test`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if req.GPUs < 0 || req.CPUs < 1 || req.Nodes < 1 {
				return usageError{fmt.Errorf("--gpus must be 0 or more, --cpus and --nodes 1 or more")}
			}
			if mem != "" {
				mb, _, err := units.ParseMemMB(mem)
				if err != nil {
					return usageError{fmt.Errorf("--mem: %v", err)}
				}
				req.MemMB = int64(mb)
			}
			if limit != "" {
				d, err := parseTimeFlag(limit)
				if err != nil {
					return usageError{err}
				}
				req.Time = d
			}
			req.Partition = part
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			rt.loadCluster(ctx, false)
			res := state.FitNodes(rt.scopedUsage(), rt.scopedPartitions(), req, rt.cfg.GPUNames)
			doc := report.Free(report.NewHeader(rt.store, a.clock()), req, res)
			if test {
				dir, _ := os.Getwd()
				asked := req
				asked.GPUType = state.ResolveGPUType(rt.scopedUsage(), req.GPUType, rt.cfg.GPUNames)
				if asked.Partition == "" { // ask about the partition that fits, not the default one
					switch {
					case len(res.Now) > 0:
						asked.Partition = res.Now[0].Partitions[0]
					case res.Soonest != nil:
						asked.Partition = res.Soonest.Partitions[0]
					}
				}
				est, err := actions.Estimate(ctx, rt.runner, testScript(asked), dir, nil)
				t := &report.TestOnly{}
				if err != nil {
					t.Error = err.Error()
				} else {
					start := est.Start
					t.Start, t.Nodes, t.Partition = &start, est.Nodes, est.Partition
				}
				doc.TestOnly = t
			}
			if asJSON {
				err = writeJSON(cmd.OutOrStdout(), doc)
			} else {
				err = renderFree(cmd.OutOrStdout(), doc, req, a.clock())
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
	fl.IntVar(&req.GPUs, "gpus", 0, "GPUs (or MIG slices) per node")
	fl.StringVar(&req.GPUType, "gpu-type", "", `GPU type or MIG profile, e.g. h200, "RTX PRO 6000", 1g.16gb`)
	fl.IntVar(&req.CPUs, "cpus", 1, "CPUs per node")
	fl.StringVar(&mem, "mem", "", "memory per node, e.g. 32G")
	fl.StringVar(&limit, "time", "", "time limit, e.g. 4h, 90m, 2:00:00 or 1-12:00:00")
	fl.IntVar(&req.Nodes, "nodes", 1, "nodes")
	fl.StringVarP(&part, "partition", "p", "", "only this partition")
	fl.BoolVar(&test, "test", false, "also ask the scheduler with sbatch --test-only (submits nothing)")
	_ = cmd.RegisterFlagCompletionFunc("partition", a.completePartitions)
	return cmd
}

// parseTimeFlag reads "4h" or "90m" (Go) or Slurm's "2:00:00" and "1-12".
func parseTimeFlag(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d, nil
	}
	d, err := units.ParseLimit(s)
	if err != nil || d == nil || *d <= 0 {
		return 0, fmt.Errorf("--time %q: use 4h, 90m, 2:00:00 or 1-12:00:00", s)
	}
	return *d, nil
}

// testScript is the batch script "sbatch --test-only" checks for req.
func testScript(req state.Request) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&b, "#SBATCH --nodes=%d\n#SBATCH --ntasks-per-node=1\n#SBATCH --cpus-per-task=%d\n", req.Nodes, req.CPUs)
	if req.GPUs > 0 {
		gres := fmt.Sprintf("gpu:%d", req.GPUs)
		if t := strings.TrimSpace(req.GPUType); t != "" && !strings.ContainsAny(t, " \t") {
			gres = fmt.Sprintf("gpu:%s:%d", t, req.GPUs)
		}
		fmt.Fprintf(&b, "#SBATCH --gres=%s\n", gres)
	}
	if req.MemMB > 0 {
		fmt.Fprintf(&b, "#SBATCH --mem=%dM\n", req.MemMB)
	}
	if req.Time > 0 {
		fmt.Fprintf(&b, "#SBATCH --time=%d\n", int(max(req.Time.Minutes(), 1)))
	}
	if req.Partition != "" {
		fmt.Fprintf(&b, "#SBATCH --partition=%s\n", req.Partition)
	}
	b.WriteString("true\n")
	return b.String()
}

func renderFree(w io.Writer, doc report.FreeDoc, req state.Request, now time.Time) error {
	var b strings.Builder
	what := []string{plural(req.CPUs, "CPU")}
	if req.GPUs > 0 {
		g := plural(req.GPUs, "GPU")
		if req.GPUType != "" {
			g += " (" + req.GPUType + ")"
		}
		what = append([]string{g}, what...)
	}
	if req.MemMB > 0 {
		what = append(what, units.FormatMB(float64(req.MemMB)))
	}
	if req.Time > 0 {
		what = append(what, units.FormatLimit(req.Time))
	}
	if req.Nodes > 1 {
		what = append(what, fmt.Sprintf("on each of %d nodes", req.Nodes))
	}
	fmt.Fprintf(&b, "Looking for: %s\n\n", strings.Join(what, ", "))
	fitLine := func(tw *tabwriter.Writer, f report.FitNode) {
		mem := "not tracked"
		if f.FreeMemMB != nil {
			mem = units.FormatMB(float64(*f.FreeMemMB))
		}
		_, _ = fmt.Fprintf(tw, "  %s\t%s\t%d\t%d\t%s\n", f.Node, strings.Join(f.Partitions, ","), f.FreeCPUs, f.FreeGPUs, mem)
	}
	switch {
	case len(doc.Now) > 0:
		fmt.Fprintf(&b, "Fits now on %d node(s):\n", len(doc.Now))
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  NODE\tPARTITIONS\tFREE CPUS\tFREE GPUS\tFREE MEMORY")
		for _, f := range doc.Now {
			fitLine(tw, f)
		}
		_ = tw.Flush()
		if req.Nodes > len(doc.Now) {
			fmt.Fprintf(&b, "That is fewer than the %d nodes asked for.\n", req.Nodes)
		}
	case doc.Soonest != nil:
		s := doc.Soonest
		fmt.Fprintf(&b, "Nothing fits now. Soonest: %s (%s) at %s, in %s, when running jobs there reach their time limits.\n",
			s.Node, strings.Join(s.Partitions, ","), s.At.Local().Format("Mon 15:04"), units.FormatShort(max(s.At.Sub(now), 0)))
	default:
		b.WriteString("No node can take this job.\n")
	}
	for _, r := range doc.Reasons {
		fmt.Fprintf(&b, "  %s\n", r)
	}
	if t := doc.TestOnly; t != nil {
		b.WriteString("\nThe scheduler (sbatch --test-only): ")
		if t.Error != "" {
			b.WriteString(t.Error + "\n")
		} else {
			fmt.Fprintf(&b, "would start %s on %s in %s.\n", t.Start.Local().Format("Mon 15:04"), orDash(t.Nodes), orDash(t.Partition))
		}
	}
	b.WriteString("\n" + report.FreeNote)
	if doc.TestOnly == nil {
		b.WriteString(" Add --test to ask the scheduler.")
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
}
