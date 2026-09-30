// Package ui is sdash's full-screen terminal interface, built on Bubble Tea
// v2. The root App owns the store, routes keys and mouse events, draws the
// five fixed bands (header, tab bar, body, flash line, footer) and overlays
// (help, debug, confirmation, palette). Views never run commands; the App
// turns their requests into actions through internal/actions.
package ui
