package model

import "testing"

func TestResolve(t *testing.T) {
	for _, c := range []struct {
		in      string
		tab     string
		gpuOnly bool
		ok      bool
	}{
		{"nodes", TabNodes, false, true},
		{"GPUs", TabNodes, true, true},
		{" queue ", TabQueue, false, true},
		{"overview", TabOverview, false, true},
		{"History", TabUsage, false, true},
		{"usage", TabUsage, false, true},
		{"nope", "", false, false},
	} {
		tab, gpu, ok := ResolveTab(c.in)
		if tab != c.tab || gpu != c.gpuOnly || ok != c.ok {
			t.Errorf("Resolve(%q) = %q %v %v", c.in, tab, gpu, ok)
		}
	}
	if len(TabNames()) != len(Tabs) || len(AcceptedTabNames()) != len(Tabs)+2 {
		t.Errorf("TabNames() = %v, Names() = %v", TabNames(), AcceptedTabNames())
	}
}
