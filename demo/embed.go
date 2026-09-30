// Package demo holds the scripted scenarios behind "sdash --demo": fictional
// clusters used for trying sdash, snapshot tests and the README animation.
// default.json is a small GPU cluster; hetero.json mixes CPU-only nodes,
// several GPU models and a MIG node.
package demo

import "embed"

// Files holds the scenarios, one JSON file each.
//
//go:embed *.json
var Files embed.FS
