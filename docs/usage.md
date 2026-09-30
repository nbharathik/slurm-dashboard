# Using sdash

```sh
sdash            # open the dashboard
sdash --demo     # try it on a simulated cluster (--demo=hetero, --demo=basic)
```

Press `?` for every key, `,` for settings, `q` to quit. Each tab is one
table with a one-line status above it; `enter` opens the details of a row.
`:` opens a command line with completion.

## The screen

```
  sdash │ 1 Overview  2 Jobs  3 Queue  4 Nodes  5 Usage  6 Storage      mycluster · you
  ─────────────────────────────────────━━━━━━━━━━━━──────────────────────────────────────
  (status line: counts, then only what is not the plain view)
    NODE          STATE   CPU FREE   MEM FREE   GPU FREE  GPU TYPE
  ▶ ▾ gpu (2)     up       256/256  1008G/1008G       8/8  H200
      node101     idle     128/128   504G/504G        4/4  H200
  ─────────────────────────────────────────────────────────────────────────────────────────
  enter details · / filter · , settings · ? help                        updated 3s ago
```

The top bar names the app and lists every tab; the heavy stretch of the rule
under it marks the one that is open. The footer shows a few keys for the
tab, then `, settings` and `? help`, and at the right when the data was last
refreshed (yellow when stale, red when it failed). A partition on Nodes, or
a group on Queue, is a bold row whose numbers are the totals of the rows
indented under it; `enter` or `→` `←` fold it.

## The six tabs

| Key | Tab | What it shows |
| --- | --- | --- |
| `1` | Overview | What needs attention, free CPUs and GPUs per partition, your jobs, storage and fairshare |
| `2` | Jobs | Your jobs: details, live usage, why pending, logs, actions |
| `3` | Queue | Everyone's jobs, grouped by state, user or partition |
| `4` | Nodes | Every node by partition: free CPUs, memory and GPUs (MIG too), who runs there |
| `5` | Usage | What your jobs used and wasted, your limits and fairshare, then your finished jobs (`:tab history` still works) |
| `6` | Storage | Quotas per location; `a` finds the largest directories |

## Keys

| Key | Does |
| --- | --- |
| `,` | Settings: refresh speed, theme, hidden partitions, ... (saved for you) |
| `:` | Command line (`:cancel 812`, `:filter state:PD`, `:help`) |
| `/` | Filter the table (Jobs, Queue, Nodes, Usage); the command line elsewhere |
| `r` / `R` | Refresh this tab / everything |
| `p` | Pause refreshing (`r` still works) |
| `esc` | Back, close, clear the filter |
| `ctrl+d` | Debug: every Slurm command run, and calls per minute |

In tables: `j` `k` `↑` `↓` move, `enter` opens details, `space` selects,
`s` sorts by the next column and `S` reverses (the same on every tab), `g`
groups (Nodes by partition; Queue by state, user or partition), `→` / `←`
open or close a group, `y` copies the job ID. On Nodes `G` shows all, GPU or
CPU-only nodes; on Queue `v` shows all jobs, then only running, pending, held,
yours or GPU jobs, and `e` estimates when a pending job will start. Your
grouping, sort, node kind and usage range are remembered.

## Clean or detailed

Every tab shows the essentials by default (`layout = "clean"`): free/total
numbers in columns that line up, one status line, no boxes. Choose
`layout = "detailed"` on the Settings screen (`,`) to bring back what was
left out: load, free-in and the Partitions panel on Nodes; the summary lines
on Queue; extra columns on Jobs, Usage and Storage; the Priority and
Wasteful lines on Usage. Opening a row (`enter`) always shows everything
about it.

## Your jobs

| Key | Does |
| --- | --- |
| `c` | Cancel (asks first and shows the exact command) |
| `h` / `u` | Hold / release |
| `Q` | Requeue |
| `w` | Why is it pending? |
| `e` (Queue) | When will it start? |
| `l` / `e` / `o` | Output log / error log / open in `$PAGER` |
| `t` | Shell inside the running job |
| `g` | Sample its GPU use once |
| `v` | Batch script |
| `m` | Menu of every action |
| `n` (Usage) | Run a finished job again, with changes and right-size suggestions |

sdash only acts on your own jobs, and only after showing the command it
will run.

### Running a job again

`n` on a job in Usage (or `:rerun <job>`) opens a form with the job's
partition, CPUs, memory, time and GPUs, the right-size suggestions next to
them (`a` uses them all) and a preview of the stored batch script. `s` asks
`sbatch --test-only` when it would start, `e` edits a private copy of the
script, and `r` shows the exact `sbatch --parsable ...` command before
anything runs.

It needs the job's script: Slurm keeps it for a few minutes after the job
ends, or for good where `AccountingStoreFlags` includes `job_script`. From
Slurm 23.02 it also reads the options the job was submitted with. Slurm
stores that line without quotes, so an option with a space in its value
(a job name, a comment) is listed as "not read" instead of guessed. Only
resource, account and output options are carried over, each checked first.

## Your usage

Tab 5 (Usage) sums up your finished jobs over the last 1, 7 or 30 days
(`[` and `]` change the range) above the table of those jobs:

| Line | Says |
| --- | --- |
| Jobs | How many finished, how many failed and the most common way, CPU and GPU hours |
| Idle | CPU, memory and GPU time your completed jobs asked for and did not use |
| Limits | The account and QOS limits closest to being hit, e.g. `CPUs in use 60 of 64 (94%)` |
| Priority | Your fairshare, and whether it helps or holds you back |
| Wasteful | The completed jobs that wasted the most; the IDLE column sorts them (`:sort waste desc`) |

Lines a cluster cannot fill say why in one line (no accounting, limits
hidden by the site, `priority/basic`). The panel shrinks on small
terminals. `sdash usage [--days N] [--json]` prints the same, and `n` on
a wasteful job reruns it with the right-size suggestions.

Idle time counts completed jobs only. It compares CPU time with what the
job held, the peak memory with what it asked for, and GPU utilisation when
the site records it (`gres/gpuutil`). Memory peaks are estimates.

## Storage over time

The Storage tab draws the last 30 days of each location and, when use is
rising towards a limit, says when it will be reached (`full ~19d`). It
needs at least four readings over a day and sees nothing while sdash is not
running: readings (at most one an hour per location, kept 90 days) go to
`~/.local/state/sdash/storage.jsonl`. Delete that file to start over.

## Warnings

| Setting | Does |
| --- | --- |
| `warn_time_left` (default `10m`) | A running job close to its time limit raises `812 train has 9m left: checkpoint or save now`. `off` turns it off |
| `warn_waste` (default on) | After 30 minutes, a job that uses under 20% of its CPUs, 10% of its memory or 20% of its GPU time raises a note to ask for less next time |

Each warning appears on the Overview and is announced once with the
message line, the bell and a desktop notice (if `notify` is on). The idle
check costs one `sstat` call per 20 running jobs every 5 minutes, only
while you have jobs older than 25 minutes. It skips array tasks, which
`sstat` cannot tell apart, and clusters without job accounting gather.

## Filters

Terms are ANDed. Jobs, Queue and Usage: `state:R` `state:PD,H`
`part:gpu` `user:alice` `gpu:>0` `name:~^train` `id:812`, or a word matched
against the name. Nodes: `state:idle` `state:drain` `part:gpu` `feat:ib`
`gpu:h200` `gpu:>0` (free) `cpu:>=16` (free), or part of a node name or GPU
type.

## On the command line

Every data command prints a table and takes `--json` (`"schema": 1`).

| Command | Does |
| --- | --- |
| `sdash status [--watch]` | One-screen summary, or kept up to date |
| `sdash jobs` / `sdash queue` | Your jobs / everyone's (`--group-by user`, `-p`, `--state PD`) |
| `sdash nodes [NAME]` | Every node, or one in detail (`--gpu`, `--cpu`, `--state drained`, `--partitions`) |
| `sdash free --gpus 1 --gpu-type h200` | Where a job fits now, or soonest (`--test` asks the scheduler) |
| `sdash usage [--days 30]` | What your jobs used and wasted, limits and fairshare |
| `sdash history [JOBID] [--days 30]` | Finished jobs with efficiency, or one job's card |
| `sdash why ID` | Why a job is pending |
| `sdash logs ID [-f] [--err]` | Print or follow a job's log |
| `sdash quota` | Storage usage and quotas |
| `sdash config` | Your settings and where each comes from (`sdash config edit`) |
| `sdash doctor` | Check Slurm, accounting, PATH, terminal, config, and what this cluster offers |
| `sdash update [--check]` | Update (SHA-256; optional signature/provenance check) |

`--profile NAME` runs against another cluster from the same login node.
`--demo[=NAME]` uses a simulated one; write the `=`.

## If your cluster lacks something

sdash asks Slurm what the cluster offers and shows a one-line reason in
place of what is missing, never an error. `sdash doctor` lists each
feature:

| Missing | What you see |
| --- | --- |
| Accounting (slurmdbd) | Usage says there is no job history, limits or fairshare |
| Account and QOS limits hidden | Usage says the site does not show them |
| Stored job scripts | Rerun works only for jobs Slurm still remembers |
| `priority/multifactor` | No fairshare or priority numbers |
| Job usage gathering | No live CPU and memory use of running jobs, no idle-job warning |
| GPUs | No GPU columns or panels |
| `PrivateData=jobs` | The Queue tab says other users' jobs are hidden |

## Troubleshooting

- **`sdash: command not found`**: see [install.md](install.md).
- **Something looks wrong**: `sdash doctor` checks Slurm, the controller,
  your terminal and the config.
- **Odd symbols or colours**: press `,` and turn on "Plain symbols", or
  pick another theme.
- **A config mistake**: `sdash config` shows the line; sdash still starts
  with defaults for bad values.
- **Too many Slurm queries**: press `,` and set Refresh to `slow` or
  `manual`. `ctrl+d` shows the calls per minute.
- **A crash**: sdash saves a report in `~/.cache/sdash/`; review it for sensitive data
  before attaching it to an issue.
