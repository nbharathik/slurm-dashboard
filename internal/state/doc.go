// Package state collects, schedules and caches dashboard data with freshness tracking.
// The scheduler handles polling and cancellation; Store is mutated only by the
// UI update loop or a single CLI goroutine.
package state
