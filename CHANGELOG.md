# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-02-10

Initial release of check-talos, a Nagios-compatible monitoring plugin for
Talos Linux nodes via the Talos gRPC API.

### Added

- **CLI framework** using `go-arg` with subcommand dispatch pattern
  (`check-talos [global-flags] <subcommand> [subcommand-flags]`)
- **Six monitoring checks**, each producing Nagios-compliant output with
  performance data:
  - `cpu` — aggregate CPU utilization from cumulative kernel counters
  - `memory` — memory utilization based on `memavailable` (accounts for
    reclaimable buffers and caches)
  - `disk` — disk capacity for a configurable mount point (`--mount`, default
    `/`)
  - `services` — Talos system service health with `--include`/`--exclude`
    filtering (binary: all healthy or CRITICAL)
  - `etcd` — etcd cluster health combining structural assertions (leader
    exists, member count, active alarms) with DB size thresholds
  - `load` — system load average with auto-computed default thresholds based on
    CPU count (warning = N CPUs, critical = 2N CPUs)
- **Two authentication modes** for connecting to the Talos gRPC API:
  - Explicit mTLS certificate paths (`--talos-ca`, `--talos-cert`,
    `--talos-key`) with `--talos-endpoint`
  - Talosconfig file (`--talosconfig`) with optional `--talos-context`
- **Node targeting** via `--node` flag for routing requests through a
  control-plane `apid` proxy to any node in the cluster
- **Nagios-standard threshold parsing** supporting the full range syntax (`10`,
  `10:`, `~:10`, `10:20`, `@10:20`) in the `internal/threshold` package
- **Nagios output formatting** with status line, optional long text, and
  machine-readable performance data (`internal/output` package)
- **`TalosClient` interface** in `internal/check` for decoupling checks from
  the real gRPC client, enabling unit testing with mock structs
- **gRPC error mapping** to Nagios exit codes — connectivity failures map to
  CRITICAL (2), configuration errors to UNKNOWN (3)
- **Panic recovery** via `go-nagios` framework, ensuring crashes produce a
  valid Nagios status line instead of a raw stack trace
- **Configurable timeout** (`--timeout`, default 10s) for gRPC call deadlines
- **Comprehensive input validation** with Nagios-formatted error messages
  (exit code 3) for all argument and configuration errors
- **Unit tests** with table-driven patterns and per-check mock structs
  covering constructor validation, status boundaries, perfdata format, and
  full output string matching
- **End-to-end tests** (build tag `e2e`) that compile the binary, generate
  self-signed mTLS certificates, start a real gRPC server, and verify stdout
  and exit codes
- `Makefile` with `build`, `test`, `lint`, and `clean` targets

[0.1.0]: https://github.com/DLAKE-IO/check-talos/releases/tag/v0.1.0
