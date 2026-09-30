package insights

import "slices"

// Median returns the median of v (the mean of the two middle values when
// len(v) is even), or 0 for an empty slice. v is not modified.
func Median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	slices.Sort(s)
	m := len(s) / 2
	if len(s)%2 == 0 {
		return (s[m-1] + s[m]) / 2
	}
	return s[m]
}
