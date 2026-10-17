# sdash: Slurm Dashboard

A terminal dashboard for Slurm users: why your jobs wait, what they waste and
where the next one fits. It runs on the login node as you, with the Slurm
commands already on your `PATH`.

![sdash on a demo cluster: why a job waits, its start estimate, wasted resources and storage](docs/demo.gif)

## What it answers

- **Why is my job pending, and when will it start?** `w` on a job gives the
  reason in plain words, its place in the queue and its priority factors. `e`
  on the Queue tab asks the scheduler for a start estimate.
- **What did my jobs waste?** The Usage tab (`5`) adds up the CPU, memory and
  GPU time your jobs held but did not use. `n` runs a finished job again with
  right-sized resources, after showing the exact `sbatch` command.
- **Where does a job fit right now?** Overview (`1`) and Nodes (`4`) show free
  CPUs, memory and GPUs per partition and node. `sdash free --gpus 1` names
  the nodes.
- **What needs my attention?** Overview lists failed and out-of-memory jobs,
  jobs close to their time limit and quotas that are nearly full.
- **When will my storage fill up?** Storage (`6`) shows your quotas with a
  30-day trend and a "full in about N days" forecast. `a` finds the largest
  directories.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/nbharathik/slurm-dashboard/main/install.sh | sh
```

```sh
sdash          # open the dashboard
sdash --demo   # try it on a simulated cluster first
```

The installer puts one executable (under 20 MiB) in `~/.local/bin`, without
root, Go or a source checkout. Install it once in your home directory on a
login node; compute nodes need nothing. Every download is checked against
SHA-256, and its signature or provenance too when `cosign` or `gh` is
installed. See
[installation](docs/install.md) for PATH, offline installs and `sdash update`.

Press `?` for every key, `,` for settings and `q` to quit.

## Screens

<p>
  <img src="docs/img/why.png" alt="Jobs tab: why the sweep array waits, its estimated start and priority" width="49%">
  <img src="docs/img/usage.png" alt="Usage tab: failed jobs, idle CPU and memory, limits, fairshare and wasteful jobs" width="49%">
  <img src="docs/img/rerun.png" alt="Rerun form with right-size suggestions beside the job's batch script" width="49%">
  <img src="docs/img/storage.png" alt="Storage tab: quotas, shared filesystems and a forecast of when Scratch is full" width="49%">
</p>

More in [usage](docs/usage.md): every tab, key and command.

## Works without

- **Root, a daemon or a database.** sdash runs the Slurm client commands as
  you (`squeue`, `sinfo`, `scontrol show`, `sacct`, `sstat`, `sshare`,
  `sprio`), at most three at once and no source more often than every 5
  seconds. `,` makes it slower, and `ctrl+d` shows every call.
- **slurmrestd or Slurm's JSON output.** It reads the plain text output of
  Slurm [21.08 to 26.05](docs/compatibility.md), tested in CI on every change.
- **ssh to compute nodes.** Logs come from the shared filesystem, and a shell
  in a running job uses `srun --overlap`.
- **A complete Slurm setup.** No accounting, no GPUs, `priority/basic` or
  `PrivateData=jobs`: each missing part shows a one-line reason, and `sdash
  doctor` lists what your cluster offers.

Every view is also a command with `--json` output for scripts: `sdash status`,
`jobs`, `queue`, `nodes`, `free`, `usage`, `history`, `why`, `logs`, `quota`.

## Security

sdash runs with your permissions and only acts on your own jobs. Slurm
commands pass an allowlist with timeouts; cancel, requeue and rerun show the
exact command and ask first. Text from Slurm is sanitized before it
reaches your terminal. No telemetry: updates contact GitHub, and hooks you
configure may use the network. Releases are signed with cosign and come with
SBOMs and build provenance. See [security](SECURITY.md).

## Contributing

Bug reports, and above all reports from clusters sdash has not seen yet, help
the most: see [contributing](CONTRIBUTING.md) and the issue templates.

[Usage](docs/usage.md) · [Settings](docs/config.md) · [Compatibility](docs/compatibility.md) · [Changelog](CHANGELOG.md) · [MIT licence](LICENSE)
