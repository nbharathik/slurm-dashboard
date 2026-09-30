package views

import (
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

type themeKindT = theme.StateKind

func kindOf(j model.Job) theme.StateKind { return theme.KindOf(j.State, j.Reason) }
