package views

import (
	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Optional abilities of a view. The app asks for them by name, and the
// checks below make a wrong signature fail the build instead of silently
// doing nothing (as :range and / on Usage once did).
type (
	// Sorter sorts its table by a named column (:sort, header clicks).
	Sorter interface {
		SortColumns() []string
		SortBy(col string, desc bool) error
	}
	// Filterer filters its table from text (/, :filter).
	Filterer interface{ SetFilter(s string) error }
	// Searcher marks a tab where / filters instead of opening the palette.
	Searcher interface{ HasSearch() }
	// Ranger changes how many days of history a tab shows (:range).
	Ranger interface {
		SetRange(days int) tea.Cmd
		Days() int
	}
	// Opener opens a row's card by ID (from alerts, :why, :node).
	Opener interface{ Open(ctx *Context, id string) }
	// Paster takes pasted text into the input being typed.
	Paster interface{ Paste(ctx *Context, s string) }
	// Exporter gives its table as shown, for :export.
	Exporter interface {
		ExportTable() (header []string, rows [][]string)
	}
	// SourceLister names every data source the tab shows (r refreshes them).
	SourceLister interface{ Sources() []string }
	// Invalidator frees data it can rebuild when hidden for long.
	Invalidator interface{ Invalidate() }
	// JobPicker gives the selected jobs and the job in focus.
	JobPicker interface {
		SelectedJobs() []model.Job
		CurrentJob() (model.Job, bool)
	}
)

var (
	_ Sorter       = (*Jobs)(nil)
	_ Sorter       = (*Nodes)(nil)
	_ Sorter       = (*Usage)(nil)
	_ Filterer     = (*Jobs)(nil)
	_ Filterer     = (*Nodes)(nil)
	_ Filterer     = (*Usage)(nil)
	_ Searcher     = (*Jobs)(nil)
	_ Searcher     = (*Nodes)(nil)
	_ Searcher     = (*Usage)(nil)
	_ Ranger       = (*Usage)(nil)
	_ Opener       = (*Jobs)(nil)
	_ Opener       = (*Nodes)(nil)
	_ Opener       = (*Usage)(nil)
	_ Paster       = (*Jobs)(nil)
	_ Paster       = (*Nodes)(nil)
	_ Paster       = (*Usage)(nil)
	_ Paster       = (*LogView)(nil)
	_ Paster       = (*RerunView)(nil)
	_ Exporter     = (*Jobs)(nil)
	_ Exporter     = (*Nodes)(nil)
	_ Exporter     = (*Usage)(nil)
	_ Exporter     = (*Storage)(nil)
	_ SourceLister = (*Jobs)(nil)
	_ SourceLister = (*Nodes)(nil)
	_ SourceLister = (*Usage)(nil)
	_ SourceLister = (*Overview)(nil)
	_ Invalidator  = (*Jobs)(nil)
	_ JobPicker    = (*Jobs)(nil)
)
