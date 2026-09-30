package ui

import (
	"slices"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

func TestThemesMatchConfig(t *testing.T) {
	a, b := slices.Sorted(slices.Values(theme.Names)), slices.Sorted(slices.Values(config.Themes))
	if !slices.Equal(a, b) {
		t.Errorf("theme names %v, config %v", a, b)
	}
}
