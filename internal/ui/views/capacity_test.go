package views

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

func TestCapacityColumnAlignment(t *testing.T) {
	parts := []state.PartSummary{
		{Partition: model.Partition{Name: "compute-node", State: "UP"}, CPUFree: 140, CPUTotal: 320},
		{Partition: model.Partition{Name: "gpu-node-mig", State: "UP"}, CPUFree: 60, CPUTotal: 64, MIGFree: 10, MIGTotal: 11},
		{Partition: model.Partition{Name: "login-node", State: "UP"}, CPUFree: 16, CPUTotal: 16},
		{Partition: model.Partition{Name: "gpu-node", State: "UP"}, CPUFree: 204, CPUTotal: 256, GPUFree: 3, GPUTotal: 8},
		{Partition: model.Partition{Name: "gpu-node-bw", State: "UP"}, CPUFree: 120, CPUTotal: 128, GPUTotal: 2},
		{Partition: model.Partition{Name: "長い名前", State: "UP"}, CPUFree: 7, CPUTotal: 1234567, GPUFree: 6, GPUTotal: 8, MIGFree: 7, MIGTotal: 14},
	}
	headings := regexp.MustCompile(`(Free) +(Total)`)
	values := regexp.MustCompile(`(CPU|GPU|MIG) +\[[^\]]*\] +([0-9]+) +([0-9]+)`)
	for _, width := range []int{40, 60, 80, 100, 160} {
		for _, palette := range []string{theme.Dark, theme.Light, theme.HighContrast} {
			for _, ascii := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/%s/ascii-%t", width, palette, ascii), func(t *testing.T) {
					st := &state.Store{}
					st.Nodes.Has = true
					ctx := &Context{Store: st, Config: config.Default(), Theme: theme.New(palette, true, false, ascii)}
					header, rows := capacityLines(ctx, parts, width)
					header = ansi.Strip(header)
					columns := headings.FindAllStringSubmatchIndex(header, -1)
					seen := map[string]int{}
					for _, row := range rows {
						for _, line := range row {
							line = ansi.Strip(line)
							if layout.Width(line) > width {
								t.Fatalf("line exceeds %d columns: %q", width, line)
							}
							for _, cell := range values.FindAllStringSubmatchIndex(line, -1) {
								freeEnd, totalEnd := layout.Width(line[:cell[5]]), layout.Width(line[:cell[7]])
								aligned := false
								for _, column := range columns {
									aligned = aligned || (freeEnd == layout.Width(header[:column[3]]) && totalEnd == layout.Width(header[:column[5]]))
								}
								if !aligned {
									t.Errorf("counts do not align with headings:\n%s\n%s", header, line)
								}
								resource := line[cell[2]:cell[3]]
								seen[resource]++
							}
						}
					}
					if seen["CPU"] != 2 || seen["GPU"] != 3 || seen["MIG"] != 1 {
						t.Errorf("missing capacity cells: %v", seen)
					}
					header, _ = capacityLines(ctx, parts[:1], width)
					if strings.Count(header, "Free") != 1 || strings.Count(header, "Total") != 1 {
						t.Errorf("CPU-only cluster has unused headings: %q", header)
					}
				})
			}
		}
	}
}

func TestCapacityMIGPartitionTRES(t *testing.T) {
	nodes, warnings := parse.Nodes([]byte("NodeName=mig01 State=MIXED CPUTot=64 CPUAlloc=4 Gres=gpu:a100_1g.10gb:11 GresUsed=gpu:a100_1g.10gb:1"))
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	parts, warnings := parse.Partitions([]byte("PartitionName=mig State=UP Nodes=mig01 TotalCPUs=64 TRES=cpu=64,gres/gpu=11"))
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	st := &state.Store{}
	st.Nodes.Has = true
	ctx := &Context{Store: st, Config: config.Default(), Theme: theme.New(theme.Dark, true, true, false)}
	summaries := state.PartitionSummaries(parts, state.NodeGPUUsage(nodes, nil, "you"), nil, nil)
	_, rows := capacityLines(ctx, summaries, 100)
	line := ansi.Strip(strings.Join(rows[0], "\n"))
	if !regexp.MustCompile(`MIG \[█ +\] +10 +11`).MatchString(line) || strings.Contains(line, "?") {
		t.Fatalf("MIG slices were confused with physical GPUs: %s", line)
	}
	summaries[0].Partition.Nodes = append(summaries[0].Partition.Nodes, "missing")
	_, rows = capacityLines(ctx, summaries, 100)
	line = ansi.Strip(strings.Join(rows[0], "\n"))
	if !strings.Contains(line, "no data") || strings.Contains(line, "[") || strings.Contains(line, "?") {
		t.Fatalf("incomplete capacity must not look free or fully allocated: %s", line)
	}
}

func TestCapacityPrimaryAllocationBar(t *testing.T) {
	st := &state.Store{}
	st.Nodes.Has = true
	ctx := &Context{Store: st, Config: config.Default(), Theme: theme.New(theme.Dark, true, true, false)}
	for _, tc := range []struct {
		name string
		part state.PartSummary
		want string
	}{
		{"gpu", state.PartSummary{CPUFree: 204, CPUTotal: 256, GPUFree: 3, GPUTotal: 8}, "GPU [█████   ]"},
		{"mig", state.PartSummary{CPUFree: 60, CPUTotal: 64, MIGFree: 10, MIGTotal: 11}, "MIG [█       ]"},
		{"idle", state.PartSummary{CPUFree: 16, CPUTotal: 16}, "CPU [        ]"},
		{"full", state.PartSummary{GPUTotal: 8}, "GPU [████████]"},
		{"down", state.PartSummary{CPUTotal: 16, CPUUnavailable: 16, Down: 1}, "CPU [--------]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.part.Partition = model.Partition{Name: tc.name, State: "UP"}
			_, rows := capacityLines(ctx, []state.PartSummary{tc.part}, 120)
			line := ansi.Strip(strings.Join(rows[0], "\n"))
			if !strings.Contains(line, tc.want) || strings.Count(line, "[") != 1 {
				t.Errorf("want one matching allocation bar %q, got %q", tc.want, line)
			}
		})
	}
}
