package parse

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// Version parses "sinfo --version" ("slurm 23.11.4" or "slurm-wlm 23.11.4" on Debian).
func Version(raw []byte) (model.Capabilities, error) {
	s := strings.TrimSpace(string(raw))
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return model.Capabilities{}, fmt.Errorf("sinfo --version: no version in %q", s)
	}
	c := model.Capabilities{Version: m[0]}
	c.Major, _ = strconv.Atoi(m[1])
	c.Minor, _ = strconv.Atoi(m[2])
	c.Patch, _ = strconv.Atoi(m[3])
	c.HasMe = c.AtLeast(20, 11)
	c.HasOverlap = c.AtLeast(20, 11)
	return c, nil
}

// ClusterName reads the "ClusterName = x" line of "scontrol show config".
func ClusterName(raw []byte) (string, error) {
	c, err := ClusterConfig(raw)
	return c.Name, err
}

// ClusterInfo is what sdash reads from "scontrol show config".
type ClusterInfo struct {
	Name string
	// PrivateData lists what the site hides, lower case ("jobs", "usage", ...).
	PrivateData []string
	// Settings holds the raw value of each present siteSettings line.
	Settings map[string]string
}

// siteSettings are the config lines that say what the cluster can do.
var siteSettings = []string{"AccountingStorageType", "AccountingStoreFlags", "PriorityType", "JobAcctGatherType"}

// HidesJobs reports whether other users' jobs are hidden.
func (c ClusterInfo) HidesJobs() bool { return slices.Contains(c.PrivateData, "jobs") }

// The methods below are true only when the config positively says a feature is absent, so unreadable config hides nothing.

// AccountingOff reports AccountingStorageType=accounting_storage/none.
func (c ClusterInfo) AccountingOff() bool {
	v, ok := c.Settings["AccountingStorageType"]
	return ok && (strings.HasSuffix(v, "/none") || v == "none")
}

// NoJobScripts reports AccountingStoreFlags lacking job_script.
func (c ClusterInfo) NoJobScripts() bool {
	v, ok := c.Settings["AccountingStoreFlags"]
	return ok && !strings.Contains(strings.ToLower(v), "job_script")
}

// BasicPriority reports PriorityType=priority/basic.
func (c ClusterInfo) BasicPriority() bool {
	v, ok := c.Settings["PriorityType"]
	return ok && strings.HasSuffix(v, "/basic")
}

// NoUsageGather reports JobAcctGatherType=jobacct_gather/none.
func (c ClusterInfo) NoUsageGather() bool {
	v, ok := c.Settings["JobAcctGatherType"]
	return ok && (strings.HasSuffix(v, "/none") || v == "none")
}

// ClusterConfig parses "scontrol show config".
func ClusterConfig(raw []byte) (ClusterInfo, error) {
	var c ClusterInfo
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = textsafe.Field(strings.TrimSpace(v))
		switch strings.TrimSpace(k) {
		case "ClusterName":
			c.Name = v
		case "PrivateData":
			for _, p := range strings.Split(strings.ToLower(v), ",") {
				if p = strings.TrimSpace(p); p != "" && p != "none" && p != "(null)" {
					c.PrivateData = append(c.PrivateData, p)
				}
			}
		default:
			if key := strings.TrimSpace(k); slices.Contains(siteSettings, key) {
				if c.Settings == nil {
					c.Settings = map[string]string{}
				}
				c.Settings[key] = v
			}
		}
	}
	if c.Name == "" {
		return c, fmt.Errorf("scontrol show config: no ClusterName")
	}
	return c, nil
}

// Estimate is the result of "sbatch --test-only".
type Estimate struct {
	JobID     string
	Start     time.Time
	Procs     int
	Nodes     string
	Partition string
}

var testOnlyRe = regexp.MustCompile(`Job (\d+) to start at (\S+) using (\d+) processors on nodes (\S+) in partition (\S+)`)

// TestOnly parses the stderr of "sbatch --test-only", e.g.
//
//	sbatch: Job 23 to start at 2026-09-28T13:38:41 using 1 processors on nodes node01 in partition gpu
func TestOnly(stderr []byte) (Estimate, error) {
	m := testOnlyRe.FindStringSubmatch(string(stderr))
	if m == nil {
		return Estimate{}, fmt.Errorf("sbatch --test-only: %s", strings.TrimSpace(firstLine(stderr)))
	}
	start, err := units.ParseTimestamp(m[2], nil)
	if err != nil {
		return Estimate{}, err
	}
	procs, _ := strconv.Atoi(m[3])
	return Estimate{JobID: m[1], Start: start, Procs: procs, Nodes: m[4], Partition: m[5]}, nil
}

// Parsable parses "sbatch --parsable" output: "<jobid>" or "<jobid>;<cluster>".
func Parsable(stdout []byte) (jobID, cluster string, err error) {
	s := strings.TrimSpace(string(stdout))
	jobID, cluster, _ = strings.Cut(s, ";")
	if _, err := strconv.ParseUint(jobID, 10, 64); err != nil {
		return "", "", fmt.Errorf("sbatch --parsable: unexpected output %q", s)
	}
	return jobID, cluster, nil
}

func firstLine(b []byte) string {
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return "no output"
}
