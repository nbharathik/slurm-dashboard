package model

import "time"

// SchedStats is what sdiag says about the controller and scheduler.
// Durations are in microseconds, as sdiag prints them.
type SchedStats struct {
	At, Since time.Time

	ServerThreads, AgentQueue, DBDQueue int

	Submitted, Started, Completed, Cancelled, Failed int
	Pending, Running                                 int

	Main     CycleStats
	Backfill BackfillStats

	RPCTypes []RPCStat // busiest message types first
	RPCUsers []RPCStat // busiest users first
}

// CycleStats are the main scheduler's numbers.
type CycleStats struct {
	LastUS, MaxUS, MeanUS int64
	MeanDepth             int
	PerMinute             int
	LastQueue             int
}

// BackfillStats are the backfill scheduler's numbers.
type BackfillStats struct {
	Jobs        int // backfilled since the last stats reset
	JobsTotal   int // since slurmctld started
	Cycles      int
	LastAt      time.Time
	LastUS      int64
	MaxUS       int64
	MeanUS      int64
	LastDepth   int
	LastTried   int // jobs it tried to start in the last cycle
	MeanDepth   int
	QueueLength int
}

// RPCStat is one row of sdiag's RPC tables.
type RPCStat struct {
	Name   string
	Count  int64
	AveUS  int64
	TotalU int64
}
