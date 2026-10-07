# Security

sdash runs as your Unix user; Slurm and filesystem permissions still apply.
It is a user tool, not a security boundary for an untrusted login node.

- Slurm commands pass an allowlist and mutation checks. Destructive job
  actions show a confirmation; hold/release and requested shell/GPU steps
  can run directly. Slurm text and logs are sanitized for terminal display;
  redirected CLI logs retain raw bytes.
- Configured storage commands and notification hooks execute as you, including
  shell commands. Use trusted config files, parent directories and `PATH` tools.
  Unsafe config permissions disable command entries and prevent settings saves.
- No telemetry. Slurm clients contact cluster services; updates contact GitHub.
  SSH sessions and configured hooks can also use the network.
- State, logs and recordings may contain usernames, paths and job details.
  Review them before sharing; use `sdash --demo` for public screenshots.
- Downloads are checked with SHA-256. Signature/provenance checks depend on
  available `cosign` or authenticated `gh` and release metadata; checksum-only
  installation is possible and is reported explicitly.
  GitHub build provenance is generated only for public-repository releases.
  Use `cosign` for signature verification of private releases; `gh` verification
  requires GitHub provenance and fails if it is missing.

Report vulnerabilities privately through this repository's **Security → Report
a vulnerability** when enabled. Do not include sensitive details in public issues.
Security fixes target the latest release.
