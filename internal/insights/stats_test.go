package insights

import "testing"

func TestMedian(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{
		{nil, 0},
		{[]float64{0.4}, 0.4},
		{[]float64{0.9, 0.1, 0.5}, 0.5},
		{[]float64{0.4, 0.1, 0.2, 0.3}, 0.25},
	}
	for _, tc := range cases {
		if got := Median(tc.in); got != tc.want {
			t.Errorf("Median(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	in := []float64{3, 1, 2}
	Median(in)
	if in[0] != 3 {
		t.Error("Median modified its input")
	}
}
