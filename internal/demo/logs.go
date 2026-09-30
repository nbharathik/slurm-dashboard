package demo

import (
	"fmt"
	"io/fs"
	"math"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/logs"
)

// LogFS serves the simulated jobs' log files. Content is a pure function
// of the job's run time, so files only grow (until the loop restarts,
// which looks like a truncation).
func (s *Sim) LogFS() logs.FS { return simFS{s} }

type simFS struct{ s *Sim }

func (f simFS) Open(name string) (logs.File, error) {
	content, ok := f.s.logContent(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{name: name, r: strings.NewReader(content), size: int64(len(content))}, nil
}

type memFile struct {
	name string
	r    *strings.Reader
	size int64
}

func (m *memFile) ReadAt(p []byte, off int64) (int, error) { return m.r.ReadAt(p, off) }
func (m *memFile) Close() error                            { return nil }
func (m *memFile) Stat() (fs.FileInfo, error)              { return memInfo{m}, nil }

type memInfo struct{ m *memFile }

func (i memInfo) Name() string       { return i.m.name }
func (i memInfo) Size() int64        { return i.m.size }
func (i memInfo) Mode() fs.FileMode  { return 0o644 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }

// logContent finds the job writing path and renders its log so far.
func (s *Sim) logContent(path string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	rows, ended := s.snapshot(now)
	for _, r := range rows {
		if r.state != "RUNNING" {
			continue
		}
		out, errp := logPaths(r.job, r.jobID, r.task)
		if path == out || path == errp {
			return render(r.job, now.Sub(r.start), false, false, path == errp), true
		}
	}
	for _, d := range ended {
		task := ""
		if _, t, ok := strings.Cut(d.raw, "_"); ok {
			task = t
		}
		out, errp := logPaths(d.job, d.jobID, task)
		if path == out || path == errp {
			return render(d.job, d.end.Sub(d.start), true, d.state != "COMPLETED", path == errp), true
		}
	}
	return "", false
}

// render writes a job's log for the given run time.
func render(j *Job, ran time.Duration, ended, failed, stderr bool) string {
	var b strings.Builder
	sec := int(ran.Seconds())
	switch j.Log {
	case "train":
		renderTrain(&b, j, sec, stderr)
	case "eval":
		renderEval(&b, sec, stderr, ended && !failed)
	case "oom":
		renderOOM(&b, j, sec, failed, stderr)
	default:
		if !stderr {
			for t := 0; t <= sec; t += 60 {
				fmt.Fprintf(&b, "[%s] %s: working (%d)\n", clock(t), j.Name, t/60)
			}
		}
	}
	return b.String()
}

func clock(sec int) string {
	return fmt.Sprintf("%02d:%02d:%02d", sec/3600, sec/60%60, sec%60)
}

// every returns the event times in [0, sec] spaced by step seconds.
func every(sec, step int, fn func(t, i int)) {
	for t, i := 0, 0; t <= sec; t, i = t+step, i+1 {
		fn(t, i)
	}
}

func renderTrain(b *strings.Builder, j *Job, sec int, stderr bool) {
	if stderr {
		b.WriteString("/home/you/venvs/ml/lib/python3.12/site-packages/torch/_utils.py:831: UserWarning: TypedStorage is deprecated.\n")
		every(sec, 1800, func(t, i int) {
			if i > 0 {
				fmt.Fprintf(b, "[%s] WARNING: dataloader worker %d was slow (%.1fs); consider more --cpus-per-task\n", clock(t), i%8, 2.5+float64(i%5))
			}
		})
		return
	}
	b.WriteString("+ module load cuda/12.4 python/3.12\n")
	b.WriteString("Python 3.12.4 | torch 2.4.0 | CUDA 12.4\n")
	fmt.Fprintf(b, "Using %d GPU(s): NVIDIA H100 80GB HBM3\n", max(j.GPUs, 1))
	b.WriteString("Loading dataset from /scratch/you/data/tokens ... 1048576 samples\n")
	const stepEvery = 4 // seconds of run time per logged step
	every(sec, stepEvery, func(_, i int) {
		step := (i + 1) * 10
		loss := 0.9 + 1.9*math.Exp(-float64(step)/6000) + 0.03*math.Sin(float64(step))
		if step%250 == 0 {
			// A progress bar that redraws itself with carriage returns.
			for p := 0; p <= 100; p += 25 {
				fmt.Fprintf(b, "\reval: %3d%%|%-20s| %d/64", p, strings.Repeat("#", p/5), p*64/100)
			}
			fmt.Fprintf(b, "\neval  step %6d | val_loss %.4f | ppl %.2f\n", step, loss+0.04, math.Exp(loss+0.04))
		}
		fmt.Fprintf(b, "step %6d | epoch %d | loss %.4f | lr %.1e | %.1fk tok/s | gpu mem %.1fG\n",
			step, step/2000+1, loss, 3e-4*math.Min(1, float64(step)/500), 40+2*math.Sin(float64(step)/37), 57.8)
		if step%1500 == 0 {
			fmt.Fprintf(b, "WARNING: gradient norm %.1f exceeded clip threshold 1.0\n", 8+math.Mod(float64(step)/100, 7))
		}
		if step%3000 == 0 {
			fmt.Fprintf(b, "checkpoint saved to %s/ckpt-%d.pt\n", j.WorkDir, step)
		}
	})
}

func renderEval(b *strings.Builder, sec int, stderr, done bool) {
	if stderr {
		return
	}
	b.WriteString("Evaluating checkpoint ckpt-42000.pt on 3 benchmarks\n")
	every(sec, 20, func(t, i int) {
		fmt.Fprintf(b, "[%s] batch %4d/400 | acc %.3f\n", clock(t), (i*7)%400+1, 0.70+0.03*math.Sin(float64(i)/9))
	})
	if done {
		b.WriteString("Final accuracy: 0.734 (mmlu 0.712, gsm8k 0.698, arc 0.792)\ndone\n")
	}
}

func renderOOM(b *strings.Builder, j *Job, sec int, failed, stderr bool) {
	if stderr {
		if !failed {
			return
		}
		b.WriteString("Traceback (most recent call last):\n")
		b.WriteString("  File \"/home/you/data/preprocess.py\", line 88, in <module>\n")
		b.WriteString("    merged = np.concatenate(shards)\n")
		b.WriteString("numpy.core._exceptions._ArrayMemoryError: Unable to allocate 12.0 GiB for an array with shape (3221225472,) and data type float32\n")
		fmt.Fprintf(b, "slurmstepd: error: Detected 1 oom_kill event in StepId=%d.batch. Some of the step tasks have been OOM Killed.\n", j.ID)
		return
	}
	b.WriteString("preprocess: 128 shards, 16 workers\n")
	every(sec, 3, func(t, i int) {
		if i < 128 {
			fmt.Fprintf(b, "[%s] shard %03d/128 ok (%.1f GB in memory)\n", clock(t), i+1, 1.2+float64(i)*1.05)
		}
	})
	if failed {
		b.WriteString("merging shards ...\n")
	}
}
