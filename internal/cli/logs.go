package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/logs"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// logStatusEvery is how often "sdash logs -f" asks Slurm whether the job
// is still running.
const logStatusEvery = 10 * time.Second

func newLogsCmd(a *app) *cobra.Command {
	var (
		stderr, follow bool
		maxBytes       int64
	)
	cmd := &cobra.Command{
		Use:               "logs <jobid>",
		ValidArgsFunction: a.completeJobs,
		Short:             "Print or follow a job's output log",
		Long: `Print the end of a job's output (or error) file, found from its
StdOut/StdErr in Slurm. With -f, keep printing new output until the job
ends or you press Ctrl-C. The file is read directly; nothing runs on the
compute node. Terminal output is sanitised; redirected output keeps the raw bytes.`,
		Example: `  sdash logs 812
  sdash logs 812 --err -f
  sdash logs 812 --bytes 0     # the whole file`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			id := args[0]
			path, note, err := a.logPathOf(ctx, rt, id, stderr)
			if err != nil {
				return err
			}
			if note != "" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), note)
			}
			if maxBytes == 0 {
				maxBytes = 1 << 62
			} else if maxBytes < 0 {
				maxBytes = logs.InitialBytes
			}
			r := logs.NewReader(a.logFS(), path, maxBytes)
			out := cmd.OutOrStdout()
			printer := logPrinter{out: out, terminal: isTerminal(out)}
			first := true
			lastCheck := time.Now()
			for {
				c := r.Poll()
				switch {
				case c.State == logs.Missing && first && !follow:
					return fmt.Errorf("%s does not exist (yet)", path)
				case c.State == logs.Unreadable:
					return fmt.Errorf("%s is not readable from this node; try: srun --jobid=%s --overlap --pty bash", path, id)
				case c.State == logs.Failed:
					return fmt.Errorf("%s: %w", path, c.Err)
				}
				if first && c.Size > int64(len(c.Data)) && c.State == logs.Reading && maxBytes < 1<<62 {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "(showing the last %d KB of %s; --bytes 0 prints all)\n", maxBytes>>10, path)
				}
				first = false
				if err := printer.write(c); err != nil {
					return err
				}
				if c.More {
					continue
				}
				if !follow {
					return printer.flush()
				}
				if time.Since(lastCheck) >= logStatusEvery {
					lastCheck = time.Now()
					if j, err := rt.sources.OwnJob(ctx, id); err != nil || !j.State.IsActive() {
						// One last read for output written as the job ended.
						if err := printer.write(r.Poll()); err != nil {
							return err
						}
						return printer.flush()
					}
				}
				select {
				case <-ctx.Done():
					return printer.flush()
				case <-time.After(time.Second):
				}
			}
		},
	}
	cmd.Flags().BoolVar(&stderr, "err", false, "the error log instead of the output log")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new output until the job ends")
	cmd.Flags().Int64Var(&maxBytes, "bytes", -1, "print at most the last N bytes (0 = all; default logs.max_initial_bytes)")
	return cmd
}

type logPrinter struct {
	out      io.Writer
	terminal bool
	stream   textsafe.Stream
}

func (p *logPrinter) write(c logs.Chunk) error {
	if !p.terminal {
		_, err := p.out.Write(c.Data)
		return err
	}
	if c.Reset {
		p.stream = textsafe.Stream{}
	}
	_, err := io.WriteString(p.out, p.stream.Text(c.Data))
	return err
}

func (p *logPrinter) flush() error {
	if !p.terminal {
		return nil
	}
	_, err := io.WriteString(p.out, p.stream.Flush())
	return err
}

// logPathOf finds a job's log: from scontrol while Slurm knows the job,
// else Slurm's default name in the job's work directory.
func (a *app) logPathOf(ctx context.Context, rt *slurmRuntime, id string, stderr bool) (path, note string, err error) {
	d, err := rt.sources.JobDetail(ctx, id)
	if err != nil {
		return "", "", err
	}
	if d != nil {
		if p := slurm.LogPath(d, stderr); p != "" {
			return p, "", nil
		}
	}
	var h *model.HistoryJob
	if rt.store.Caps.HasSacct {
		h, _ = rt.sources.HistoryJob(ctx, id)
	}
	if h == nil || h.WorkDir == "" {
		return "", "", errors.New("the job's log path is unknown: Slurm no longer knows the job and accounting has no work directory")
	}
	return filepath.Join(h.WorkDir, "slurm-"+id+".out"), "(guessing Slurm's default file name; the job's own --output is no longer known)", nil
}
