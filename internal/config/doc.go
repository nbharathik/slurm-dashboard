// Package config loads, validates and writes sdash's TOML configuration
// and resolves its XDG directories.
//
// Loading never prevents sdash from starting. A missing file yields the
// defaults. An unknown key is a warning. An invalid value is an error that
// names its line and resets only that key (or storage entry) to its
// default. A file that is not valid TOML is reported with its line and
// column and replaced by the defaults as a whole.
package config
