# Changelog

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and the project uses [Semantic Versioning](https://semver.org/). Command-line
flags, `--json` output (`"schema": 1`) and config keys stay compatible within
0.x; new JSON fields may be added.

## [0.3.0] - 2026-11-20

Lighter on the login node, more accurate numbers, safer defaults.

Update with `sdash update`, or install fresh with
`curl -fsSL https://raw.githubusercontent.com/nbharathik/slurm-dashboard/main/install.sh | sh`.

### Lighter

- Fewer Slurm calls: an open job card refreshes every 30 seconds and its
  live usage every minute; nodes, running jobs and queue ranks refresh every
  2 to 5 minutes on tabs that do not show them.
- Left alone for 2 hours, sdash slows to about one call a minute and frees
  what it can rebuild; your own jobs stay checked often enough for job-end
  notices while you have jobs and notices are on. The first key press
  catches up, spread over a few seconds.
- Less memory: everyone's jobs are freed 2 minutes after leaving the Queue
  tab, repeated names are stored once, command output is not copied, and
  the Queue no longer copies the job list. sdash uses at most 4 threads and
  a 512 MiB soft memory ceiling unless `GOMAXPROCS`, `GOGC` or `GOMEMLIMIT`
  is set.
- The clock ticks every 5 seconds when the terminal is unfocused and every
  30 seconds when idle; alerts are recomputed once a minute, not every second.
- No slow growth over days: the demo no longer keeps every command it ran,
  and lists of ended jobs, unfolded arrays and hidden alerts are bounded.
- `ctrl+d` shows memory in use; `sdash doctor` estimates the Slurm calls a
  minute at your refresh setting.

### Added

- An optional Scheduler tab (key `7`, `scheduler_tab` in Settings): main
  and backfill cycle times, how deep backfill looks, controller load and
  the busiest requests and users, from `sdiag`. Off by default; it calls
  `sdiag` every 2 minutes only while open.
- A pending job that can never start as requested (time limit above the
  partition's, more nodes than it has, more per node than its largest node)
  is flagged in `why`, `sdash why` and Needs attention.
- `why` says how long your jobs in that partition usually waited.
- `--csv` on `sdash jobs`, `queue` and `history`; `:export csv` and
  `--csv` neutralise cells that a spreadsheet would read as a formula.
- `sdash doctor --report`: one anonymised block to paste into an issue.
- `n` on a running job opens its node; a multi-node job's card lists its
  nodes, and a node's card links to its jobs (click).
- An array task's card sums up the whole array: running, pending, done and
  failed tasks, naming the failed ones.
- The history card lists the job's steps (state, exit code, time, peak
  memory), so you can see which step failed.
- Batch scripts sdash fetched are kept privately for 30 days, so rerun
  still works after Slurm has forgotten the script.
- Docs: hook recipes (mail, Slack, failures only) and a page for
  administrators (calls a minute, what sdash runs, site install, module).

### Security

- A job name starting with `=`, `+`, `-` or `@` no longer runs as a
  formula when an export is opened in a spreadsheet.
- The submit-line parser no longer splits a multi-byte character after a
  dash (found by fuzzing).
- A `$VISUAL` of only spaces no longer makes `:config` or the rerun editor
  run the file itself; `$EDITOR`, then `vi`, is used.

### Changed

- Needs attention uses spare room for more alerts and says how many are
  hidden ("2 more").
- Help opens with the keys of the tab you are on.
- In the command line, Enter runs the highlighted suggestion when you moved
  to it, or when what you typed is only its beginning.

### Fixed

- GPU and CPU counts for arrays: a pending range such as `815_[3-9]` counts
  all its tasks in its row, a folded array, group headings, the Queue
  summary, the Overview and `sdash queue`, so folded and unfolded totals
  match. Pending counts on the Overview count tasks too. `--json` and
  `--csv` rows of `jobs` and `queue` gain `tasks`, `total_cpus` and
  `total_gpus` (`cpus` and `gpus` stay per task), and the JSON has a
  `totals` block. Stepped arrays (`--array=0-98:2`) count every task.
- `sdash queue` uses what running jobs were allocated, as the Queue tab
  does, so `--gpus` jobs show their GPUs there too.
- Running jobs that asked with `--gpus` show their GPUs in the Queue and
  match the `GPU jobs` filter, when running-job data is loaded.
- `--mem-per-cpu` jobs: running jobs and the job card show the real memory
  per node (from the allocation and the TRES totals), not the per-CPU
  value; the idle-memory warning uses it too.
- Live memory of a multi-task job is the total of its tasks, as the
  request and the history are, not its largest task.
- On clusters with tens of thousands of running jobs the running-jobs
  query no longer runs into the output limit: padding is dropped as it
  arrives.
- An unterminated escape sequence in a log hides at most the rest of its
  line, not the rest of the log.
- Two sdash instances sharing a debug log no longer rotate it twice.
- Opening one task of an array (for example from the Overview) no longer
  lets cancel or logs act on the whole array or another task.
- `:range` changes the Usage range; `/` filters the Usage table.
- Right-size suggestions no longer give `--mem` or `--cpus-per-task` values
  for jobs on several nodes, where they could raise the request.
- The priority line adds up: factors sdash does not read show as "other".
- Start estimates are kept when the pending reason has spaces.
- Queue rank for jobs submitted to several partitions.
- A pending array that starts some tasks is no longer reported as ended.
- Reserved, planned and rebooting nodes are not counted as free;
  `sdash free --nodes N` needs N fitting nodes. They are not called down:
  "down" counts only down, drained, failing or not responding nodes, and
  partitions show "N reserved" for nodes held for a waiting job, a
  reservation or maintenance (JSON: `nodes_reserved`). `sdash free` says
  when the only nodes that fit are held.
- Under Fair Tree, the fairshare verdict compares usage with shares.
- A capability probe that failed because slurmdbd was down is retried after
  10 minutes instead of a day.
- Late log or script lookups no longer open over another job or tab; paste
  works in the command line, filters, log search and the rerun form; `esc`
  while editing a filter keeps the filter applied before; long info boxes
  scroll; a card whose job left the queue closes.

### Safer

- `sdash update` refuses a release without a signature when cosign or gh is
  installed, as `install.sh` does; `--allow-unsigned` overrides.
- A config file others can write also loses `[profiles]`.
- One hung filesystem no longer makes every storage location unavailable.
- Job-end hooks for many jobs ending at once all run.
- State, cache and logs work in group-writable homes; only sdash's own
  directories must be private.
- Settings saves never leave the config missing.
- Hold and release ask first for several jobs; new setting `confirm`.

### New

- `sdash wait JOBID...`: wait until jobs end; exits 0 only if all completed,
  never when stopped. It also waits for one task of a pending array.
- Rerun of a job that ran out of memory or time offers half as much again.
- `:node` jumps from a job to its node; `:export csv|json` saves a table.

## [0.2.0] - 2026-10-17

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

[0.3.0]: https://github.com/nbharathik/slurm-dashboard/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/nbharathik/slurm-dashboard/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/nbharathik/slurm-dashboard/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/nbharathik/slurm-dashboard/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/nbharathik/slurm-dashboard/releases/tag/v0.1.0
