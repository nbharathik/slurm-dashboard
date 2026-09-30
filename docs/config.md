# Settings

sdash needs no config file. Press `,` in the dashboard to change the common
settings: they apply at once and are saved to your config file, with your
comments kept. `sdash config` shows every setting, its value and where it
comes from; `sdash config edit` opens the file.

Saving settings refuses unsafe config permissions. Review its commands before
correcting ownership or removing group/other write access.

## The file

`~/.config/sdash/config.toml` (or `$XDG_CONFIG_HOME/sdash/config.toml`,
`$SDASH_CONFIG`, or `--config PATH`):

```toml
refresh         = "slow"
theme           = "dark"
hide_partitions = ["login"]

[gpu_names]
nvidia_h200_nvl = "H200"
```

## Settings

| Key | Default | What it does |
| --- | --- | --- |
| `refresh` | `"normal"` | How often data refreshes: `fast`, `normal`, `slow`, or `manual` (load once, then press `r`) |
| `start_tab` | `"overview"` | The tab sdash opens on: `overview`, `jobs`, `queue`, `nodes`, `usage`, `storage` (`history` still works) |
| `theme` | `"auto"` | `auto`, `dark`, `light`, `high-contrast`; `NO_COLOR` is always honoured |
| `layout` | `"clean"` | `clean` shows the essentials on each tab; `detailed` adds extra columns and summary lines |
| `ascii` | `false` | Plain symbols, for terminals or fonts without Unicode |
| `mouse` | `true` | `false` frees the mouse for selecting text |
| `notify` | `true` | A message, the bell and a desktop notice when one of your jobs ends |
| `shell` | `"srun"` | How `t` opens a shell in a running job: `srun`, or `ssh` where `pam_slurm_adopt` allows it |
| `hide_partitions` | `[]` | Partitions left out of Nodes and the Overview, e.g. `["login"]` |
| `storage_warn` | `90` | Percent of a quota that shows a warning |
| `storage_crit` | `97` | Percent that is critical (above `storage_warn`) |
| `warn_time_left` | `"10m"` | Warn when a running job is this close to its time limit: `off`, `5m`, `10m`, `30m`, `1h` |
| `warn_waste` | `true` | After 30 minutes, note a running job that uses under 20% of its CPUs, 10% of its memory or 20% of its GPUs (one `sstat` call every 5 minutes) |
| `cluster_name` | `""` | The name in the header, instead of Slurm's `ClusterName` |
| `notify_command` | `""` | Also run this when your job ends (see below); only from the file, never from the Settings screen |

How fast each speed is, per data source:

| `refresh` | Your jobs | Queue, nodes, cluster | Usage, fairshare, storage |
| --- | --- | --- | --- |
| `fast` | 5 s | 15 s | 2 to 5 min |
| `normal` | 10 s | 30 s | 5 to 10 min |
| `slow` | 30 s | 2 min | 15 to 30 min |
| `manual` | once at start, then `r` (this tab) or `R` (all) | | |

Nothing refreshes faster than every 5 seconds, at most 3 Slurm commands run
at once, and tabs you are not looking at refresh less. `p` pauses.

## GPU names

sdash shortens GPU types (`nvidia_h200_nvl` shows as `H200 NVL`). Give your
own names in `[gpu_names]`; MIG profiles such as `1g.33gb` work too:

```toml
[gpu_names]
nvidia_h200_nvl = "H200"
"1g.16gb"       = "MIG small"
```

## Storage locations

Without `[[storage]]` entries sdash finds `$HOME`, `$SCRATCH`, `$WORK`,
`$PROJECT`, `$DATA`, `/scratch/$USER` and `/work/$USER`, and picks the quota
tool from the filesystem (Lustre, GPFS, BeeGFS, `quota`, or the filesystem
size). Listing entries replaces that:

```toml
[[storage]]
label = "Scratch"
path  = "/scratch/$USER"
note  = "Files older than 30 days are purged"

[[storage]]
label   = "Project"
backend = "command"                 # auto | lustre | gpfs | beegfs | quota | statfs | command
command = ["myquota", "--json"]     # an array runs as is; a string runs with sh -c
format  = "json"                    # raw, or json with used_bytes, soft_bytes, hard_bytes, used_files, soft_files, hard_files, grace
```

## Job-end command

`notify_command` runs with `sh -c` when one of your jobs ends, with the job
in `SDASH_JOB_ID`, `SDASH_JOB_NAME`, `SDASH_JOB_STATE`, `SDASH_EXIT_CODE` and
`SDASH_ELAPSED` (never pasted into the command, so any job name is safe):

```toml
notify_command = 'curl -s -d "$SDASH_JOB_NAME $SDASH_JOB_STATE" ntfy.sh/my-topic'
```

Notices come only while sdash runs; keep it open in tmux. For desktop
notices inside tmux, add `set -g allow-passthrough on` to `~/.tmux.conf`.

## Other clusters

`sdash --profile other` uses another cluster reachable from the same login
node:

```toml
[profiles.other]
slurm_conf   = "/etc/slurm-other/slurm.conf"
cluster_name = "other"
```

## For administrators

Site defaults go in `/etc/sdash/config.toml`; each user's file is read on
top of it (tables such as `[gpu_names]` merge, other values are replaced).
Good candidates: `[[storage]]`, `[gpu_names]` and `hide_partitions`.

**Commands.** `notify_command` and storage commands run programs as the
user, so sdash takes them only from a file nobody else can change. If the
file is writable by its group or everyone, belongs to another user (root
excepted), or has an unsafe parent directory,
those two are ignored and `sdash config` says so. Check ownership and write
permissions on the file and its parent directories.

## Problems in the file

sdash always starts. A typo is ignored with a "did you mean" hint, an
invalid value falls back to its default, a file that is not valid TOML is
ignored as a whole, and settings from before version 0.1 are named with
what replaced them. `sdash config` lists each problem with its line.

## Files sdash writes

| What | Where |
| --- | --- |
| Settings | your config file (a `.bak` keeps the previous version) |
| State: last tab, dismissed alerts, palette history, and the grouping, sort, node kind and usage range you chose | `~/.local/state/sdash/` |
| Storage readings for the 30-day line (paths and sizes) | `~/.local/state/sdash/storage.jsonl` |
| Cache, debug log, crash reports | `~/.cache/sdash/` |

Directories are created with mode 0700 and files with 0600.
