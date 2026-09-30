// Package meta holds build-time identity for sdash: the application name
// used for paths and messages, and the version, commit and build date that
// release builds inject with -ldflags "-X".
//
// AppName is the single place the name "sdash" is defined. Renaming
// the project means changing AppName here and project_name in
// .goreleaser.yaml.
package meta
