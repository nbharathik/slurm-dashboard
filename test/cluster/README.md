# Throwaway Slurm clusters

Docker Compose clusters named `sdashtest` (db, ctld, c1, c2, g1, login) built from
Ubuntu or Debian Slurm packages, one per version in `matrix.txt`. Test use only:
fixed passwords, a baked-in munge key, no cgroups, fake GPUs (`File=` on dummy devices).

`make cluster-up SLURM=24.11`, then `make cluster-seed` and `make cluster-shell`
(a shell as `you` with `sdash` on the PATH). `make cluster-down` removes it.
`make compat` runs `compat.sh` for every version: CLI checks, tmux smoke test, fixtures,
integration tests. Results go to `build/compat/<version>/` and `build/compat/report.md`.
CI runs the same in `.github/workflows/compat.yml`. Never point these tests at a real cluster.
