package insights

import (
	"math"
	"slices"
	"sync"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Limits of what is kept and shown.
const (
	MinGap = time.Hour           // at most one sample per location per gap
	Keep   = 90 * 24 * time.Hour // older samples are dropped
	Window = 30 * 24 * time.Hour // what the line and the forecast look at
)

// Sample is one reading of a location's space use.
type Sample struct {
	T     time.Time `json:"t"`
	Key   string    `json:"loc"`
	Used  int64     `json:"used"`
	Limit int64     `json:"limit"` // soft limit, else hard, else 0
}

// KeyOf names a location in the log: its path, else its label.
func KeyOf(q model.Quota) string {
	if q.Path != "" {
		return q.Path
	}
	return q.Label
}

// SampleOf reads a quota. It reports false when there is nothing worth
// remembering: no space figure, or a failed check that only repeats old data.
func SampleOf(q model.Quota, now time.Time) (Sample, bool) {
	if q.UsedBytes <= 0 || q.Err != "" || KeyOf(q) == "" {
		return Sample{}, false
	}
	limit := q.SoftBytes
	if limit <= 0 {
		limit = q.HardBytes
	}
	return Sample{T: now.UTC().Truncate(time.Second), Key: KeyOf(q), Used: q.UsedBytes, Limit: limit}, true
}

// Log is the samples of every location. The zero value is empty and ready;
// a nil *Log reads as empty. Concurrent reads and writes are safe.
// History covers only periods when sdash runs; Persist saves it to a private file.
type Log struct {
	mu   sync.RWMutex
	by   map[string][]Sample // oldest first
	path string              // the file, "" for none
}

// New returns a Log holding samples (dropping any older than Keep before now).
func New(samples []Sample, now time.Time) *Log {
	l := &Log{by: map[string][]Sample{}}
	for _, s := range samples {
		if now.Sub(s.T) <= Keep {
			l.by[s.Key] = append(l.by[s.Key], s)
		}
	}
	for k := range l.by {
		slices.SortStableFunc(l.by[k], func(a, b Sample) int { return a.T.Compare(b.T) })
	}
	return l
}

// Series returns a location's samples, oldest first.
func (l *Log) Series(key string) []Sample {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return slices.Clone(l.by[key])
}

// Add records the quotas as of now and returns the samples that were new:
// a location is sampled at most once per MinGap.
func (l *Log) Add(quotas []model.Quota, now time.Time) []Sample {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.by == nil {
		l.by = map[string][]Sample{}
	}
	var added []Sample
	for _, q := range quotas {
		s, ok := SampleOf(q, now)
		if !ok {
			continue
		}
		series := l.by[s.Key]
		if n := len(series); n > 0 && s.T.Sub(series[n-1].T) < MinGap {
			continue
		}
		l.by[s.Key] = append(series, s)
		added = append(added, s)
	}
	return added
}

// Forecast says where a location's use is heading.
type Forecast struct {
	OK      bool    // enough samples to say anything
	PerDay  float64 // bytes per day, negative when shrinking
	Days    float64 // until the limit is reached; 0 unless Filling
	Filling bool    // rising, with a limit that it will reach
	Flat    bool    // no meaningful change over the window
}

// Enough samples, spread over enough time, before a forecast is offered.
const (
	minSamples = 4
	minSpan    = 24 * time.Hour
	// A change smaller than this share of the limit over the window is
	// "flat".
	flatShare = 0.01
)

// Predict fits a straight line to the window before now and reads off
// when the limit is reached. Fewer than minSamples samples, or less than
// minSpan between the first and last, give OK=false.
func Predict(series []Sample, now time.Time) Forecast {
	var pts []Sample
	for _, s := range series {
		if now.Sub(s.T) <= Window {
			pts = append(pts, s)
		}
	}
	if len(pts) < minSamples || pts[len(pts)-1].T.Sub(pts[0].T) < minSpan {
		return Forecast{}
	}
	// Least squares of Used against days since the first sample.
	trendEpoch := pts[0].T
	var sx, sy, sxx, sxy float64
	n := float64(len(pts))
	for _, s := range pts {
		x := s.T.Sub(trendEpoch).Hours() / 24
		y := float64(s.Used)
		sx, sy, sxx, sxy = sx+x, sy+y, sxx+x*x, sxy+x*y
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return Forecast{}
	}
	slope := (n*sxy - sx*sy) / den
	last := pts[len(pts)-1]
	f := Forecast{OK: true, PerDay: slope}
	span := last.T.Sub(pts[0].T).Hours() / 24
	change := slope * span
	limit := float64(last.Limit)
	ref := limit
	if ref <= 0 {
		ref = float64(last.Used)
	}
	if math.Abs(change) < flatShare*ref {
		f.Flat = true
		return f
	}
	if slope > 0 && limit > float64(last.Used) {
		f.Filling, f.Days = true, (limit-float64(last.Used))/slope
	}
	return f
}

// Points resamples a series into n values ending at now, carrying readings over gaps; NaN before the first.
func Points(series []Sample, now time.Time, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
	}
	if n <= 0 || len(series) == 0 {
		return out
	}
	start := now.Add(-Window)
	slot := Window / time.Duration(n)
	j, cur := 0, math.NaN()
	for i := range out {
		end := start.Add(time.Duration(i+1) * slot)
		for j < len(series) && !series[j].T.After(end) {
			cur = float64(series[j].Used)
			j++
		}
		out[i] = cur
	}
	return out
}
