# sdash for administrators

What sdash asks of your controller, what it runs, and how to install it for
everyone.

## Load on the controller

Measured with the real collectors over a simulated hour (`TestCallBudget`),
at the default `refresh = "normal"`, one user:

| Situation | Slurm calls a minute |
| --- | --- |
| Overview open | about 12 |
| Jobs tab / a job card open | about 9 / 12 |
| Queue tab | about 10 |
| Usage tab | about 8 |
| Terminal unfocused or idle | about 4 |
| Left alone for 2 hours | about 3 (about 1.5 without notices) |
| Paused (`p`) or `refresh = "manual"` | 0 |

`refresh = "slow"` makes about a third of these calls. When a query returns
more than 5,000 jobs, the running-job and everyone's-jobs queries slow to
once a minute on their own. When the controller rate-limits a user, every
source backs off. `sdash doctor` prints the estimate for a user's setting,
and `ctrl+d` in the dashboard shows the live number.

The optional Scheduler tab adds one `sdiag` call every 2 minutes while it is
open. If `sdiag` is restricted to operators on your site, the tab says so.

## What sdash runs

Every command goes through one allowlist with timeouts and at most a few
calls at once:

- Read-only: `squeue`, `sinfo`, `scontrol show ...`, `sacct`, `sstat`,
  `sshare`, `sprio`, `sdiag` (no arguments), `id`, and the quota tools
  (`lfs quota`, `mmlsquota`, `beegfs-ctl --getquota`, `quota`).
- Changes, only after the user confirms the exact command: `scancel`,
  `scontrol hold|release|requeue`, `sbatch` (rerun), `srun --overlap`
  (shell in a job).

No daemon, no web service, no admin commands, no database access.

## Site-wide install

Put the binary somewhere on the default `PATH` (or in a module, below) and
site defaults in `/etc/sdash/config.toml`. Each user's file is read on top
of it. Useful site settings:

```toml
hide_partitions = ["login", "debug"]

[gpu_names]
"nvidia_h100_80gb_hbm3" = "H100"

[[storage]]
label   = "Scratch"
path    = "/scratch/$USER"
backend = "lustre"
```

Commands in a config file (`notify_command`, storage commands) and
`[profiles]` are used only when the file and its parent directories belong
to root or the user and are not writable by group or others.

## Environment module

A minimal Lmod file, `modulefiles/sdash/0.3.0.lua`:

```lua
help([[sdash: a terminal dashboard for your Slurm jobs]])
whatis("Name: sdash")
whatis("Version: 0.3.0")
prepend_path("PATH", "/opt/sdash/0.3.0/bin")
```

## Reporting problems

`sdash doctor --report` prints one block with the version, platform and every
check, with the user name, home directory and cluster name replaced, ready
to paste into an issue.
