package ui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/privatefile"
)

// uiState is what the dashboard remembers between runs
// ($XDG_STATE_HOME/sdash/state.json).
type uiState struct {
	LastTab       string               `json:"last_tab,omitempty"`
	LastSubmitted string               `json:"last_submitted,omitempty"`
	Welcome       int                  `json:"welcome,omitempty"`          // welcome card version seen
	Dismissed     map[string]time.Time `json:"dismissed_alerts,omitempty"` // key → until
	Prefs         map[string]string    `json:"prefs,omitempty"`            // view choices (views.PrefMsg)
}

func loadState(dir string) *uiState {
	s := &uiState{Dismissed: map[string]time.Time{}, Prefs: map[string]string{}}
	if dir == "" {
		return s
	}
	b, err := privatefile.Read(filepath.Join(dir, "state.json"), 1<<20)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, s)
	if s.Dismissed == nil {
		s.Dismissed = map[string]time.Time{}
	}
	if s.Prefs == nil {
		s.Prefs = map[string]string{}
	}
	now := time.Now()
	for k, until := range s.Dismissed {
		if now.After(until) {
			delete(s.Dismissed, k)
		}
	}
	return s
}

// welcomeVersion goes up when the welcome card says something new.
const welcomeVersion = 1

// welcome shows the first-run card once. Without a state directory (demo
// mode, tests) there is nowhere to remember it, so it never shows.
func (a *App) welcome() {
	if a.opt.StateDir == "" || a.state.Welcome >= welcomeVersion {
		return
	}
	a.state.Welcome = welcomeVersion
	saveState(a.opt.StateDir, a.state)
	th := a.th
	k := func(s string) string { return th.Key.Render(s) }
	body := strings.Join([]string{
		k("1-6") + "  Overview, Jobs, Queue (everyone), Nodes, Usage, Storage",
		k("/") + "    filter a table        " + k(":") + "  run a command (:help)",
		k("?") + "    every key             " + k(",") + "  settings (refresh speed, theme, ...)",
		"",
		th.Muted.Render("sdash only runs Slurm commands you could run yourself, polls gently,"),
		th.Muted.Render("and shows the exact command before it changes a job."),
		"",
		th.Muted.Render("Press any key to start."),
	}, "\n")
	a.info = &infoBox{title: "Welcome to sdash", body: body, anyKey: true}
}

// saveState writes the state atomically with private permissions. Errors
// are ignored: losing the last tab is not worth bothering the user.
func saveState(dir string, s *uiState) {
	if dir == "" || s == nil {
		return
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err == nil {
		_ = privatefile.Write(filepath.Join(dir, "state.json"), b)
	}
}
