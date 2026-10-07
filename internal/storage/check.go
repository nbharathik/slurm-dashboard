package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// Storage query limits.
const (
	Timeout       = 30 * time.Second
	MaxConcurrent = 2
	gpfsBin       = "/usr/lpp/mmfs/bin/mmlsquota"
)

// FSStat is a filesystem's size. Free counts blocks reserved for root;
// Avail is what users can still write.
type FSStat struct {
	Total, Free, Avail uint64
	Files, FreeFiles   uint64
}

// StatFS reads a filesystem's size.
type StatFS func(path string) (FSStat, error)

// Checker reads quotas. It remembers the last good value per location, so
// a failing backend shows stale data instead of nothing.
type Checker struct {
	Runner execx.Runner // its own runner: at most 2 at a time (NewRunner)
	// Shell runs a quota command configured as a string (sh -c); see
	// execx.RunUserShell.
	Shell    func(ctx context.Context, script string) (execx.Result, error)
	User     string
	StatFS   StatFS
	LookPath func(string) (string, error)
	Now      func() time.Time

	mu      sync.Mutex
	last    map[string]model.Quota
	slots   chan struct{}
	flights map[string]*checkTask
}

// NewRunner returns the storage runner (2 commands at a time, 30 s each); argv quota commands are read-only by basename.
func NewRunner(locs []Location, log execx.Options) *execx.RealRunner {
	opts := log
	opts.MaxConcurrent, opts.DefaultTimeout = MaxConcurrent, Timeout
	for _, l := range locs {
		if l.Backend == "command" && len(l.Command.Argv) > 0 {
			opts.Policy.ExtraReadOnly = append(opts.Policy.ExtraReadOnly, filepath.Base(l.Command.Argv[0]))
		}
	}
	return execx.NewReal(opts)
}

// ShellFor runs string-form quota commands on the storage runner.
func ShellFor(r *execx.RealRunner) func(context.Context, string) (execx.Result, error) {
	return func(ctx context.Context, script string) (execx.Result, error) {
		return execx.RunUserShell(ctx, r, script, nil)
	}
}

// Check reads every location, at most MaxConcurrent at once.
func (c *Checker) Check(ctx context.Context, locs []Location) []model.Quota {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out := make([]model.Quota, len(locs))
	tasks := make([]*checkTask, len(locs))
	for i, l := range locs {
		key := l.Label + "\x00" + l.Path
		c.mu.Lock()
		if c.slots == nil {
			c.slots = make(chan struct{}, MaxConcurrent)
			c.flights = map[string]*checkTask{}
		}
		task := c.flights[key]
		c.mu.Unlock()
		if task == nil {
			select {
			case c.slots <- struct{}{}:
			case <-ctx.Done():
				out[i] = c.unavailable(l, ctx.Err())
				continue
			}
			c.mu.Lock()
			task = c.flights[key]
			if task != nil {
				<-c.slots
			} else {
				task = &checkTask{done: make(chan struct{})}
				c.flights[key] = task
				go func(t *checkTask, loc Location, k string) {
					t.result = c.one(ctx, loc)
					c.mu.Lock()
					delete(c.flights, k)
					close(t.done)
					c.mu.Unlock()
					<-c.slots
				}(task, l, key)
			}
			c.mu.Unlock()
		}
		tasks[i] = task
	}
	for i, l := range locs {
		task := tasks[i]
		if task == nil {
			continue
		}
		select {
		case <-task.done:
			out[i] = task.result
		case <-ctx.Done():
			out[i] = c.unavailable(l, ctx.Err())
		}
	}
	return out
}

type checkTask struct {
	done   chan struct{}
	result model.Quota
}

func (c *Checker) unavailable(l Location, err error) model.Quota {
	c.mu.Lock()
	defer c.mu.Unlock()
	q, ok := c.last[l.Label+"\x00"+l.Path]
	if !ok {
		q = model.Quota{Label: l.Label, Path: l.Path, Backend: l.Backend}
	}
	q.Err = textsafe.Field(firstLine(err.Error()))
	if q.Filesystem != nil {
		fs := *q.Filesystem
		fs.Err = q.Err
		q.Filesystem = &fs
	}
	return q
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// one reads a location and falls back to the last good value on error.
func (c *Checker) one(ctx context.Context, l Location) model.Quota {
	q := model.Quota{Label: l.Label, Path: l.Path, FSType: l.Mount.FSType, Backend: l.Backend, Note: l.Note}
	err := c.read(ctx, l, &q)
	if !q.IsFilesystemTotal && ctx.Err() == nil {
		fs := model.Quota{Label: q.Label, Path: q.Path, FSType: q.FSType, Backend: "statfs", IsFilesystemTotal: true}
		if fsErr := c.statfs(l, &fs); fsErr != nil {
			fs.Err = textsafe.Field(firstLine(fsErr.Error()))
		} else {
			fs.At = c.now()
		}
		q.Filesystem = &fs
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		c.last = map[string]model.Quota{}
	}
	key := l.Label + "\x00" + l.Path
	if q.Filesystem != nil {
		if ctx.Err() != nil {
			q.Filesystem.Err = textsafe.Field(firstLine(ctx.Err().Error()))
		}
		if prev, ok := c.last[key]; ok && prev.Filesystem != nil && q.Filesystem.Err != "" {
			fs := *prev.Filesystem
			fs.Err = q.Filesystem.Err
			q.Filesystem = &fs
		}
	}
	if err != nil {
		if prev, ok := c.last[key]; ok {
			prev.Err = firstLine(err.Error())
			if q.Filesystem != nil {
				prev.Filesystem = q.Filesystem
			}
			return prev
		}
		q.Err = textsafe.Field(firstLine(err.Error()))
		return q
	}
	q.At = c.now()
	c.last[key] = q
	return q
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

func (c *Checker) read(ctx context.Context, l Location, q *model.Quota) error {
	run := func(argv ...string) ([]byte, error) {
		res, err := c.Runner.Run(execx.WithLabel(ctx, "storage-"+l.Backend), argv...)
		q.Raw = textsafe.Text(strings.TrimRight(string(res.Stdout)+string(res.Stderr), "\n"))
		return res.Stdout, err
	}
	mount := l.Mount.Point
	if mount == "" {
		mount = l.Real
	}
	switch l.Backend {
	case "lustre":
		out, err := run("lfs", "quota", "-q", "-u", c.User, mount)
		if err != nil {
			return err
		}
		u, err := parseLustre(out)
		if err != nil {
			return err
		}
		u.into(q)
		return nil
	case "gpfs":
		bin := "mmlsquota"
		if c.LookPath != nil {
			if _, err := c.LookPath(bin); err != nil {
				bin = gpfsBin
			}
		}
		out, err := run(bin, "-u", c.User, "-Y", "--block-size", "1K")
		if err != nil {
			return err
		}
		u, err := parseGPFS(out, l.Mount.Device, filepath.Base(l.Mount.Point), filepath.Base(l.Real))
		if err != nil {
			return err
		}
		u.into(q)
		return nil
	case "beegfs":
		out, err := run("beegfs-ctl", "--getquota", "--uid", c.User, "--csv")
		if err == nil {
			var u usage
			if u, err = parseBeeGFS(out); err == nil {
				u.into(q)
				return nil
			}
		}
		return c.statfsFallback(l, q, err)
	case "quota":
		out, err := run("quota", "-w", "-u", c.User)
		var ee *execx.ExitError
		if errors.As(err, &ee) && strings.TrimSpace(q.Raw) == "" {
			// quota prints nothing and exits 1 when no filesystem has
			// quotas.
			err = errNoQuota
		}
		if err == nil {
			var u usage
			if u, err = parseQuota(out, l.Mount.Device); err == nil {
				u.into(q)
				return nil
			}
		}
		return c.statfsFallback(l, q, err)
	case "command":
		return c.command(ctx, l, q)
	case "statfs", "":
		q.Backend = "statfs"
		return c.statfs(l, q)
	}
	return fmt.Errorf("unknown storage backend %q", l.Backend)
}

// statfsFallback reports the filesystem total when a quota tool has
// nothing (no quota set, or the tool failed).
func (c *Checker) statfsFallback(l Location, q *model.Quota, cause error) error {
	raw := q.Raw
	if err := c.statfs(l, q); err != nil {
		return errors.Join(cause, err)
	}
	q.Raw = raw
	switch {
	case cause == nil || errors.Is(cause, errNoQuota):
	case errors.Is(cause, execx.ErrNotFound):
		q.Note = strings.TrimSpace(q.Note + " (no " + l.Backend + " tool on this node)")
	default:
		q.Note = strings.TrimSpace(q.Note + " (" + l.Backend + " failed: " + firstLine(cause.Error()) + ")")
	}
	q.Backend = l.Backend + "+statfs"
	return nil
}

func (c *Checker) statfs(l Location, q *model.Quota) error {
	if c.StatFS == nil {
		return errors.New("statfs unavailable")
	}
	st, err := c.StatFS(l.Real)
	if err != nil {
		return err
	}
	// Like df: used excludes reserved blocks, and the limit is what users
	// can reach (used + available), so the percentage matches df's Use%.
	if st.Free > st.Total || st.Avail > st.Free || st.FreeFiles > st.Files || st.Total > uint64(1<<63-1) || st.Files > uint64(1<<63-1) {
		return errors.New("invalid filesystem capacity")
	}
	used := st.Total - st.Free
	q.UsedBytes, q.SoftBytes, q.HardBytes = int64(used), 0, int64(used+st.Avail)             //nolint:gosec // filesystem sizes fit in int64
	q.UsedFiles, q.SoftFiles, q.HardFiles = int64(st.Files-st.FreeFiles), 0, int64(st.Files) //nolint:gosec // inode counts fit in int64
	q.IsFilesystemTotal = true
	q.AvailableBytes = int64(st.Avail) //nolint:gosec // Avail <= Total <= MaxInt64 checked above
	q.FilesystemBytes = int64(st.Total)
	q.AvailabilityKnown = true
	return nil
}

func (c *Checker) command(ctx context.Context, l Location, q *model.Quota) error {
	var res execx.Result
	var err error
	switch {
	case len(l.Command.Argv) > 0:
		res, err = c.Runner.Run(execx.WithLabel(ctx, "storage-command"), l.Command.Argv...)
	case l.Command.Shell != "" && c.Shell != nil:
		res, err = c.Shell(ctx, l.Command.Shell)
	default:
		return errors.New("backend \"command\" needs a command")
	}
	q.Raw = textsafe.Text(strings.TrimRight(string(res.Stdout), "\n"))
	if err != nil {
		return err
	}
	if l.Format == "json" {
		u, err := parseJSON(res.Stdout)
		if err != nil {
			return err
		}
		u.into(q)
	}
	return nil
}
