# Slurm compatibility

Every week, and on every change to the Slurm layer, GitHub Actions runs sdash
against throwaway clusters built from these distribution packages
(`test/cluster/`, `.github/workflows/compat.yml`):

| Slurm | Package | Result |
| --- | --- | --- |
| 21.08 | Ubuntu 22.04, 21.08.5 | all checks pass |
| 22.05 | Debian 12, 22.05.8 | all checks pass |
| 23.11 | Ubuntu 24.04, 23.11.4 | all checks pass |
| 24.11 | Debian 13, 24.11.5 | all checks pass |
| 25.11 | Ubuntu 26.04, 25.11.2 | all checks pass; see the note on live usage |
| 26.05 | Debian unstable, 26.05.4 | all checks pass |

Each run checks every `--json` command, `why`, `logs`, `free --test`, the
dashboard at 160x45, 80x24 and in ASCII, records fixtures with `sdash record`,
and runs the integration tests (actions, rerun, usage against `sacct`,
efficiency against seff's formula, followed logs, notifications). The recorded
output of each version is in `testdata/fixtures/<version>-docker/`.

## Differences sdash handles

- **Start estimates, 25.11 and newer.** `sbatch --test-only` prints an extra
  word after the start time (`... 21:24:23 a using 1 processors ...`).
- **Live usage on 25.11.2.** `sstat` prints CPU time it never measured as
  `213503982334-14:25:51`, and accounting records no peak memory. sdash shows
  live CPU use and memory efficiency as unknown instead of zero, so the idle-job
  warning stays quiet. Slurm 25.11.3 lists a fix for missing step CPU values.
- **Rerun before 23.02.** Slurm stores no submit line, so a rerun uses the
  script's `#SBATCH` lines and the job's working directory from `scontrol`.
- **Partitions closed to your groups** are hidden by Slurm itself; sdash marks a
  partition as closed only when a `DenyAccounts` list holds all your accounts.

## Not yet tested

Slurm 23.02, 24.05 and 25.05 have no distribution packages and need source
builds; site quota tools (`lfs quota`, `mmlsquota`, `beegfs-ctl`) are tested
against their documented formats only.

If something looks wrong on your cluster, open a cluster compatibility issue
with the output of `sdash doctor`, and if you can, a recording:

```sh
sdash record --anonymize --out ~/sdash-fixtures
```

Read the files first and never share `anonymize.mapping.json`.
