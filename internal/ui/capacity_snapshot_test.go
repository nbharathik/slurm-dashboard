package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

func TestCapacitySnapshots(t *testing.T) {
	f := newFixture(t, 0)
	nodes := append([]model.Node{}, f.st.Nodes.Data...)
	nodes = append(nodes,
		model.Node{Name: "large-up", State: "MIXED", CPUTotal: 1234567, CPUAlloc: 1234560, MemTotalMB: 999999, MemFreeMB: -1, GPUTotal: 8, GPUAlloc: 2, MIGTotal: 14, MIGAlloc: 4, GPUs: []model.GPUGroup{{Type: "h100", Total: 8, Alloc: 2}, {Type: "a100_1g.10gb", Total: 14, Alloc: 4, MIG: true}}},
		model.Node{Name: "large-down", State: "DOWN", CPUTotal: 128, GPUTotal: 4, MemTotalMB: 524288, MemFreeMB: -1, GPUs: []model.GPUGroup{{Type: "h200", Total: 4}}},
	)
	parts := append([]model.Partition{}, f.st.Partitions.Data...)
	parts = append(parts, model.Partition{Name: "長い-partition-name-for-mixed-GPU-and-MIG", State: "UP", Nodes: []string{"large-up", "large-down"}})
	f.st.Apply(state.Update{Source: "nodes", Data: nodes, At: f.clock.Now().Add(-time.Hour)})
	f.st.Apply(state.Update{Source: "partitions", Data: parts, At: f.clock.Now()})
	for _, width := range []int{40, 60, 80, 100, 160} {
		for _, ascii := range []bool{false, true} {
			if ascii {
				parts[len(parts)-1].Name = "long-partition-name-for-mixed-GPU-and-MIG"
			} else {
				parts[len(parts)-1].Name = "長い-partition-name-for-mixed-GPU-and-MIG"
			}
			f.st.Apply(state.Update{Source: "partitions", Data: parts, At: f.clock.Now()})
			name := fmt.Sprintf("capacity-%d-ascii-%t", width, ascii)
			t.Run(name, func(t *testing.T) {
				a := f.app(t, width, 45, ascii)
				got := screen(a)
				checkSize(t, got, width, 45)
				golden(t, name, got)
			})
			t.Run(name+"-nodes", func(t *testing.T) {
				a := f.app(t, width, 45, ascii)
				press(a, "4")
				got := screen(a)
				checkSize(t, got, width, 45)
				golden(t, name+"-nodes", got)
			})
		}
	}
}
