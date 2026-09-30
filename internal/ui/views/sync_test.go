package views

import (
	"slices"
	"testing"
)

// Every column the Jobs and Queue tabs show exists in the table.
func TestShownColumnsExist(t *testing.T) {
	var cols []string
	for _, c := range jobColumns {
		cols = append(cols, c.ID)
	}
	for _, c := range append(slices.Clone(jobsShown), queueShown...) {
		if !slices.Contains(cols, c) {
			t.Errorf("column %q has no table column", c)
		}
	}
}
