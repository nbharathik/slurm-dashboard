package model

import "strings"

// Tab names.
const (
	TabOverview = "overview"
	TabJobs     = "jobs"
	TabQueue    = "queue"
	TabNodes    = "nodes"
	TabUsage    = "usage"
	TabStorage  = "storage"
)

// Tab is one tab: its stable name and its label.
type Tab struct{ Name, Title string }

// Tabs lists the tabs in order; key N opens the Nth.
var Tabs = []Tab{
	{TabOverview, "Overview"},
	{TabJobs, "Jobs"},
	{TabQueue, "Queue"},
	{TabNodes, "Nodes"},
	{TabUsage, "Usage"},
	{TabStorage, "Storage"},
}

// alias is another name for a tab. GPUOnly says it also asks for the GPU
// filter ("gpus" is Nodes with only GPU nodes).
type alias struct {
	tab     string
	gpuOnly bool
}

// aliases are other names a tab answers to: "gpus" was the old name of
// the GPU view (now Nodes) and "history" the old name of Usage.
var aliases = map[string]alias{
	"gpus":    {TabNodes, true},
	"gpu":     {TabNodes, true},
	"history": {TabUsage, false},
}

// ResolveTab returns the tab a name or alias stands for (case-insensitive),
// and whether the name asks for GPU nodes only.
func ResolveTab(name string) (tab string, gpuOnly, ok bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if a, found := aliases[name]; found {
		return a.tab, a.gpuOnly, true
	}
	for _, t := range Tabs {
		if t.Name == name {
			return t.Name, false, true
		}
	}
	return "", false, false
}

// TabNames lists the tabs' own names, in order.
func TabNames() []string {
	out := make([]string, 0, len(Tabs))
	for _, t := range Tabs {
		out = append(out, t.Name)
	}
	return out
}

// AcceptedTabNames lists every accepted name: the tabs, then the aliases worth
// suggesting.
func AcceptedTabNames() []string { return append(TabNames(), "gpus", "history") }
