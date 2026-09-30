// Package views implements the five tabs (Overview, Jobs, GPUs, History,
// Storage) and the full-screen viewers (job detail, logs, rerun form).
//
// A view is a pure function of the store plus its own view state (cursor,
// sort, filter). Views never run commands: they return messages that ask
// the app to run an action, open a log, or refresh a source.
package views
