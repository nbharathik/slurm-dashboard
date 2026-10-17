# Changelog

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and the project uses [Semantic Versioning](https://semver.org/). Command-line
flags, `--json` output (`"schema": 1`) and config keys stay compatible within
0.x; new JSON fields may be added.

## [0.2.0] - unreleased

Tested on six Slurm versions, works on more clusters, installs more reliably.

### Tested on every Slurm

- GitHub Actions builds throwaway Slurm clusters for 21.08, 22.05, 23.11,
  24.11, 25.11 and 26.05 every week and on every change to the Slurm layer.
  Each run checks every `--json` command, the dashboard at three sizes, the
  integration tests and job output latency. See
  [compatibility](https://github.com/nbharathik/slurm-dashboard/blob/main/docs/compatibility.md).
- The output of each version is recorded in `testdata/fixtures/` and checked
  on every push.

### Works on more clusters

- Partition access: `sdash free` and the Overview mark a partition closed to
  you when a `DenyAccounts` list holds all your accounts, and `free` says when
  it could not check your access. JSON: `partitions_denied` and
  `partitions_unverified`.
- Controller rate limiting (Slurm 23.02 and newer): sdash recognises the
  error, waits at least 30 seconds before asking again and says "controller is
  rate-limiting; slowing down".
- Start estimates work with the `sbatch --test-only` wording of Slurm 25.11
  and newer.
- Slurm 25.11.2 prints CPU time it never measured; live usage now shows it as
  unknown instead of calling every running job idle.
- Rerun works on Slurm before 23.02: it uses the script's `#SBATCH` lines and
  the job's working directory.
- `sdash record` names its folders by version (`22.05`, not `22.5`).

### Install and update

- `install.sh` and `sdash update` find the latest version through the
  `releases/latest` redirect, which has no API rate limit; the GitHub API is
  the fallback. A rate-limited install suggests `--version vX.Y.Z`.
- The installer prints the PATH line for csh and tcsh too.
- Tested on Ubuntu 22.04 and 24.04, Debian 12, Rocky Linux 8 and 9 and
  Alpine, with bash, dash and busybox sh, and with only curl or only wget.
- Tags such as `v0.2.0-rc.1` are published as pre-releases; the installer and
  `sdash update` keep serving the latest final release.

### Other

- The demo shows the capacity of shared filesystems on the Storage tab.
- The rerun form keeps its suggestions whole on narrow terminals.
- Bubble Tea 2.1.0 and other dependency updates.
- New README, screenshots, contributing guide, issue templates and a manual
  check list for new clusters.

## [0.1.2] - 2026-10-07

- Release process fixes.

## [0.1.1] - 2026-10-07

- Fixed the release build's dependency declaration.

## [0.1.0] - 2026-10-07

The first release.

### Dashboard

- Six tabs: Overview (what needs attention, capacity per partition, your
  jobs, storage and fairshare), Jobs (yours), Queue (everyone's, grouped,
  with start estimates), Nodes (every node by partition, GPUs by type with MIG
  slices apart, memory or "not tracked"), Usage (what your jobs used and
  wasted, your limits and fairshare, and the finished jobs) and Storage
  (quotas, a 30-day trend, largest directories).
- Why a job is pending, in plain words, with its place in the queue and its
  priority factors.
- Job actions (cancel, hold, release, requeue) only on your own jobs; cancel
  and requeue show the exact command first. Rerun a finished job with
  editable resources and right-size suggestions. Live logs, a shell in a
  job, GPU sampling.
- `/` filters tables, `:` runs commands; mouse support; light, dark,
  high-contrast, `NO_COLOR` and ASCII; job-end notifications.
- A Settings screen (`,`) that saves to your config file and keeps your
  comments. A clean layout by default, `layout = "detailed"` for more columns.
- One `refresh` setting: `fast`, `normal`, `slow` or `manual`. Never faster
  than every 5 seconds, at most 3 commands at once, slower for hidden tabs,
  `p` to pause, calls per minute in `ctrl+d`.

### Usage and warnings

- The Usage tab sums up 1, 7 or 30 days: outcomes, CPU and GPU hours, idle
  CPU, memory and GPU time, the account and QOS limits closest to being hit,
  your fairshare and the least efficient jobs.
- The Storage tab remembers how full each location was and forecasts when it
  will be full.
- Warnings for jobs close to their time limit (`warn_time_left`) and for jobs
  that use little of what they asked for (`warn_waste`).

### Command line

- `status [--watch]`, `jobs`, `queue`, `nodes`, `free`, `usage`,
  `history [JOBID]`, `why`, `logs`, `quota`, `doctor`, `config`, `update`;
  `--json` everywhere (schema 1); shell completion.
- `--demo[=default|hetero|basic]`, `--profile NAME`, `--config PATH`.

### Works on any cluster

- Needs no config. Asks Slurm what the cluster offers (accounting, stored job
  scripts, priority plugin, job usage gathering) and shows a one-line reason
  for what is missing; `sdash doctor` lists each feature.
- Short GPU names, MIG slices, CPU-only and login nodes, untracked memory,
  clusters without GPUs, accounting or visibility of others' jobs.
- Site defaults in `/etc/sdash/config.toml`, the user's file on top.

### Security

- One command gateway with an allowlist, no shell, timeouts and a mutation
  guard; terminal escapes and bidi characters removed from all Slurm text;
  config files others can change cannot run commands.
- Signed releases (cosign), SBOMs, build provenance, pinned GitHub Actions;
  `install.sh` and `sdash update` check checksums and signatures.

[0.2.0]: https://github.com/nbharathik/slurm-dashboard/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/nbharathik/slurm-dashboard/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/nbharathik/slurm-dashboard/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/nbharathik/slurm-dashboard/releases/tag/v0.1.0
