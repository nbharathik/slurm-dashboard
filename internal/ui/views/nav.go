package views

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/keys"
)

// navigate moves a table's cursor for the shared navigation keys and the
// mouse wheel, and reports whether msg was one of them.
func navigate(t *components.Table, k *keys.Map, msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, k.Up):
			t.Move(-1)
		case key.Matches(msg, k.Down):
			t.Move(1)
		case key.Matches(msg, k.PageUp):
			t.Page(-1)
		case key.Matches(msg, k.PageDown):
			t.Page(1)
		case key.Matches(msg, k.Home):
			t.Home()
		case key.Matches(msg, k.End):
			t.End()
		default:
			return false
		}
		return true
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			t.Move(-3)
		case tea.MouseWheelDown:
			t.Move(3)
		default:
			return false
		}
		return true
	}
	return false
}
