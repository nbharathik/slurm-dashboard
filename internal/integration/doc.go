// Package integration holds end-to-end tests that run against a real,
// throwaway Slurm cluster named sdashtest. They are compiled only with the
// "integration" build tag and run
// only when SDASH_INTEGRATION=local is set. They create jobs named
// sdash-test-* and cancel them afterwards; never point them at a shared
// cluster.
package integration
