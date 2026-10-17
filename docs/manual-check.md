# Checking sdash on a real cluster

About 30 minutes on a login node, as your normal user. CI already tests six
Slurm versions in Docker; this checks what only a real site has: its own
configuration, quota tools, terminal and scale. Note anything that differs and
send it as a cluster compatibility issue.

Sections 1 to 4 are read-only. Section 5 changes state only for jobs you submit
for the test, all named `sdash-test-*`.

## 1. Install

```sh
curl -fsSL https://raw.githubusercontent.com/nbharathik/slurm-dashboard/main/install.sh | sh
```

To test a pre-release, add `-s -- --version v0.2.0-rc.1` after `sh`.

- [ ] `sdash version` prints the version you installed, from any directory.
- [ ] `sdash doctor` says Slurm, the controller and PATH are fine, and lists
      each feature (history, rerun, fairshare, live usage, everyone's jobs)
      with a reason that matches what you know of the cluster.
- [ ] `sdash update --check` answers without an error.

## 2. The dashboard against Slurm

Open `sdash` and compare each tab with the Slurm command beside it.

- [ ] Jobs (`2`) lists the same jobs as `squeue --me`.
- [ ] Queue (`3`) holds as many jobs as `squeue -h | wc -l`, or says other
      users' jobs are hidden (`PrivateData=jobs`).
- [ ] Nodes (`4`) agrees with `sinfo -N -l`: states, free CPUs, memory, GPUs.
- [ ] Usage (`5`): fairshare matches `sshare -U`; the job counts match
      `sdash history --days 7`.
- [ ] Storage (`6`) matches your quota command (`quota -s`, `lfs quota`,
      `mmlsquota` or what your site documents).
- [ ] Overview (`1`): each partition's free and total CPUs or GPUs look right;
      partitions you may not use are marked.
- [ ] `ctrl+d` after a few idle minutes on the Overview shows about 15 Slurm
      calls per minute or fewer.

## 3. Terminals

- [ ] An 80x24 window: every tab is readable and nothing is cut mid-word.
- [ ] Inside tmux, with `NO_COLOR=1`, and with "Plain symbols" on (`,`).
- [ ] The terminal you use every day (Windows Terminal, MobaXterm, PuTTY,
      iTerm2): borders, colours and the mouse work.

## 4. Command line

- [ ] `sdash free --gpus 1` (or `--cpus 4 --mem 8G` without GPUs) names nodes
      that fit now or the soonest; `--test` adds the scheduler's answer.
- [ ] `sdash why <id>` for a pending job gives the same reason as `squeue`.
- [ ] `sdash status --json | python3 -m json.tool` prints valid JSON.

## 5. Your own test jobs

```sh
sbatch -J sdash-test-run -t 10 --wrap 'for i in $(seq 300); do echo line $i; sleep 1; done'
sbatch -J sdash-test-idle -c 8 -t 10 --wrap 'sleep 120'
```

- [ ] `l` on `sdash-test-run` follows new lines within a few seconds.
- [ ] `c` on `sdash-test-run` shows the exact `scancel` command; confirm it.
- [ ] After `sdash-test-idle` ends, Usage (`5`) counts its idle CPUs. `n` on
      it opens the rerun form with smaller suggestions; `s` asks
      `sbatch --test-only` for a start time. Close it with `esc` without
      submitting. Where the cluster stores no job scripts, this works only
      for a few minutes after the job ends.
- [ ] Clean up: `scancel -u $USER -n sdash-test-run,sdash-test-idle`.

## 6. Send what you found

```sh
sdash record --anonymize --out ~/sdash-fixtures
```

Read the files first, never share `anonymize.mapping.json`, and attach the
version folder to a cluster compatibility issue together with `sdash doctor`.
