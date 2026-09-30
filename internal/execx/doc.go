// Package execx is the only gateway through which sdash starts
// subprocesses. Every Slurm, quota and helper command goes through a Runner,
// so the rules that keep sdash safe on a shared cluster live in one place:
//
//   - Commands are argv slices, never shell strings.
//   - Only allowlisted executables run; anything else is refused.
//   - Commands that change cluster state (scancel, scontrol hold, sbatch,
//     srun, ...) are refused unless the context carries an authorisation
//     for that exact argv, created with WithMutation by the actions layer
//     after the user confirmed it.
//   - Test binaries cannot reach real cluster tools unless a runner opts in.
//   - At most a fixed number of commands run at once, each with a timeout,
//     in its own process group that is killed as a whole on timeout, and
//     with capped output.
//   - Child processes get an environment stripped of user overrides that
//     change Slurm's output format.
//
// Use RealRunner in production, FakeRunner in tests and demo mode, and
// RecordingRunner to capture fixtures.
//
// This package is the only one allowed to import os/exec; golangci-lint's
// forbidigo rule enforces that.
package execx
