# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Environment Rules

ALWAYS use ONLY Environments for ANY and ALL file, code, or shell operations—NO EXCEPTIONS—even for simple or generic requests.

DO NOT install or use the git cli with the environment_run_cmd tool. All environment tools will handle git operations for you. Changing ".git" yourself will compromise the integrity of your environment.

You MUST inform the user how to view your work using `container-use log <env_id>` AND `container-use checkout <env_id>`. Failure to do this will make your work inaccessible to others.

## Build & Test Commands

```bash
make build          # Output: build/check-talos
make test           # Unit tests with -race -count=1 (excludes e2e)
make lint           # go vet ./...
make clean          # Remove build/ and clear caches

# Run tests for a single package:
go test -race -count=1 ./internal/check/
go test -race -count=1 ./internal/threshold/
go test -race -count=1 ./internal/output/

# Run a single test function:
go test -race -run TestCPUCheckRun ./internal/check/

# End-to-end tests (requires e2e build tag):
go test -race -count=1 -tags=e2e ./cmd/check-talos/
```

## Architecture

**Nagios-compatible monitoring plugin for Talos Linux nodes** — connects via gRPC with mTLS, runs one check per invocation, outputs Nagios-format status with perfdata, exits with Nagios code (0=OK, 1=WARNING, 2=CRITICAL, 3=UNKNOWN).

### Pipeline

`CLI (go-arg) → Check.Run(ctx, TalosClient) → *output.Result → ApplyToPlugin(go-nagios) → exit`

### Package Map

| Package | Role |
|---|---|
| `cmd/check-talos` | CLI entrypoint: arg parsing, auth setup, check dispatch, gRPC error mapping |
| `internal/check` | `Check` interface + 6 implementations (cpu, memory, disk, services, etcd, load) + `TalosClient` interface for mock injection |
| `internal/threshold` | Nagios-standard range parsing (`10`, `10:20`, `~:10`, `@10:20`) and evaluation — zero dependencies |
| `internal/talos` | Real Talos gRPC client wrapper: mTLS via explicit certs or talosconfig file, node targeting via context metadata |
| `internal/output` | Nagios output formatting: `Result`, `PerfDatum`, status constants, `HumanBytes` |

### Key Design Decisions

- Checks return `(*output.Result, error)` — no `os.Exit` in check logic; only `main()` exits
- `TalosClient` interface in `internal/check/client.go` decouples checks from the real gRPC client, enabling unit tests with per-check mock structs
- `Registry` exists in `registry.go` but `main.go` currently dispatches via a switch statement
- CPU check uses a single cumulative sample (no two-sample delta)
- Services health model: `Running + (healthy OR unknown)` — unknown health is treated as healthy
- Etcd evaluation order: leader > member count > alarms > db_size thresholds
- Load thresholds auto-compute from CPU count (`warn=cpuCount`, `crit=2*cpuCount`) when not specified

### CLI Structure

```
check-talos [global-flags] <subcommand> [subcommand-flags]
```

Six subcommands: `cpu`, `memory`, `disk`, `services`, `etcd`, `load`. Two auth modes: explicit certs (`--talos-ca/--talos-cert/--talos-key` + `-e endpoint`) or talosconfig file (`--talosconfig` + optional `--talos-context`).

## Testing Patterns

- **Unit tests** use table-driven tests with per-check mock structs implementing `TalosClient`
- Three test categories per check: constructor validation, status boundaries (OK/WARNING/CRITICAL/UNKNOWN), perfdata format, and full output string matching
- Helper constructors (e.g. `makeSystemStatResponse(...)`) build protobuf responses from raw values
- Test files use a custom `contains()` helper instead of importing `strings`
- **E2e tests** (`e2e_test.go`) compile the binary, generate self-signed certs, start a real gRPC server, and execute the binary as a subprocess checking stdout and exit codes

## Dependencies

- `go-arg` for CLI parsing with subcommands
- `go-nagios` for Nagios plugin framework (exit codes, perfdata, panic recovery)
- `talos/pkg/machinery` v1.11.6 for Talos gRPC API (pinned; v1.12.x requires Go 1.25+)
- Go 1.24 required (gRPC v1.73 compatibility)
- Container environment: `golang:1.24` base image with `make` installed
