# sdash: Slurm Dashboard

A terminal dashboard for Slurm jobs, queues, CPU/GPU capacity, usage and storage.
Use it on Linux login nodes at university, research or other HPC clusters with
Slurm commands on your `PATH`. No root access or daemon required.

![sdash demo](docs/demo.gif)

## Install and run

Install the latest prebuilt binary:

```sh
curl -fsSL https://raw.githubusercontent.com/nbharathik/slurm-dashboard/main/install.sh | sh
```

Then run:

```sh
sdash
sdash --demo
```

Installs one executable in `~/.local/bin`, without root, Go or a source checkout.
Install once on a login node or shared home; compute nodes need no installation.
Release validation requires **under 20 MiB installed**. Bash completion is optional.
See [installation](docs/install.md) for measured sizes, PATH, offline setup and updates.

Press `?` for help, `,` for settings and `q` to quit.

## Security

Runs with your permissions. Slurm commands use an allowlist; destructive job
actions require confirmation, and displayed Slurm text is sanitized. No
telemetry; updates contact GitHub, and configured hooks may access the network.
Trust your config and cluster tools. See [security](SECURITY.md).

[Usage](docs/usage.md) · [Settings](docs/config.md) · [MIT licence](LICENSE)
