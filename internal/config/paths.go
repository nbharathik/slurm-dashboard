package config

import (
	"errors"
	"path/filepath"

	"github.com/nbharathik/slurm-dashboard/internal/meta"
)

// Paths are the directories and files sdash uses. They follow the XDG
// base directory specification and never need root.
type Paths struct {
	ConfigDir  string // $XDG_CONFIG_HOME/sdash or ~/.config/sdash
	ConfigFile string // ConfigDir/config.toml
	StateDir   string // $XDG_STATE_HOME/sdash or ~/.local/state/sdash
	CacheDir   string // $XDG_CACHE_HOME/sdash or ~/.cache/sdash
}

// ResolvePaths computes Paths from the environment. It reads $HOME and the
// XDG variables only; it never looks users up in the password database,
// which cannot see LDAP accounts in a static binary. Relative XDG values
// are ignored, as the XDG specification requires.
func ResolvePaths(getenv func(string) string) (Paths, error) {
	home := getenv("HOME")
	base := func(xdgVar, fallback string) (string, error) {
		if v := getenv(xdgVar); filepath.IsAbs(v) {
			return filepath.Join(v, meta.AppName), nil
		}
		if !filepath.IsAbs(home) {
			return "", errors.New("$HOME is not set to an absolute path; set $HOME or $" + xdgVar)
		}
		return filepath.Join(home, fallback, meta.AppName), nil
	}

	configDir, err := base("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return Paths{}, err
	}
	stateDir, err := base("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return Paths{}, err
	}
	cacheDir, err := base("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		ConfigDir:  configDir,
		ConfigFile: filepath.Join(configDir, "config.toml"),
		StateDir:   stateDir,
		CacheDir:   cacheDir,
	}, nil
}
