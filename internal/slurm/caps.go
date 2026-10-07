package slurm

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/privatefile"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// CacheTTL is how long probed capabilities and the cluster name are reused.
const CacheTTL = 24 * time.Hour

// Probe determines the Slurm version and which optional features work (cheap queries that fail when missing).
func Probe(ctx context.Context, r execx.Runner, user string) (model.Capabilities, error) {
	res, err := r.Run(execx.WithLabel(ctx, "version"), Version()...)
	if err != nil {
		return model.Capabilities{}, err
	}
	caps, err := parse.Version(res.Stdout)
	if err != nil {
		return caps, err
	}
	probeFeatures(ctx, r, user, &caps)
	return caps, nil
}

func probeFeatures(ctx context.Context, r execx.Runner, user string, caps *model.Capabilities) {
	ok := func(label string, argv ...string) bool {
		_, err := r.Run(execx.WithLabel(ctx, label), argv...)
		return err == nil
	}
	caps.HasSacct = ok("probe-sacct", "sacct", "-n", "-X", "-S", "now-1minutes", "-E", "now", "-o", "JobID")
	caps.HasSshare = ok("probe-sshare", "sshare", "-U", "-n", "-P", "-o", "User")
	caps.HasSprio = ok("probe-sprio", "sprio", "-h", "-u", user, "-o", "%i")
	caps.HasSstat = ok("probe-sstat", "sstat", "--version")
}

type capsCache struct {
	Version  string             `json:"version"`
	ProbedAt time.Time          `json:"probed_at"`
	Caps     model.Capabilities `json:"caps"`
}

// CachedProbe is Probe with a 24-hour cache in cacheDir, keyed by Slurm version.
func CachedProbe(ctx context.Context, r execx.Runner, user, cacheDir string, now time.Time) (model.Capabilities, error) {
	res, err := r.Run(execx.WithLabel(ctx, "version"), Version()...)
	if err != nil {
		return model.Capabilities{}, err
	}
	caps, err := parse.Version(res.Stdout)
	if err != nil {
		return caps, err
	}
	path := cachePath(cacheDir, "capabilities.json")
	var c capsCache
	if readJSON(path, &c) == nil && c.Version == caps.Version && now.Sub(c.ProbedAt) < CacheTTL && now.After(c.ProbedAt) {
		return c.Caps, nil
	}
	probeFeatures(ctx, r, user, &caps)
	_ = writeJSON(path, capsCache{Version: caps.Version, ProbedAt: now, Caps: caps})
	return caps, nil
}

// infoCacheVersion goes up when infoCache gains fields, so older caches are refetched.
const infoCacheVersion = 2

type infoCache struct {
	Version     int               `json:"version"`
	Name        string            `json:"name"`
	PrivateData []string          `json:"private_data"`
	Settings    map[string]string `json:"settings,omitempty"`
	SetAt       time.Time         `json:"set_at"`
}

// ClusterInfo reads the cluster name and PrivateData from "scontrol show config", cached 24 hours.
func ClusterInfo(ctx context.Context, r execx.Runner, cacheDir string, now time.Time) (parse.ClusterInfo, error) {
	path := cachePath(cacheDir, "clusterinfo.json")
	var c infoCache
	if readJSON(path, &c) == nil && c.Version == infoCacheVersion && c.Name != "" && now.Sub(c.SetAt) < CacheTTL && now.After(c.SetAt) {
		return parse.ClusterInfo{Name: c.Name, PrivateData: c.PrivateData, Settings: c.Settings}, nil
	}
	res, err := r.Run(execx.WithLabel(ctx, "clusterinfo"), Config()...)
	if err != nil {
		return parse.ClusterInfo{}, err
	}
	info, err := parse.ClusterConfig(res.Stdout)
	if err != nil {
		return info, err
	}
	_ = writeJSON(path, infoCache{Version: infoCacheVersion, Name: info.Name, PrivateData: info.PrivateData, Settings: info.Settings, SetAt: now})
	return info, nil
}

// cachePath is a file in cacheDir, or "" without one (never touches the working directory).
func cachePath(cacheDir, name string) string {
	if cacheDir == "" {
		return ""
	}
	return filepath.Join(cacheDir, name)
}

func readJSON(path string, v any) error {
	if path == "" {
		return errors.New("no cache directory")
	}
	b, err := privatefile.Read(path, 1<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// writeJSON writes v atomically with mode 0600.
func writeJSON(path string, v any) error {
	if path == "" || filepath.Dir(path) == "." {
		return errors.New("no cache directory")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return privatefile.Write(path, b)
}

// User resolves the operating-system identity, including LDAP/SSSD accounts.
func User(ctx context.Context, _ func(string) string, r execx.Runner) (string, error) {
	res, err := r.Run(execx.WithLabel(ctx, "user"), "id", "-un")
	if err != nil {
		return "", err
	}
	if u := strings.TrimSpace(string(res.Stdout)); u != "" {
		return u, nil
	}
	return "", errors.New("cannot determine the operating-system user: id -un printed nothing")
}
