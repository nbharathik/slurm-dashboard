// Package report builds the stable, versioned JSON documents behind every
// "--json" flag, and renders the same data as plain text
// tables for the CLI. The JSON shapes are a public interface: fields may be
// added, but never renamed or removed without bumping Schema.
package report
