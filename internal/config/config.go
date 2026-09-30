package config

import (
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// MinRefresh is the lowest allowed collector interval. It protects the
// shared Slurm controller; the scheduler enforces it again at run time.
const MinRefresh = 5 * time.Second

// Config is the parsed config.toml; every field has a default (see Default).
type Config struct {
	Refresh        string             `toml:"refresh"` // fast | normal | slow | manual
	StartTab       string             `toml:"start_tab"`
	Theme          string             `toml:"theme"`  // auto | dark | light | high-contrast
	Layout         string             `toml:"layout"` // clean | detailed
	ASCII          bool               `toml:"ascii"`
	Mouse          bool               `toml:"mouse"`
	Notify         bool               `toml:"notify"`
	NotifyCommand  string             `toml:"notify_command"`
	Shell          string             `toml:"shell"` // srun | ssh
	HidePartitions []string           `toml:"hide_partitions"`
	ClusterName    string             `toml:"cluster_name"`
	StorageWarn    int                `toml:"storage_warn"`   // percent of a quota that warns
	StorageCrit    int                `toml:"storage_crit"`   // percent that is critical
	WarnTimeLeft   string             `toml:"warn_time_left"` // off | 5m | 10m | 30m | 1h
	WarnWaste      bool               `toml:"warn_waste"`     // tell me about running jobs that sit idle
	GPUNames       map[string]string  `toml:"gpu_names"`
	Storage        []StorageEntry     `toml:"storage"`
	Profiles       map[string]Profile `toml:"profiles"`
}

// Profile selects another cluster reachable from this login node
// ("sdash --profile NAME"): its slurm.conf and a display name.
type Profile struct {
	SlurmConf   string `toml:"slurm_conf"`
	ClusterName string `toml:"cluster_name"`
}

// StorageEntry is one [[storage]] location. When the file has no
// [[storage]] entries, sdash detects locations automatically.
type StorageEntry struct {
	Label   string `toml:"label"`
	Path    string `toml:"path"`    // expanded ($VAR, ${VAR}, ~) and absolute after Load
	Backend string `toml:"backend"` // auto | lustre | gpfs | beegfs | quota | statfs | command
	Format  string `toml:"format"`  // raw | json, for backend "command"
	Note    string `toml:"note"`

	// RawCommand is the command as written: an array (argv) or a string
	// (run with sh -c). Load normalises it into Command.
	RawCommand any `toml:"command"`
	// Command is the normalised form of RawCommand.
	Command CommandSpec `toml:"-"`
}

// CommandSpec is a user-configured command: either an argv (preferred) or
// a shell string that runs via sh -c.
type CommandSpec struct {
	Argv  []string
	Shell string
}

// IsZero reports whether no command is set.
func (c CommandSpec) IsZero() bool { return len(c.Argv) == 0 && c.Shell == "" }

// Refresh speeds.
const (
	RefreshFast   = "fast"
	RefreshNormal = "normal"
	RefreshSlow   = "slow"
	RefreshManual = "manual"
)

// Allowed values, shared by validation, the Settings screen and the docs.
var (
	Tabs     = model.TabNames()
	Speeds   = []string{RefreshFast, RefreshNormal, RefreshSlow, RefreshManual}
	Themes   = []string{"auto", "light", "dark", "high-contrast"}
	Shells   = []string{"srun", "ssh"}
	Backends = []string{"auto", "lustre", "gpfs", "beegfs", "quota", "statfs", "command"}
	Formats  = []string{"raw", "json"}
	Layouts  = []string{"clean", "detailed"}
	// TimeLeftChoices are the warning distances before a time limit.
	TimeLeftChoices = []string{"off", "5m", "10m", "30m", "1h"}
)

// Intervals are how often each data source refreshes. With Manual, each
// source loads once and then only when asked (r).
type Intervals struct {
	MyJobs, AllJobs, Cluster, Nodes, QueueRank    time.Duration
	History, Fairshare, Storage, Partitions, Resv time.Duration
	Stats                                         time.Duration // live usage of long-running jobs
	Manual                                        bool
}

// speeds are the intervals of each refresh speed. normal is the default;
// slow makes roughly a third of the queries, fast about twice as many.
var speeds = map[string]Intervals{
	RefreshFast: {
		MyJobs: 5 * time.Second, AllJobs: 15 * time.Second, Cluster: 15 * time.Second, Nodes: 15 * time.Second, QueueRank: 30 * time.Second,
		History: 2 * time.Minute, Fairshare: 5 * time.Minute, Storage: 5 * time.Minute, Partitions: 2 * time.Minute, Resv: 5 * time.Minute, Stats: 5 * time.Minute,
	},
	RefreshNormal: {
		MyJobs: 10 * time.Second, AllJobs: 30 * time.Second, Cluster: 30 * time.Second, Nodes: 30 * time.Second, QueueRank: time.Minute,
		History: 5 * time.Minute, Fairshare: 5 * time.Minute, Storage: 10 * time.Minute, Partitions: 5 * time.Minute, Resv: 5 * time.Minute, Stats: 5 * time.Minute,
	},
	RefreshSlow: {
		MyJobs: 30 * time.Second, AllJobs: 2 * time.Minute, Cluster: 2 * time.Minute, Nodes: 2 * time.Minute, QueueRank: 5 * time.Minute,
		History: 15 * time.Minute, Fairshare: 15 * time.Minute, Storage: 30 * time.Minute, Partitions: 15 * time.Minute, Resv: 15 * time.Minute, Stats: 15 * time.Minute,
	},
}

// SpeedIntervals returns the intervals of a refresh speed; an unknown
// speed is normal. Manual has normal's intervals and Manual set.
func SpeedIntervals(speed string) Intervals {
	if speed == RefreshManual {
		iv := speeds[RefreshNormal]
		iv.Manual = true
		return iv
	}
	if iv, ok := speeds[speed]; ok {
		return iv
	}
	return speeds[RefreshNormal]
}

// Intervals returns the intervals of the configured refresh speed.
func (c Config) Intervals() Intervals { return SpeedIntervals(c.Refresh) }

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Refresh:        RefreshNormal,
		StartTab:       "overview",
		Theme:          "auto",
		Layout:         "clean",
		Mouse:          true,
		Notify:         true,
		Shell:          "srun",
		HidePartitions: []string{},
		StorageWarn:    90,
		StorageCrit:    97,
		WarnTimeLeft:   "10m",
		WarnWaste:      true,
		GPUNames:       map[string]string{},
		Storage:        []StorageEntry{},
	}
}

// TimeLeftWarn is how long before its time limit a running job is warned
// about; negative means never.
func (c Config) TimeLeftWarn() time.Duration {
	if c.WarnTimeLeft == "off" {
		return -1
	}
	d, err := time.ParseDuration(c.WarnTimeLeft)
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}

// Detailed reports layout = "detailed": the tables show their extra columns
// and summary lines.
func (c Config) Detailed() bool { return c.Layout == "detailed" }
