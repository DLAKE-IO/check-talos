# check-talos

Nagios-compatible monitoring plugin for [Talos Linux](https://www.talos.dev/) nodes. Connects via gRPC with mutual TLS, runs one check per invocation, outputs Nagios-format status with performance data, and exits with a standard Nagios code.

## Features

- **Six checks** — CPU, memory, disk, services, etcd, load
- **Nagios-standard thresholds** — full range syntax (`10`, `10:20`, `~:10`, `@10:20`)
- **Two authentication modes** — explicit certificate paths or talosconfig file
- **Node targeting** — reach any node through a control-plane load balancer via `--node`
- **Performance data** — machine-readable metrics for graphing (PNP4Nagios, Grafana, etc.)
- **Single binary** — one binary with subcommands, easy to distribute and version

## Requirements

- Go 1.24+
- Talos Linux cluster with mTLS credentials (CA cert, client cert, client key)

## Building

```bash
make build        # Output: build/check-talos
```

Other targets:

```bash
make test         # Unit tests with -race -count=1
make lint         # go vet ./...
make clean        # Remove build/ and clear caches
```

## Quick Start

```bash
# CPU check with explicit certificates
check-talos -e 10.0.0.1:50000 \
  --talos-ca /etc/talos/ca.crt \
  --talos-cert /etc/talos/admin.crt \
  --talos-key /etc/talos/admin.key \
  cpu

# Memory check using talosconfig
check-talos --talosconfig /etc/talos/config \
  -n worker-01 \
  memory

# Disk check on /var with custom thresholds
check-talos -e 10.0.0.1:50000 \
  --talos-ca /etc/talos/ca.crt \
  --talos-cert /etc/talos/admin.crt \
  --talos-key /etc/talos/admin.key \
  disk -m /var -w 85 -c 95
```

## Usage

```
check-talos [global-flags] <subcommand> [subcommand-flags]
```

### Global Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--talos-endpoint` | `-e` | | Talos API endpoint (`host:port`). Required with explicit certs. |
| `--talos-ca` | | | Path to Talos CA certificate. |
| `--talos-cert` | | | Path to client certificate. |
| `--talos-key` | | | Path to client private key. |
| `--talosconfig` | | | Path to talosconfig file. Alternative to explicit cert paths. |
| `--talos-context` | | | Named context within talosconfig. |
| `--timeout` | `-t` | `10s` | gRPC call timeout (max 120s). |
| `--node` | `-n` | | Target node hostname or IP for apid proxy routing. |

### Authentication

Two modes are supported. Explicit certificate paths take precedence if both are provided.

**Explicit certificates** — provide all three of `--talos-ca`, `--talos-cert`, `--talos-key` along with `-e endpoint`:

```bash
check-talos -e 10.0.0.1:50000 \
  --talos-ca ca.crt --talos-cert admin.crt --talos-key admin.key \
  cpu
```

**Talosconfig file** — provide `--talosconfig` with optional `--talos-context`:

```bash
check-talos --talosconfig /etc/talos/config --talos-context production \
  -n worker-01 cpu
```

### Node Targeting

The `--node` flag sets a gRPC metadata header that tells the Talos `apid` proxy to route the request to a specific node. This allows monitoring any node in the cluster through a single control-plane load balancer:

```bash
# LB at 10.0.0.100 routes to control plane apid;
# --node tells apid to proxy to worker-07
check-talos -e 10.0.0.100:50000 \
  --talos-ca ca.crt --talos-cert admin.crt --talos-key admin.key \
  -n worker-07 cpu
```

## Checks

### cpu

Aggregate CPU utilization from cumulative kernel counters.

```bash
check-talos [...] cpu [-w 80] [-c 90]
```

| Flag | Default | Description |
|---|---|---|
| `-w` | `80` | Warning threshold (%) |
| `-c` | `90` | Critical threshold (%) |

Output example:
```
TALOS CPU OK - CPU usage 34.2% | cpu_usage=34.2%;80;90;0;100
TALOS CPU WARNING - CPU usage 82.5% | cpu_usage=82.5%;80;90;0;100
```

### memory

Memory utilization based on `memavailable` (not `memfree`), which correctly accounts for reclaimable buffers and caches.

```bash
check-talos [...] memory [-w 80] [-c 90]
```

| Flag | Default | Description |
|---|---|---|
| `-w` | `80` | Warning threshold (%) |
| `-c` | `90` | Critical threshold (%) |

Output example:
```
TALOS MEMORY OK - Memory usage 62.3% (4.98 GB / 8.00 GB) | memory_usage=62.3%;80;90;0;100 memory_used=5348024320B;;;0;8589934592 memory_total=8589934592B;;;0;
```

### disk

Disk capacity for a specific mount point.

```bash
check-talos [...] disk [-m /var] [-w 80] [-c 90]
```

| Flag | Default | Description |
|---|---|---|
| `-m` | `/var` | Mount point to check (must be absolute path) |
| `-w` | `80` | Warning threshold (%) |
| `-c` | `90` | Critical threshold (%) |

Output example:
```
TALOS DISK OK - /var usage 45.0% (9.00 GB / 20.00 GB) | disk_usage=45.0%;80;90;0;100 disk_used=9663676416B;;;0;21474836480 disk_total=21474836480B;;;0;
```

### services

Talos system service health. No thresholds — any unhealthy service is immediately CRITICAL.

A service is considered healthy when its state is `Running` AND its health check reports `healthy` or `unknown` (services without a health endpoint).

```bash
check-talos [...] services [--exclude apid --exclude trustd]
check-talos [...] services [--include kubelet --include etcd]
```

| Flag | Default | Description |
|---|---|---|
| `--exclude` | | Service IDs to ignore (repeatable). |
| `--include` | | Only check these service IDs (repeatable). |

`--include` and `--exclude` are mutually exclusive.

Output example:
```
TALOS SERVICES OK - 8/8 services healthy | services_total=8;;;0; services_healthy=8;;;0; services_unhealthy=0;;;0;
TALOS SERVICES CRITICAL - 1/8 services unhealthy: kubelet | services_total=8;;;0; services_healthy=7;;;0; services_unhealthy=1;;;0;
kubelet: state=Finished, health=unhealthy, message="readiness probe failed"
```

### etcd

Etcd cluster health with structural assertions and DB size thresholds. Must be run against **control-plane nodes only** (worker nodes don't run etcd).

Evaluation order: leader exists > member count >= minimum > no active alarms > DB size thresholds. Structural failures are always CRITICAL regardless of thresholds.

```bash
check-talos [...] etcd [-w '~:100000000'] [-c '~:200000000'] [--min-members 3]
```

| Flag | Default | Description |
|---|---|---|
| `-w` | `~:100000000` | Warning threshold for DB size in bytes (~100 MB) |
| `-c` | `~:200000000` | Critical threshold for DB size in bytes (~200 MB) |
| `--min-members` | `3` | Minimum expected member count |

Output example:
```
TALOS ETCD OK - Leader 1234, 3/3 members, DB 12.50 MB | etcd_dbsize=13107200B;100000000;200000000;0; etcd_dbsize_in_use=8388608B;;;0; etcd_members=3;;;0;
TALOS ETCD CRITICAL - No leader elected | etcd_dbsize=45000000B;100000000;200000000;0; etcd_dbsize_in_use=40000000B;;;0; etcd_members=3;;;0;
TALOS ETCD CRITICAL - Active alarm: NOSPACE | etcd_dbsize=2147483648B;100000000;200000000;0; etcd_dbsize_in_use=2000000000B;;;0; etcd_members=3;;;0;
```

### load

System load average with auto-computed defaults based on CPU count.

When `-w` and `-c` are not specified, thresholds are computed at runtime: warning = CPU count, critical = 2x CPU count. For a 4-core node, this defaults to `-w 4 -c 8`.

All three load averages (1m, 5m, 15m) are always emitted in performance data for graphing. Thresholds apply only to the selected period.

```bash
check-talos [...] load [--period 5] [-w 4] [-c 8]
```

| Flag | Default | Description |
|---|---|---|
| `--period` | `5` | Load average period: `1`, `5`, or `15` (minutes) |
| `-w` | *(auto)* | Warning threshold (raw load value) |
| `-c` | *(auto)* | Critical threshold (raw load value) |

Output example:
```
TALOS LOAD OK - Load average (5m) 1.23 | load1=0.98;;;0; load5=1.23;4;8;0; load15=1.45;;;0;
TALOS LOAD WARNING - Load average (5m) 4.56 | load1=5.12;;;0; load5=4.56;4;8;0; load15=3.21;;;0;
```

## Threshold Format

Thresholds follow the [Nagios Plugin Development Guidelines](https://nagios-plugins.org/doc/guidelines.html#THRESHOLDFORMAT):

| Notation | Alert Condition |
|---|---|
| `10` | Value < 0 or > 10 (outside 0..10) |
| `10:` | Value < 10 (outside 10..+inf) |
| `~:10` | Value > 10 (outside -inf..10) |
| `10:20` | Value < 10 or > 20 (outside 10..20) |
| `@10:20` | Value >= 10 and <= 20 (inside 10..20) |

Critical is always evaluated before warning. If both thresholds are violated, the exit code is `2` (CRITICAL).

## Exit Codes

| Code | Status | When |
|---|---|---|
| `0` | OK | Metric within acceptable range |
| `1` | WARNING | Metric exceeds warning threshold but not critical |
| `2` | CRITICAL | Metric exceeds critical threshold, connectivity failure, structural assertion failure |
| `3` | UNKNOWN | Configuration error, invalid arguments, API version mismatch |

## Output Format

All output follows the Nagios plugin specification:

```
TALOS <CHECK> <STATUS> - <summary> | <perfdata>
<optional long text>
```

- The status line is always exactly one line on stdout
- Performance data uses raw values (bytes as integers with `B` UOM) for graphing compatibility
- Human-readable values appear in the summary (e.g., `12.50 MB`)
- Long text (multi-line details) appears only for CRITICAL states with diagnostic information

## Nagios Integration

### Command Definition

```cfg
object CheckCommand "check_talos" {
  command = [ "/usr/lib/nagios/plugins/check-talos", "$talos_command$" ]

  arguments = {
    "--talos-endpoint" = {
      value = "$talos_endpoint$"
    }

    "--talosconfig" = {
      value = "$talos_config$"
    }

    "--talos-ca" = {
      value = "$talos_ca$"
    }

    "--talos-cert" = {
      value = "$talos_cert$"
    }

    "--talos-key" = {
      value = "$talos_key$"
    }

    "--talos-context" = {
      value = "$talos_context$"
    }

    "--node" = {
      value = "$talos_node$"
    }

    "--timeout" = {
      value = "$talos_timeout$"
    }

    "--warning" = {
      value = "$talos_warning$"
    }

    "--critical" = {
      value = "$talos_critical$"
    }

    "--mount" = {
      value = "$talos_mount$"
    }

    "--period" = {
      value = "$talos_period$"
    }

    "--min-members" = {
      value = "$talos_min_members$"
    }

    "--include" = {
      value = "$talos_include$"
    }

    "--exclude" = {
      value = "$talos_exclude$"
    }
  }
  vars.talos_timeout = "10s"
}
```

### Service Definitions

```cfg
apply Service "talos-cpu" {
  display_name = "CPU"
  check_command = "check_talos"

  vars.talos_command  = "cpu"
  vars.talos_warning  = "80"
  vars.talos_critical = "90"

  vars.talos_node     = host.address
  vars.talos_endpoint = host.address + ":50000"

  vars.talos_ca = "path/to/ca"
  vars.talos_cert = "path/to/cert"
  vars.talos_key = "path/to/key"

  assign where host.vars.os == "talos-linux"
}

apply Service "talos-memory" {
  display_name = "memory"
  check_command = "check_talos"

  vars.talos_command  = "memory"
  vars.talos_warning  = "80"
  vars.talos_critical = "90"

  vars.talos_node     = host.address
  vars.talos_endpoint = host.address + ":50000"

  vars.talos_ca = "path/to/ca"
  vars.talos_cert = "path/to/cert"
  vars.talos_key = "path/to/key"

  assign where host.vars.os == "talos-linux"
}

apply Service "talos-disk-var" {
  display_name = "disk /var"
  check_command = "check_talos"

  vars.talos_command  = "disk"
  vars.talos_warning  = "80"
  vars.talos_critical = "90"
  vars.talos_mount    = "/var"

  vars.talos_node     = host.address
  vars.talos_endpoint = host.address + ":50000"

  vars.talos_ca = "path/to/ca"
  vars.talos_cert = "path/to/cert"
  vars.talos_key = "path/to/key"

  assign where host.vars.os == "talos-linux"
}

apply Service "talos-etcd" {
  display_name = "etcd"
  check_command = "check_talos"

  vars.talos_command      = "etcd"
  vars.talos_warning      = "~:100000000"
  vars.talos_critical     = "~:200000000"
  vars.talos_min_members  = 3

  vars.talos_node     = host.address
  vars.talos_endpoint = host.address + ":50000"

  vars.talos_ca = "path/to/ca"
  vars.talos_cert = "path/to/cert"
  vars.talos_key = "path/to/key"

  assign where host.vars.os == "talos-linux"
}

apply Service "talos-services" {
  display_name = "services"
  check_command = "check_talos"

  vars.talos_command      = "services"

  vars.talos_node     = host.address
  vars.talos_endpoint = host.address + ":50000"

  vars.talos_ca = "path/to/ca"
  vars.talos_cert = "path/to/cert"
  vars.talos_key = "path/to/key"

  assign where host.vars.os == "talos-linux"
}

apply Service "talos-load" {
  import "gtrs1-service"

  display_name = "load"
  check_command = "check_talos"

  vars.talos_command      = "load"
  vars.talos_warning      = "10"
  vars.talos_critical     = "12"
  vars.talos_period       = 5

  vars.talos_node     = host.address
  vars.talos_endpoint = host.address + ":50000"

  vars.talos_ca = "/path/to/ca"
  vars.talos_cert = "/path/to/cert"
  vars.talos_key = "/path/to/key"

  assign where host.vars.os == "talos-linux"
}
```

## Error Handling

| Scenario | Exit Code | Perfdata |
|---|---|---|
| Invalid CLI arguments | 3 (UNKNOWN) | No |
| Certificate file not found | 3 (UNKNOWN) | No |
| Connection refused | 2 (CRITICAL) | No |
| TLS handshake failure | 2 (CRITICAL) | No |
| gRPC timeout | 2 (CRITICAL) | No |
| gRPC PermissionDenied | 3 (UNKNOWN) | No |
| gRPC Unimplemented | 3 (UNKNOWN) | No |
| Empty API response | 3 (UNKNOWN) | No |
| Mount point not found | 3 (UNKNOWN) | No |
| Etcd RPC fails on worker node | 3 (UNKNOWN) | No |
| Structural failure (e.g., no etcd leader) | 2 (CRITICAL) | Yes (when data was retrieved) |

Connectivity failures are CRITICAL (actionable). Configuration errors are UNKNOWN. WARNING is exclusively for successful checks where a threshold is breached.

## Architecture

```
CLI (go-arg) --> Check.Run(ctx, TalosClient) --> *output.Result --> ApplyToPlugin(go-nagios) --> exit
```

### Package Layout

| Package | Role |
|---|---|
| `cmd/check-talos` | CLI entrypoint: arg parsing, validation, auth setup, check dispatch, gRPC error mapping |
| `internal/check` | `Check` interface + 6 implementations + `TalosClient` interface for mock injection |
| `internal/threshold` | Nagios-standard range parsing and evaluation (zero dependencies) |
| `internal/talos` | Talos gRPC client wrapper: mTLS, talosconfig, node targeting |
| `internal/output` | Nagios output formatting: `Result`, `PerfDatum`, status constants, `HumanBytes` |

### Key Design Decisions

- **Checks return `(*output.Result, error)`** — no `os.Exit` in check logic; only `main()` exits
- **`TalosClient` interface** decouples checks from the real gRPC client, enabling unit tests with mock structs
- **Single cumulative CPU sample** — acceptable for Nagios polling intervals (1-5 min); avoids added latency of a two-sample delta
- **Services are binary** — a non-running service is always CRITICAL; there is no meaningful WARNING state
- **Etcd uses structural assertions before thresholds** — no leader / missing members is categorical, not a gradient
- **Load thresholds auto-compute from CPU count** — a 2-core and 64-core node have very different "normal" load

## Testing

```bash
# All unit tests
make test

# Single package
go test -race -count=1 ./internal/check/
go test -race -count=1 ./internal/threshold/
go test -race -count=1 ./internal/output/

# Single test function
go test -race -run TestCPUCheckRun ./internal/check/

# End-to-end tests (builds binary, generates certs, starts mock gRPC server)
go test -race -count=1 -tags=e2e ./cmd/check-talos/
```

Unit tests use table-driven patterns with per-check mock structs. E2e tests compile the binary, generate self-signed mTLS certificates, start a real gRPC server, and execute the binary as a subprocess checking stdout and exit codes.

## Dependencies

| Dependency | Purpose |
|---|---|
| [go-arg](https://github.com/alexflint/go-arg) | CLI parsing with subcommands |
| [go-nagios](https://github.com/atc0005/go-nagios) | Nagios plugin framework (exit codes, perfdata, panic recovery) |
| [talos/pkg/machinery](https://github.com/siderolabs/talos) v1.11.6 | Talos gRPC API client and protobuf types |
| [grpc-go](https://google.golang.org/grpc) | gRPC transport |

## License

See [LICENSE](LICENSE) for details.
