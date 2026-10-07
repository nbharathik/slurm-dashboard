package views

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

type capacityResource struct {
	label                    string
	free, total, unavailable int
}

func primaryCapacity(p state.PartSummary) capacityResource {
	if p.GPUTotal > 0 {
		return capacityResource{"GPU", p.GPUFree, p.GPUTotal, p.GPUUnavailable}
	}
	if p.MIGTotal > 0 {
		return capacityResource{"MIG", p.MIGFree, p.MIGTotal, p.MIGUnavailable}
	}
	if p.Partition.TotalGPUs > 0 {
		return capacityResource{"GPU", 0, 0, 0}
	}
	return capacityResource{"CPU", p.CPUFree, p.CPUTotal, p.CPUUnavailable}
}

// capacityLines uses one allocation bar for each partition's primary resource.
func capacityLines(ctx *Context, parts []state.PartSummary, w int) (string, [][]string) {
	th := ctx.Theme
	nameW, freeW, totalW := 9, 4, 5
	for _, p := range parts {
		nameW = max(nameW, min(layout.Width(p.Partition.Name)+1, 22))
		g := primaryCapacity(p)
		freeW = max(freeW, len(strconv.Itoa(g.free)))
		totalW = max(totalW, len(strconv.Itoa(g.total)))
		switch g.label {
		case "CPU":
			totalW = max(totalW, len(strconv.Itoa(p.Partition.TotalCPUs)))
		case "GPU":
			totalW = max(totalW, len(strconv.Itoa(p.Partition.TotalGPUs)))
		}
	}
	barW := min(8, max(w-(4+2+2+freeW+1+totalW), 2))
	gaugeW := barW + 2
	groupW := 4 + gaugeW + 2 + freeW + 1 + totalW
	wrapNames := nameW+2+groupW > w
	prefixW := nameW + 2
	if wrapNames {
		prefixW = min(2, max(w-groupW, 0))
	}
	prefix := strings.Repeat(" ", prefixW)
	header := prefix + layout.Pad("", 4, false, "") + layout.Pad("Used", gaugeW, false, "") + "  " + layout.Pad("Free", freeW, true, "") + " " + layout.Pad("Total", totalW, true, "")
	if !wrapNames {
		header = layout.Pad("Partition", nameW, false, th.Sym.Ellipsis) + header[nameW:]
	}
	out := make([][]string, len(parts))
	for i, p := range parts {
		g := primaryCapacity(p)
		free := min(max(g.free, 0), max(g.total, 0))
		unavailable := min(max(g.unavailable, 0), max(g.total-free, 0))
		used := max(g.total-free-unavailable, 0)
		freeText, totalText := strconv.Itoa(free), strconv.Itoa(g.total)
		complete := ctx.Store.Nodes.Has && p.Nodes >= len(p.Partition.Nodes) && p.CPUTotal >= p.Partition.TotalCPUs
		// Partition TRES includes MIG slices in the generic GPU total.
		known := complete && (g.label == "CPU" || p.GPUTotal+p.MIGTotal >= p.Partition.TotalGPUs)
		if !known {
			freeText, totalText = "-", "-"
			if g.label == "CPU" && p.Partition.TotalCPUs > 0 {
				totalText = strconv.Itoa(p.Partition.TotalCPUs)
			} else if g.label == "GPU" && p.MIGTotal == 0 && p.Partition.TotalGPUs > 0 {
				totalText = strconv.Itoa(p.Partition.TotalGPUs)
			}
		}
		gauge := th.Faint.Render(layout.Pad("no data", gaugeW, false, ""))
		if known && g.total > 0 {
			usage := float64(used) / float64(g.total)
			filled := min(int(usage*float64(barW)+0.5), barW)
			blocked := min(int(float64(unavailable)/float64(g.total)*float64(barW)+0.5), barW-filled)
			bar := th.OK.Render(strings.Repeat(th.Sym.Filled, filled)) + th.Faint.Render(strings.Repeat("-", blocked)) + strings.Repeat(" ", barW-filled-blocked)
			gauge = "[" + bar + "]"
		}
		name := p.Partition.Name
		if p.Partition.Default {
			name += "*"
		}
		line := prefix
		if wrapNames {
			out[i] = append(out[i], layout.Truncate(name, w, th.Sym.Ellipsis))
		} else {
			line = layout.Pad(name, nameW, false, th.Sym.Ellipsis) + "  "
		}
		line += layout.Pad(g.label, 4, false, "") + gauge + "  " + layout.Pad(freeText, freeW, true, "") + " " + layout.Pad(totalText, totalW, true, "")
		var notes []string
		if ctx.Config.Detailed() && g.label != "CPU" {
			cpuFree, cpuTotal := strconv.Itoa(p.CPUFree), strconv.Itoa(p.CPUTotal)
			if !complete {
				cpuFree = "-"
				if p.Partition.TotalCPUs > 0 {
					cpuTotal = strconv.Itoa(p.Partition.TotalCPUs)
				} else {
					cpuTotal = "-"
				}
			}
			notes = append(notes, "CPU "+cpuFree+"/"+cpuTotal+" free")
		}
		if g.label == "GPU" && p.MIGTotal > 0 {
			mig := fmt.Sprintf("MIG %d/%d free", p.MIGFree, p.MIGTotal)
			if !known {
				mig = "MIG unavailable"
			}
			notes = append(notes, mig)
		}
		if p.PendingKnown && p.Pending > 0 {
			notes = append(notes, fmt.Sprintf("%d waiting", p.Pending))
		}
		if p.Down > 0 {
			notes = append(notes, fmt.Sprintf("%d down", p.Down))
		}
		if p.Partition.State != "UP" {
			notes = append(notes, strings.ToLower(p.Partition.State))
		}
		for _, t := range p.GPUTypes {
			notes = append(notes, units.GPUDisplayName(t.Type, ctx.Config.GPUNames))
		}
		if len(notes) > 0 {
			note := strings.Join(notes, ", ")
			if layout.Width(line)+2+layout.Width(note) <= w {
				line += "  " + th.Muted.Render(note)
			} else {
				out[i] = append(out[i], line)
				wrapped := layout.Wrap(note, max(w-prefixW, 1))
				for _, n := range wrapped[:len(wrapped)-1] {
					out[i] = append(out[i], prefix+th.Muted.Render(n))
				}
				line = prefix + th.Muted.Render(wrapped[len(wrapped)-1])
			}
		}
		out[i] = append(out[i], line)
	}
	return th.Muted.Render(header), out
}

func freshness(ctx *Context, source string, at time.Time, has bool, err error) string {
	if err != nil {
		if !has {
			return ctx.Theme.Warn.Render("unavailable")
		}
		return ctx.Theme.Warn.Render("update failed")
	}
	if !has {
		return ctx.Theme.Muted.Render("waiting for data")
	}
	age := max(ctx.Now.Sub(at), 0)
	interval := 10 * time.Second
	if ctx.Interval != nil {
		interval = ctx.Interval(source)
	}
	label := units.FormatShort(age) + " ago"
	if interval > 0 && age > 2*interval {
		return ctx.Theme.Warn.Render("stale " + units.FormatShort(age) + " ago")
	}
	return ctx.Theme.Faint.Render(label)
}

func capacityHeight(ctx *Context, parts []state.PartSummary, w int) int {
	_, rows := capacityLines(ctx, parts, w)
	n := 0
	for _, r := range rows {
		n += len(r)
	}
	return max(n, 1)
}

func clusterFreshness(ctx *Context) string {
	st := ctx.Store
	type source struct {
		name string
		at   time.Time
		has  bool
		err  error
	}
	sources := []source{{"nodes", st.Nodes.At, st.Nodes.Has, st.Nodes.Err}, {"cluster", st.Cluster.At, st.Cluster.Has, st.Cluster.Err}, {"partitions", st.Partitions.At, st.Partitions.Has, st.Partitions.Err}}
	if st.AllJobs.Has {
		sources = append(sources, source{"alljobs", st.AllJobs.At, true, st.AllJobs.Err})
	} else if st.QueueRank.Has {
		sources = append(sources, source{"queuerank", st.QueueRank.At, true, st.QueueRank.Err})
	}
	chosen := sources[0]
	score := -1
	for _, s := range sources {
		rank := 0
		interval := 10 * time.Second
		if ctx.Interval != nil {
			interval = ctx.Interval(s.name)
		}
		if s.err != nil {
			rank = 3
		} else if !s.has {
			rank = 2
		} else if interval > 0 && ctx.Now.Sub(s.at) > 2*interval {
			rank = 1
		}
		if rank > score || (rank == score && s.at.Before(chosen.at)) {
			chosen = s
			score = rank
		}
	}
	return freshness(ctx, chosen.name, chosen.at, chosen.has, chosen.err)
}
