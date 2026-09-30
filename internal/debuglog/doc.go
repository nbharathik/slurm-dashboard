// Package debuglog writes sdash's debug log and crash reports under the
// cache directory (~/.cache/sdash by default).
//
// The log rotates at 5 MB and keeps three files (debug.log, debug.log.1,
// debug.log.2). Files are created with mode 0600 because they can contain
// job names, paths and usernames. Environment variables are never logged.
// A log that cannot be opened is replaced by a discarding logger: logging
// must never stop sdash from starting.
package debuglog
