package parse

import "testing"

// D3: a 4-task step's memory is the total of its tasks, not its largest task.
func TestSstatMemoryIsTotalOfTasks(t *testing.T) {
	raw := "900.0|4|1G|900M|00:10:00|cpu=00:40:00,mem=4G\n"
	stat, warns := Sstat([]byte(raw))
	if len(warns) > 0 || stat == nil {
		t.Fatalf("stat %+v, warnings %+v", stat, warns)
	}
	if stat.MaxRSSMB != 4096 || stat.NTasks != 4 {
		t.Fatalf("memory %d MB over %d tasks, want 4096 over 4", stat.MaxRSSMB, stat.NTasks)
	}
	only := "901.batch|1|2G|1G|00:01:00|\n" // no TRES: MaxRSS is the fallback
	if s, _ := Sstat([]byte(only)); s == nil || s.MaxRSSMB != 2048 {
		t.Fatalf("fallback %+v", s)
	}
}
