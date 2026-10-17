# Contributing

Thanks for helping. Reports from clusters sdash has not seen yet help the most,
so they come first.

## Report how sdash works on your cluster

Open a **cluster compatibility report** issue with `sdash version`, `sdash
doctor` and what looks wrong. If you can, record what your Slurm prints:

```sh
sdash record --anonymize --out ~/sdash-fixtures
```

This runs only read-only commands and replaces user, account, job and cluster
names and your home directory. Node names and other paths stay, so read the
files before you share them, and never share `anonymize.mapping.json`: it maps
the replacements back to the real names. A
recording becomes a fixture in `testdata/fixtures/`, and every later change is
checked against it.

## Bugs and ideas

Use the issue templates. For a bug, include `sdash version`, `sdash doctor`
and the terminal you use. Report security problems privately, see
[SECURITY.md](SECURITY.md).

## Develop

You need the Go version in `go.mod`, Make and
[golangci-lint](https://golangci-lint.run/).

```sh
make check                   # vet, lint, race tests
make build && ./sdash --demo # try your change on a simulated cluster
srun -c 4 -t 30 make check   # on a shared login node, use a compute node
```

After a change to the screen, run `go test ./internal/ui/... -update` and read
every snapshot diff in `internal/ui/testdata/`. After a parser change, run
`go test ./internal/slurm/parse -run TestGoldenFixtures -update` and read the
golden diffs. Commit messages follow
[Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`,
`docs:`, `test:`, `ci:`).

### Real Slurm in Docker

`test/cluster/` builds a throwaway Slurm cluster (controller, accounting, three
compute nodes, a login node) for each version in `test/cluster/matrix.txt`.
GitHub Actions runs it on every pull request that touches the Slurm layer
(`.github/workflows/compat.yml`), so you do not need Docker yourself. With
Docker:

```sh
bash test/cluster/compat.sh 24.11      # one version: build, seed, check, report
make cluster-up SLURM=24.11 && make cluster-seed && make cluster-shell
make cluster-down SLURM=24.11
```

See [test/cluster/README.md](test/cluster/README.md).

## Rules that keep it safe

- Every subprocess goes through `internal/execx`; nothing else imports
  `os/exec`. Do not widen its allowlist, timeouts or concurrency limit without
  discussing it in an issue first.
- Only `internal/actions` may run a command that changes Slurm state, and only
  after a confirmation that shows the exact command.
- Parsers return warnings and never panic. Unknown Slurm output gets a fixture,
  not a guess. Views do no I/O.
- Tests never cancel, submit or change jobs on a real cluster. The integration
  tests run only on the throwaway cluster and only touch `sdash-test-*` jobs.
- New dependencies, network calls and changed defaults need an issue first.
  Command-line flags, `--json` output (`"schema": 1`) and config keys stay
  compatible.

## Where things go

- **A setting:** a `Setting` in `internal/config/settings.go`, its field in
  `Config` and `Default` (`config.go`), its commented line in `default.toml`
  and a row in `docs/config.md`. The tests fail until all four agree.
- **A job action:** `actions.Default()` in `internal/actions/actions.go`, a key
  in `internal/ui/keys/keymap.go` and a command in `internal/ui/commands.go`.
- **A Slurm command:** its argv in `internal/slurm/commands.go`, a parser with a
  fixture in `internal/slurm/parse/`, and a collector; the scheduler keeps the
  5 second floor and the limit of three commands at once.
- **A cluster feature check:** `siteSettings` in `internal/slurm/parse/misc.go`
  and `Features` in `internal/slurm/features.go`; `sdash doctor` picks it up.
