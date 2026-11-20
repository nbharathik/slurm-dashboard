package ui

// layer is what sits on top of the screen. The order below is the one
// place that decides which layer gets keys, mouse and paste and which is
// drawn last; the higher the value, the higher the layer.
type layer int

const (
	layerTab layer = iota
	layerLog
	layerRerun
	layerDebug
	layerHelp
	layerInfo
	layerSettings
	layerPalette
	layerConfirm
)

// top is the highest open layer.
func (a *App) top() layer {
	switch {
	case a.confirm != nil:
		return layerConfirm
	case a.palette != nil:
		return layerPalette
	case a.settings != nil:
		return layerSettings
	case a.info != nil:
		return layerInfo
	case a.overlay == overlayHelp:
		return layerHelp
	case a.overlay == overlayDebug:
		return layerDebug
	case a.rerun != nil:
		return layerRerun
	case a.logView != nil:
		return layerLog
	}
	return layerTab
}

// screen is the full-screen layer under any box: the rerun form, the log
// viewer or the tab.
func (a *App) screen() layer {
	switch {
	case a.rerun != nil:
		return layerRerun
	case a.logView != nil:
		return layerLog
	}
	return layerTab
}
