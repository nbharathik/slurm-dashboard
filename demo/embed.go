// Package demo holds the fictional cluster scenarios (JSON) behind "sdash --demo".
package demo

import "embed"

// Files holds the scenarios, one JSON file each.
//
//go:embed *.json
var Files embed.FS
