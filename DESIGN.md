# check-talos — Design Document

Nagios-compatible monitoring plugin for Talos Linux nodes via the Talos gRPC API.

---

## 1. Overall Architecture

```
cmd/
  check-talos/
    main.go              # Entrypoint: parse args, dispatch to check, format output
internal/
  check/
    check.go             # Check interface + Result type
    cpu.go               # CPU usage check
    memory.go            # Memory usage check
    disk.go              # Disk usage check
    services.go          # Talos system service health check
    etcd.go              # Etcd cluster health check
    load.go              # Load average check
    registry.go          # Check registry (name -> factory)
  threshold/
    threshold.go         # Nagios-style threshold parsing and evaluation
  talos/
    client.go            # Talos gRPC client wrapper (connection, auth, lifecycle)
  output/
    nagios.go            # Nagios output formatter (perfdata, exit codes, multi-line)
go.mod
go.sum
Makefile
```

### Package responsibilities

| Package | Role |
|---|---|
| `cmd/check-talos` | CLI entrypoint. Parses arguments with `go-arg`, instantiates Talos client, dispatches to the requested check, formats output, exits with Nagios code. |
| `internal/check` | Defines the `Check` interface and concrete implementations (CPU, memory, disk, services, etcd, load). Each check knows how to query the Talos API and return a structured `Result`. |
| `internal/threshold` | Parses Nagios-standard threshold ranges (`-w 80 -c 90`, `@10:20`, `~:100`, etc.) and evaluates a metric value against them. Standalone, no Talos dependency. |
| `internal/talos` | Thin wrapper around the official `talos/machinery` gRPC client. Handles mTLS setup, connection lifecycle, and context deadlines. Exposes typed helper methods used by checks. |
| `internal/output` | Builds Nagios-compliant plugin output: status line, optional long text, performance data. Handles `OK`, `WARNING`, `CRITICAL`, `UNKNOWN` formatting. |

### Why this layout

- **Single binary** — Nagios expects one binary per check. Subcommands inside one binary are simpler to distribute and version than N separate binaries.
- **`internal/`** — Prevents consumers from importing internals; the binary is the only public contract.
- **Separation of threshold logic** — Thresholds are pure functions (parse string, evaluate float). Keeping them isolated makes them trivially testable and reusable.
- **Talos client wrapper** — Insulates checks from gRPC boilerplate and authentication details. If the Talos API changes between versions, only this package needs updating.

---

## 2. CLI Interface

### 2.1 Invocation pattern

```
check-talos [global-flags] <check-name> [check-flags]
```

Exactly **one subcommand** per execution. Nagios forks a new process for every check interval — there is no daemon mode, no batch execution, no multiplexing. One process, one check, one exit code.

### 2.2 Global flags

| Flag | Short | Type | Required | Default | Description |
|---|---|---|---|---|---|
| `--talos-endpoint` | `-e` | `string` | conditional | *(none)* | Talos API endpoint (`host:port`). Required unless `--talosconfig` is provided. |
| `--talos-ca` | | `string` | conditional | *(none)* | Path to Talos CA certificate. Required unless `--talosconfig` is provided. |
| `--talos-cert` | | `string` | conditional | *(none)* | Path to client certificate. Required unless `--talosconfig` is provided. |
| `--talos-key` | | `string` | conditional | *(none)* | Path to client private key. Required unless `--talosconfig` is provided. |
| `--talosconfig` | | `string` | conditional | *(none)* | Path to talosconfig file. Alternative to explicit cert paths. |
| `--talos-context` | | `string` | no | *(default context in talosconfig)* | Named context within talosconfig to use. Ignored if `--talosconfig` is not set. |
| `--timeout` | `-t` | `duration` | no | `10s` | gRPC call timeout. Governs context deadline. |
| `--node` | `-n` | `string` | no | *(none)* | Target node hostname or IP. Sets gRPC metadata for apid proxy routing. |

**Authentication precedence:**

1. If `--talos-ca`, `--talos-cert`, and `--talos-key` are all provided → use explicit cert paths (ignore `--talosconfig`).
2. If `--talosconfig` is provided (and explicit cert paths are not) → parse the file, select `--talos-context` (or the default context), extract endpoints and credentials.
3. If neither is provided → exit `UNKNOWN (3)` with message: `TALOS UNKNOWN - No authentication configured. Provide --talos-ca/--talos-cert/--talos-key or --talosconfig`.
4. If explicit paths are partially provided (e.g. `--talos-ca` without `--talos-key`) → exit `UNKNOWN (3)` with message listing the missing flags.

**Endpoint resolution:**

- With explicit cert paths: `--talos-endpoint` is required. If missing → exit `UNKNOWN (3)`.
- With `--talosconfig`: endpoints come from the config file. `--talos-endpoint` can override. If talosconfig has no endpoints and `--talos-endpoint` is missing → exit `UNKNOWN (3)`.

### 2.3 Check subcommands and their flags

**`check-talos cpu`**

| Flag | Short | Type | Default | Description |
|---|---|---|---|---|
| `--warning` | `-w` | `string` | `80` | Warning threshold (Nagios range, %) |
| `--critical` | `-c` | `string` | `90` | Critical threshold (Nagios range, %) |

**`check-talos memory`**

| Flag | Short | Type | Default | Description |
|---|---|---|---|---|
| `--warning` | `-w` | `string` | `80` | Warning threshold (Nagios range, %) |
| `--critical` | `-c` | `string` | `90` | Critical threshold (Nagios range, %) |

**`check-talos disk`**

| Flag | Short | Type | Default | Description |
|---|---|---|---|---|
| `--warning` | `-w` | `string` | `80` | Warning threshold (Nagios range, %) |
| `--critical` | `-c` | `string` | `90` | Critical threshold (Nagios range, %) |
| `--mount` | `-m` | `string` | `/` | Mount point to check |

**`check-talos services`**

| Flag | Short | Type | Default | Description |
|---|---|---|---|---|
| `--exclude` | | `[]string` | *(none)* | Service IDs to ignore (repeatable) |
| `--include` | | `[]string` | *(none)* | Only check these service IDs (all others ignored) |

No `-w`/`-c` thresholds — this check is binary: every monitored service must be `Running` and `Healthy`, otherwise CRITICAL. There is no meaningful "warning" state for a service that is down.

**`check-talos etcd`**

| Flag | Short | Type | Default | Description |
|---|---|---|---|---|
| `--warning` | `-w` | `string` | `~:100000000` | Warning threshold for DB size in bytes (~100 MB) |
| `--critical` | `-c` | `string` | `~:200000000` | Critical threshold for DB size in bytes (~200 MB) |
| `--min-members` | | `int` | `3` | Minimum expected etcd member count (CRITICAL if below) |

This check verifies: (1) etcd is reachable, (2) a leader exists, (3) member count >= `--min-members`, (4) DB size within thresholds. Any structural failure (no leader, members below minimum) is always CRITICAL regardless of thresholds.

**`check-talos load`**

| Flag | Short | Type | Default | Description |
|---|---|---|---|---|
| `--warning` | `-w` | `string` | *(auto: CPU count)* | Warning threshold (raw load average) |
| `--critical` | `-c` | `string` | *(auto: 2 x CPU count)* | Critical threshold (raw load average) |
| `--period` | | `string` | `5` | Load average period: `1`, `5`, or `15` (minutes) |

Thresholds apply to **raw load average**, not per-CPU normalized values. Defaults are computed at runtime from the CPU count returned by `SystemStat`: warning = N CPUs, critical = 2N CPUs. A 4-core node defaults to `-w 4 -c 8`. Users can override with fixed values.

### 2.4 go-arg modeling

`go-arg` supports subcommands natively. The top-level struct holds global flags and embeds subcommand structs:

```
Args
├── Cpu      *CpuCmd      `arg:"subcommand:cpu"`
├── Mem      *MemCmd       `arg:"subcommand:memory"`
├── Disk     *DiskCmd      `arg:"subcommand:disk"`
├── Services *ServicesCmd  `arg:"subcommand:services"`
├── Etcd     *EtcdCmd      `arg:"subcommand:etcd"`
├── Load     *LoadCmd      `arg:"subcommand:load"`
├── Endpoint string        `arg:"-e,--talos-endpoint"`
├── CA       string        `arg:"--talos-ca"`
├── Cert     string        `arg:"--talos-cert"`
├── Key      string        `arg:"--talos-key"`
├── Config   string        `arg:"--talosconfig"`
├── Context  string        `arg:"--talos-context"`
├── Timeout  duration      `arg:"-t,--timeout"`
└── Node     string        `arg:"-n,--node"`
```

When `args.Cpu != nil`, we know the user invoked `check-talos cpu`.  
When all subcommand pointers are nil, no check was requested → validation error.

### 2.5 Validation rules

Validation happens **before** any network call. All failures exit `UNKNOWN (3)`.

| # | Rule | Error message |
|---|---|---|
| V1 | Exactly one subcommand must be specified | `TALOS UNKNOWN - No check specified. Usage: check-talos <cpu\|memory\|disk\|services\|etcd\|load> [flags]` |
| V2 | Authentication must be fully configured (see precedence in 2.2) | `TALOS UNKNOWN - No authentication configured. Provide --talos-ca/--talos-cert/--talos-key or --talosconfig` |
| V3 | If explicit cert paths: all three of `--talos-ca`, `--talos-cert`, `--talos-key` must be present | `TALOS UNKNOWN - Incomplete cert auth: missing --talos-key` (names the missing flag(s)) |
| V4 | Cert/key/CA files must exist and be readable | `TALOS UNKNOWN - Cannot read --talos-ca: /etc/talos/ca.crt: no such file or directory` |
| V5 | Endpoint must be resolvable (either explicit or from talosconfig) | `TALOS UNKNOWN - No endpoint configured. Provide --talos-endpoint or use --talosconfig` |
| V6 | `--timeout` must be > 0 and <= 120s | `TALOS UNKNOWN - Invalid timeout "0s": must be between 1s and 120s` |
| V7 | Threshold strings (`-w`, `-c`) must parse as valid Nagios ranges | `TALOS UNKNOWN - Invalid warning threshold "abc": expected Nagios range format` |
| V8 | Warning threshold must not be wider than critical (soft warning to stderr, not an error — Nagios convention allows it) | *(stderr only)* `Warning: -w range is wider than -c range` |
| V9 | `services --include` and `--exclude` are mutually exclusive | `TALOS UNKNOWN - Cannot use both --include and --exclude` |
| V10 | `load --period` must be one of `1`, `5`, `15` | `TALOS UNKNOWN - Invalid --period "10": must be 1, 5, or 15` |
| V11 | `etcd --min-members` must be >= 1 | `TALOS UNKNOWN - Invalid --min-members "0": must be >= 1` |
| V12 | `disk --mount` must start with `/` | `TALOS UNKNOWN - Invalid --mount "var": must be an absolute path` |

**Validation order:** V1 → V2/V3 → V4 → V5 → V6 → V7–V12 (subcommand-specific). First failure aborts; no accumulation of errors.

### 2.6 Default values summary

| Flag | Default | Rationale |
|---|---|---|
| `--timeout` | `10s` | Generous for mTLS handshake + one RPC; short enough that Nagios won't kill the process (default `check_timeout` is 60s) |
| `--node` | *(unset)* | When absent, the gRPC call targets whichever node the endpoint resolves to |
| `cpu -w` | `80` | Industry-standard warning for CPU utilization |
| `cpu -c` | `90` | Leave 10% headroom before hard saturation |
| `memory -w` | `80` | Same reasoning as CPU |
| `memory -c` | `90` | Same reasoning as CPU |
| `disk -w` | `80` | Disk fills non-linearly; 80% gives time to act |
| `disk -c` | `90` | At 90%, many filesystems degrade (reserved blocks, journal) |
| `disk --mount` | `/` | Root filesystem is the most common check target |
| `etcd -w` | `~:100000000` (~100 MB) | Etcd docs recommend compaction well before 2 GB; 100 MB is conservative warning |
| `etcd -c` | `~:200000000` (~200 MB) | 200 MB signals compaction is overdue |
| `etcd --min-members` | `3` | Standard etcd quorum for a 3-node control plane |
| `load -w` | *(auto: N CPUs)* | Load == CPU count means all cores are saturated on average |
| `load -c` | *(auto: 2N CPUs)* | 2x CPU count means significant scheduling backlog |
| `load --period` | `5` | 5-minute average smooths transient spikes while still catching sustained load |

### 2.7 Failure behavior

**On invalid arguments (validation fails):**

- Print single-line Nagios-format message to **stdout**: `TALOS <CHECK> UNKNOWN - <reason>`
- Exit code: `3` (UNKNOWN)
- No performance data
- No network calls attempted

**On connection failure (post-validation):**

- Print single-line message: `TALOS <CHECK> CRITICAL - <reason>` (e.g., `connection refused`, `TLS handshake failed`)
- Exit code: `2` (CRITICAL) — an unreachable node is an actionable alert
- No performance data

**On API error (connected but RPC fails):**

- gRPC `DeadlineExceeded` → `CRITICAL (2)`: node likely unhealthy
- gRPC `Unavailable` → `CRITICAL (2)`: node down
- gRPC `Unimplemented` → `UNKNOWN (3)`: API version mismatch
- gRPC `PermissionDenied` → `UNKNOWN (3)`: cert lacks required role
- All other gRPC errors → `UNKNOWN (3)`

**On unexpected panic:**

- `go-nagios` installs a panic handler that catches the panic, prints `TALOS <CHECK> CRITICAL - Internal error: <panic message>`, and exits `2`
- This prevents Nagios from seeing a raw stack trace

**On `--help`:**

- Print usage and exit `3` (UNKNOWN). This is Nagios convention — the check didn't run, so the exit code must be non-zero. `go-arg` handles this natively.

### 2.8 Example usage

**Direct invocation with explicit cert paths:**

```bash
# CPU check with custom thresholds
check-talos -e 10.0.0.1:50000 \
  --talos-ca /etc/talos/ca.crt \
  --talos-cert /etc/talos/admin.crt \
  --talos-key /etc/talos/admin.key \
  cpu -w 75 -c 90

# Disk check for /var mount
check-talos -e 10.0.0.1:50000 \
  --talos-ca /etc/talos/ca.crt \
  --talos-cert /etc/talos/admin.crt \
  --talos-key /etc/talos/admin.key \
  disk -m /var -w 85 -c 95

# Service health (exclude optional services)
check-talos -e 10.0.0.1:50000 \
  --talos-ca /etc/talos/ca.crt \
  --talos-cert /etc/talos/admin.crt \
  --talos-key /etc/talos/admin.key \
  services --exclude apid --exclude trustd

# Etcd health (5-node cluster, strict DB size limits)
check-talos -e 10.0.0.1:50000 \
  --talos-ca /etc/talos/ca.crt \
  --talos-cert /etc/talos/admin.crt \
  --talos-key /etc/talos/admin.key \
  etcd --min-members 5 -w ~:50000000 -c ~:100000000

# Load average (15-min period, custom thresholds)
check-talos -e 10.0.0.1:50000 \
  --talos-ca /etc/talos/ca.crt \
  --talos-cert /etc/talos/admin.crt \
  --talos-key /etc/talos/admin.key \
  load --period 15 -w 8 -c 16
```

**Using talosconfig (admin workstation or nodes with talosconfig deployed):**

```bash
# Memory check using default context from talosconfig
check-talos --talosconfig /etc/talos/config \
  -n worker-01 \
  memory

# Etcd check using a specific named context
check-talos --talosconfig /var/lib/nagios/talosconfig \
  --talos-context production \
  -n cp-01 \
  etcd

# Override endpoint from talosconfig
check-talos --talosconfig /etc/talos/config \
  -e 10.0.0.100:50000 \
  -n worker-03 \
  disk -m /var
```

**Targeting a specific node through a control-plane load balancer:**

```bash
# The LB at 10.0.0.100 routes to control plane apid;
# --node tells apid to proxy the request to worker-07
check-talos -e 10.0.0.100:50000 \
  --talos-ca /etc/talos/ca.crt \
  --talos-cert /etc/talos/admin.crt \
  --talos-key /etc/talos/admin.key \
  -n worker-07 \
  cpu
```

### 2.9 Nagios integration examples

**Command definitions** (`/etc/nagios/objects/commands.cfg`):

```cfg
# Generic check-talos command with per-check arguments
define command {
    command_name    check_talos
    command_line    /usr/local/bin/check-talos \
                        -e $ARG1$ \
                        --talos-ca /etc/nagios/talos/ca.crt \
                        --talos-cert /etc/nagios/talos/admin.crt \
                        --talos-key /etc/nagios/talos/admin.key \
                        -n $HOSTADDRESS$ \
                        $ARG2$ $ARG3$
}

# Convenience wrappers for common checks
define command {
    command_name    check_talos_cpu
    command_line    /usr/local/bin/check-talos \
                        -e $ARG1$ \
                        --talos-ca /etc/nagios/talos/ca.crt \
                        --talos-cert /etc/nagios/talos/admin.crt \
                        --talos-key /etc/nagios/talos/admin.key \
                        -n $HOSTADDRESS$ \
                        cpu -w $ARG2$ -c $ARG3$
}

define command {
    command_name    check_talos_disk
    command_line    /usr/local/bin/check-talos \
                        -e $ARG1$ \
                        --talos-ca /etc/nagios/talos/ca.crt \
                        --talos-cert /etc/nagios/talos/admin.crt \
                        --talos-key /etc/nagios/talos/admin.key \
                        -n $HOSTADDRESS$ \
                        disk -m $ARG2$ -w $ARG3$ -c $ARG4$
}

define command {
    command_name    check_talos_etcd
    command_line    /usr/local/bin/check-talos \
                        -e $ARG1$ \
                        --talos-ca /etc/nagios/talos/ca.crt \
                        --talos-cert /etc/nagios/talos/admin.crt \
                        --talos-key /etc/nagios/talos/admin.key \
                        -n $HOSTADDRESS$ \
                        etcd --min-members $ARG2$ -w $ARG3$ -c $ARG4$
}

# Using talosconfig instead of explicit cert paths
define command {
    command_name    check_talos_tc
    command_line    /usr/local/bin/check-talos \
                        --talosconfig /etc/nagios/talos/config \
                        --talos-context $ARG1$ \
                        -n $HOSTADDRESS$ \
                        $ARG2$ $ARG3$
}
```

**Service definitions** (`/etc/nagios/objects/talos-services.cfg`):

```cfg
# CPU check on all Talos nodes (workers + control plane)
define service {
    use                     generic-service
    hostgroup_name          talos-nodes
    service_description     Talos CPU Usage
    check_command           check_talos_cpu!10.0.0.100:50000!80!90
    check_interval          5
    retry_interval          1
    max_check_attempts      3
}

# Memory check with tighter thresholds for workers
define service {
    use                     generic-service
    hostgroup_name          talos-workers
    service_description     Talos Memory Usage
    check_command           check_talos!10.0.0.100:50000!memory!-w 75 -c 85
    check_interval          5
    retry_interval          1
    max_check_attempts      3
}

# Disk check for /var on all nodes
define service {
    use                     generic-service
    hostgroup_name          talos-nodes
    service_description     Talos Disk /var
    check_command           check_talos_disk!10.0.0.100:50000!/var!80!90
    check_interval          15
    retry_interval          5
    max_check_attempts      2
}

# Etcd health — control plane only
define service {
    use                     critical-service
    hostgroup_name          talos-controlplane
    service_description     Talos Etcd Health
    check_command           check_talos_etcd!10.0.0.100:50000!3!~:100000000!~:200000000
    check_interval          2
    retry_interval          1
    max_check_attempts      2
}

# Service health — all nodes
define service {
    use                     generic-service
    hostgroup_name          talos-nodes
    service_description     Talos Services
    check_command           check_talos!10.0.0.100:50000!services!
    check_interval          3
    retry_interval          1
    max_check_attempts      2
}

# Load average — all nodes, defaults auto-computed from CPU count
define service {
    use                     generic-service
    hostgroup_name          talos-nodes
    service_description     Talos Load Average
    check_command           check_talos!10.0.0.100:50000!load!
    check_interval          5
    retry_interval          2
    max_check_attempts      3
}
```

**Host definition pattern** (`/etc/nagios/objects/talos-hosts.cfg`):

```cfg
define host {
    use             linux-server
    host_name       talos-worker-01
    alias           Talos Worker 01
    address         10.0.1.11
    hostgroups      talos-nodes,talos-workers
}

define host {
    use             linux-server
    host_name       talos-cp-01
    alias           Talos Control Plane 01
    address         10.0.1.1
    hostgroups      talos-nodes,talos-controlplane
}

define hostgroup {
    hostgroup_name  talos-nodes
    alias           All Talos Nodes
}

define hostgroup {
    hostgroup_name  talos-workers
    alias           Talos Worker Nodes
}

define hostgroup {
    hostgroup_name  talos-controlplane
    alias           Talos Control Plane Nodes
}
```

### 2.10 Expected output format

All output follows the Nagios plugin output specification:

```
TALOS <CHECK> <STATUS> - <summary> | <perfdata>
<optional long text>
```

**Examples of expected stdout:**

```
TALOS CPU OK - CPU usage 34.2% | cpu_usage=34.2%;80;90;0;100
TALOS CPU WARNING - CPU usage 82.5% | cpu_usage=82.5%;80;90;0;100
TALOS MEMORY CRITICAL - Memory usage 94.1% (7.53 GB / 8.00 GB) | memory_usage=94.1%;80;90;0;100 memory_used=8083886080B;;;0;8589934592
TALOS DISK OK - / usage 45.0% (9.0 GB / 20.0 GB) | disk_usage=45.0%;80;90;0;100 disk_used=9663676416B;;;0;21474836480
TALOS SERVICES OK - 8/8 services healthy
TALOS SERVICES CRITICAL - 1/8 services unhealthy: kubelet (state=Finished, health=unhealthy: "readiness probe failed")
TALOS ETCD OK - Leader 1234, 3/3 members, DB 12.5 MB | etcd_dbsize=13107200B;100000000;200000000;0; etcd_members=3;;;0;
TALOS ETCD CRITICAL - No leader elected | etcd_dbsize=45000000B;100000000;200000000;0; etcd_members=3;;;0;
TALOS LOAD OK - Load average (5m) 1.23 | load5=1.23;4;8;0;
TALOS LOAD WARNING - Load average (5m) 4.56 | load5=4.56;4;8;0;
TALOS CPU UNKNOWN - Invalid warning threshold "abc": expected Nagios range format
TALOS DISK CRITICAL - Talos API timeout after 10s
```

---

## 3. Generic Check Abstraction

### Core interface

```go
type Result struct {
    Status   nagios.Status     // OK, WARNING, CRITICAL, UNKNOWN
    Summary  string            // One-line status text
    Details  string            // Optional multi-line details
    PerfData []nagios.PerfDatum // Nagios performance data
}

type Check interface {
    // Name returns the check identifier (used in output prefix)
    Name() string

    // Run executes the check against the Talos API and returns a Result.
    // The context carries the gRPC deadline.
    Run(ctx context.Context, client *talos.Client) (*Result, error)
}
```

### Why an interface and not just functions

- **Stateful construction** — Each check carries its parsed thresholds and config (e.g., mount point for disk). The struct implementing `Check` is built once from CLI flags, then `Run()` is called.
- **Testability** — You can unit-test each check by injecting a mock `talos.Client` that returns canned gRPC responses.
- **Registry pattern** — A `map[string]func(args) Check` registry lets us add new checks without modifying the dispatcher.

### Result → Nagios mapping

The `Result` struct is the boundary between check logic and output formatting. The `output` package takes a `Result` and:
1. Prints `CHECK_NAME STATUS - Summary | perfdata`
2. Optionally appends `Details` as long text
3. Calls `os.Exit(Status.ExitCode())`

---

## 4. Nagios Output and Exit Code Strategy

This section defines the complete output contract between the plugin and Nagios. Every invocation must produce deterministic, parseable output on stdout and exit with the correct code. No exceptions.

### 4.1 Exit codes

| Code | Status | When used |
|---|---|---|
| `0` | OK | Metric within acceptable range; all assertions pass |
| `1` | WARNING | Metric exceeds warning threshold but not critical |
| `2` | CRITICAL | Metric exceeds critical threshold; connectivity failure; structural assertion failure (no etcd leader, service down) |
| `3` | UNKNOWN | Configuration error; invalid arguments; API version mismatch; cannot determine node state |

**Evaluation precedence:** Critical is always evaluated before warning. If both thresholds are violated, the exit code is `2` (CRITICAL), never `1`.

### 4.2 Status line format

Every plugin invocation produces exactly one status line on stdout:

```
TALOS <CHECK> <STATUS> - <summary> | <perfdata>
```

| Component | Description | Example |
|---|---|---|
| `TALOS` | Fixed prefix identifying the plugin family | `TALOS` |
| `<CHECK>` | Uppercase check name: `CPU`, `MEMORY`, `DISK`, `SERVICES`, `ETCD`, `LOAD` | `CPU` |
| `<STATUS>` | One of: `OK`, `WARNING`, `CRITICAL`, `UNKNOWN` | `WARNING` |
| `<summary>` | Human-readable one-line description of current state | `CPU usage 82.5%` |
| `<perfdata>` | Nagios performance data (after the pipe). Optional. | `cpu_usage=82.5%;80;90;0;100` |

**Rules:**

- The pipe `|` separating summary from perfdata has exactly one space before and one space after it.
- If there is no performance data, the pipe and everything after it are omitted entirely.
- The status line must not exceed 4096 bytes (Nagios truncation limit in default configurations).
- Output ends with exactly one newline character.

### 4.3 Multi-line output (long text)

For checks that produce detailed diagnostics beyond the status line, additional lines follow immediately:

```
TALOS <CHECK> <STATUS> - <summary> | <perfdata>
<long text line 1>
<long text line 2>
...
```

Nagios displays only the first line in the "Status Information" column. Long text is visible in the extended service detail view and in notification emails/messages.

**When long text is used:**

- **services**: Lists each unhealthy service with its state, health status, and last message.
- **etcd**: Lists member details and alarm descriptions when structural assertions fail.
- **All checks**: May include diagnostic detail on API errors when partial data was retrieved.
- Long text is never emitted for OK results.

### 4.4 Performance data format

Performance data follows the Nagios plugin development guidelines:

```
'label'=value[UOM];[warn];[crit];[min];[max]
```

| Field | Description |
|---|---|
| `label` | Metric name. Lowercase, underscore-separated. Single-quoted only if it contains spaces (ours do not). |
| `value` | Numeric value. No spaces. Decimal point allowed. |
| `UOM` | Unit of measurement: `%` (percentage), `B` (bytes), `s` (seconds), `c` (counter), or empty (dimensionless). |
| `warn` | Warning threshold as passed to `-w`. May be empty if no threshold applies to this metric. |
| `crit` | Critical threshold as passed to `-c`. May be empty if no threshold applies to this metric. |
| `min` | Minimum possible value. May be empty if unbounded. |
| `max` | Maximum possible value. May be empty if unbounded. |

**Formatting rules:**

- Multiple perfdata items are separated by a single space.
- Semicolons are always present as field delimiters, even when trailing fields are empty (e.g., `etcd_members=3;;;0;`).
- Byte values are always raw integers (never human-formatted as KB/MB/GB) with `B` UOM so that graphing tools (PNP4Nagios, Grafana, Graphite) can apply their own unit scaling.
- Percentage-based metrics use no UOM suffix — the label name implies the unit. The value is a bare float (e.g., `cpu_usage=34.2`).
- Warning and critical fields in perfdata reflect the threshold values as Nagios range strings when applicable.

### 4.5 Threshold-to-state mapping

Threshold evaluation follows strict Nagios conventions:

```
1. Parse -w and -c values as Nagios ranges
2. Retrieve the metric value from the Talos API
3. If the critical range is violated → CRITICAL (exit 2)
4. Else if the warning range is violated → WARNING (exit 1)
5. Else → OK (exit 0)
```

For checks with non-threshold assertions (services, etcd structural checks), assertions are evaluated **before** any threshold comparison. An assertion failure immediately produces CRITICAL regardless of metric thresholds.

**Combined evaluation order for etcd (most complex check):**

```
1. RPC failure (etcd not running) → UNKNOWN
2. No leader (leader == 0) → CRITICAL
3. Member count < --min-members → CRITICAL
4. Active alarms present → CRITICAL
5. DB size violates critical threshold → CRITICAL
6. DB size violates warning threshold → WARNING
7. All checks pass → OK
```

**Services evaluation (no thresholds):**

```
1. Enumerate monitored services (apply --include/--exclude filters)
2. For each service: state == "Running" AND (health.healthy OR health.unknown) → healthy
3. Any service not healthy → CRITICAL
4. All services healthy → OK
```

There is no WARNING state for services — a non-running service is always an immediate incident, not a degradation.

### 4.6 Error, timeout, and invalid data handling

| Scenario | Exit code | Perfdata? | Summary example |
|---|---|---|---|
| Invalid CLI arguments | 3 (UNKNOWN) | No | `TALOS CPU UNKNOWN - Invalid warning threshold "abc": expected Nagios range format` |
| Certificate file missing/unreadable | 3 (UNKNOWN) | No | `TALOS CPU UNKNOWN - Cannot read --talos-ca: /etc/talos/ca.crt: no such file or directory` |
| No authentication configured | 3 (UNKNOWN) | No | `TALOS CPU UNKNOWN - No authentication configured` |
| Connection refused | 2 (CRITICAL) | No | `TALOS CPU CRITICAL - Connection refused: 10.0.0.1:50000` |
| TLS handshake failure | 2 (CRITICAL) | No | `TALOS CPU CRITICAL - TLS handshake failed: certificate signed by unknown authority` |
| gRPC DeadlineExceeded | 2 (CRITICAL) | No | `TALOS DISK CRITICAL - Talos API timeout after 10s` |
| gRPC Unavailable | 2 (CRITICAL) | No | `TALOS MEMORY CRITICAL - Talos API unavailable: transport is closing` |
| gRPC PermissionDenied | 3 (UNKNOWN) | No | `TALOS CPU UNKNOWN - Permission denied: client certificate lacks admin role` |
| gRPC Unimplemented | 3 (UNKNOWN) | No | `TALOS CPU UNKNOWN - RPC not supported (API version mismatch?)` |
| Nil or empty API response | 3 (UNKNOWN) | No | `TALOS MEMORY UNKNOWN - Empty response from Talos API` |
| Mount point not found in response | 3 (UNKNOWN) | No | `TALOS DISK UNKNOWN - Mount point /data not found` |
| Division by zero (total capacity = 0) | 3 (UNKNOWN) | No | `TALOS DISK UNKNOWN - Invalid data: total capacity is zero for /` |
| Etcd RPC fails on worker node | 3 (UNKNOWN) | No | `TALOS ETCD UNKNOWN - EtcdStatus RPC failed: etcd not running on this node` |
| Unexpected panic | 2 (CRITICAL) | No | `TALOS CPU CRITICAL - Internal error: runtime error: index out of range` |

**Guiding principles:**

- **Never exit 0 on error.** If the plugin cannot determine the node's actual state, it exits UNKNOWN (3). If it can determine that something is definitively wrong (node unreachable, timeout), it exits CRITICAL (2).
- **WARNING (1) is exclusively for threshold breaches** where the check executed successfully and retrieved valid data. Errors never produce WARNING.
- **Perfdata is only emitted when valid metric data was retrieved.** Configuration errors and connection failures produce no perfdata. Structural assertion failures (etcd no leader) still emit perfdata when the metric data itself was successfully retrieved.

### 4.7 Per-check output specification

#### 4.7.1 CPU

**Perfdata labels:**

| Label | UOM | Description | min | max |
|---|---|---|---|---|
| `cpu_usage` | *(empty — value is %)* | Aggregate CPU utilization percentage | `0` | `100` |

**Summary format:** `CPU usage <value>%`

**Examples for each state:**

```
TALOS CPU OK - CPU usage 34.2% | cpu_usage=34.2%;80;90;0;100
TALOS CPU WARNING - CPU usage 82.5% | cpu_usage=82.5%;80;90;0;100
TALOS CPU CRITICAL - CPU usage 96.3% | cpu_usage=96.3%;80;90;0;100
TALOS CPU UNKNOWN - Invalid warning threshold "abc": expected Nagios range format
TALOS CPU CRITICAL - Talos API timeout after 10s
```

#### 4.7.2 Memory

**Perfdata labels:**

| Label | UOM | Description | min | max |
|---|---|---|---|---|
| `memory_usage` | *(empty — value is %)* | Memory utilization based on `memavailable` | `0` | `100` |
| `memory_used` | `B` | Absolute used bytes (`memtotal - memavailable`) | `0` | `<memtotal>` |
| `memory_total` | `B` | Total physical RAM in bytes | `0` | *(empty)* |

**Summary format:** `Memory usage <pct>% (<used_human> / <total_human>)`

Human-readable sizes in the summary use GB with one decimal (e.g., `7.53 GB`). Perfdata uses raw bytes.

**Examples for each state:**

```
TALOS MEMORY OK - Memory usage 62.3% (4.98 GB / 8.00 GB) | memory_usage=62.3%;80;90;0;100 memory_used=5348024320B;;;0;8589934592 memory_total=8589934592B;;;0;
TALOS MEMORY WARNING - Memory usage 83.7% (6.70 GB / 8.00 GB) | memory_usage=83.7%;80;90;0;100 memory_used=7193739264B;;;0;8589934592 memory_total=8589934592B;;;0;
TALOS MEMORY CRITICAL - Memory usage 94.1% (7.53 GB / 8.00 GB) | memory_usage=94.1%;80;90;0;100 memory_used=8083886080B;;;0;8589934592 memory_total=8589934592B;;;0;
TALOS MEMORY CRITICAL - Talos API unavailable: transport is closing
```

#### 4.7.3 Disk

**Perfdata labels:**

| Label | UOM | Description | min | max |
|---|---|---|---|---|
| `disk_usage` | *(empty — value is %)* | Disk utilization for the target mount | `0` | `100` |
| `disk_used` | `B` | Absolute used bytes (`size - available`) | `0` | `<size>` |
| `disk_total` | `B` | Total mount capacity in bytes | `0` | *(empty)* |

**Summary format:** `<mount> usage <pct>% (<used_human> / <total_human>)`

The mount point is included in the summary for disambiguation when monitoring multiple mounts on the same host.

**Examples for each state:**

```
TALOS DISK OK - / usage 45.0% (9.0 GB / 20.0 GB) | disk_usage=45.0%;80;90;0;100 disk_used=9663676416B;;;0;21474836480 disk_total=21474836480B;;;0;
TALOS DISK WARNING - /var usage 84.2% (42.1 GB / 50.0 GB) | disk_usage=84.2%;80;90;0;100 disk_used=45204377190B;;;0;53687091200 disk_total=53687091200B;;;0;
TALOS DISK CRITICAL - / usage 93.8% (18.8 GB / 20.0 GB) | disk_usage=93.8%;80;90;0;100 disk_used=20091567308B;;;0;21474836480 disk_total=21474836480B;;;0;
TALOS DISK UNKNOWN - Mount point /data not found
TALOS DISK CRITICAL - Talos API timeout after 10s
```

#### 4.7.4 Services

**Perfdata labels:**

| Label | UOM | Description | min | max |
|---|---|---|---|---|
| `services_total` | *(empty)* | Total number of monitored services | `0` | *(empty)* |
| `services_healthy` | *(empty)* | Count of healthy services | `0` | *(empty)* |
| `services_unhealthy` | *(empty)* | Count of unhealthy services | `0` | *(empty)* |

No warning/critical thresholds appear in perfdata — this check uses assertion-based logic, not threshold evaluation.

**Summary format:**

- OK: `<n>/<n> services healthy`
- CRITICAL: `<unhealthy>/<total> services unhealthy: <name1>, <name2>`

**Examples for each state:**

```
TALOS SERVICES OK - 8/8 services healthy | services_total=8;;;0; services_healthy=8;;;0; services_unhealthy=0;;;0;

TALOS SERVICES CRITICAL - 1/8 services unhealthy: kubelet | services_total=8;;;0; services_healthy=7;;;0; services_unhealthy=1;;;0;
kubelet: state=Finished, health=unhealthy, message="readiness probe failed"

TALOS SERVICES CRITICAL - 2/8 services unhealthy: kubelet, etcd | services_total=8;;;0; services_healthy=6;;;0; services_unhealthy=2;;;0;
kubelet: state=Finished, health=unhealthy, message="readiness probe failed"
etcd: state=Starting, health=unknown, message=""
```

Unhealthy service names are listed in the summary line (comma-separated). Per-service details (state, health, last message) appear as long text below the status line.

There is no WARNING state — any unhealthy service is CRITICAL.

#### 4.7.5 Etcd

**Perfdata labels:**

| Label | UOM | Description | min | max |
|---|---|---|---|---|
| `etcd_dbsize` | `B` | Database allocated size in bytes | `0` | *(empty)* |
| `etcd_dbsize_in_use` | `B` | Database actual data size (post-compaction) | `0` | *(empty)* |
| `etcd_members` | *(empty)* | Number of cluster members | `0` | *(empty)* |

Warning and critical thresholds appear only on `etcd_dbsize`. The other metrics are informational.

**Summary format:**

- OK/WARNING: `Leader <id>, <n>/<min> members, DB <size_human>`
- CRITICAL (structural): `No leader elected` / `Member count <n> below minimum <min>` / `Active alarm: <type>`
- CRITICAL (threshold): `Leader <id>, <n>/<min> members, DB <size_human>`

**Examples for each state:**

```
TALOS ETCD OK - Leader 1234, 3/3 members, DB 12.5 MB | etcd_dbsize=13107200B;100000000;200000000;0; etcd_dbsize_in_use=8388608B;;;0; etcd_members=3;;;0;

TALOS ETCD WARNING - Leader 1234, 3/3 members, DB 112.4 MB | etcd_dbsize=117878784B;100000000;200000000;0; etcd_dbsize_in_use=96468992B;;;0; etcd_members=3;;;0;

TALOS ETCD CRITICAL - No leader elected | etcd_dbsize=45000000B;100000000;200000000;0; etcd_dbsize_in_use=40000000B;;;0; etcd_members=3;;;0;

TALOS ETCD CRITICAL - Member count 2 below minimum 3 | etcd_dbsize=13107200B;100000000;200000000;0; etcd_dbsize_in_use=8388608B;;;0; etcd_members=2;;;0;

TALOS ETCD CRITICAL - Active alarm: NOSPACE | etcd_dbsize=2147483648B;100000000;200000000;0; etcd_dbsize_in_use=2000000000B;;;0; etcd_members=3;;;0;

TALOS ETCD UNKNOWN - EtcdStatus RPC failed: etcd not running on this node
```

Structural assertion failures still emit perfdata when the data was retrieved before the failure was detected. When the RPC itself fails, no perfdata is emitted.

#### 4.7.6 Load

**Perfdata labels:**

| Label | UOM | Description | min | max |
|---|---|---|---|---|
| `load1` | *(empty)* | 1-minute load average | `0` | *(empty)* |
| `load5` | *(empty)* | 5-minute load average | `0` | *(empty)* |
| `load15` | *(empty)* | 15-minute load average | `0` | *(empty)* |

All three load averages are always emitted for graphing regardless of which `--period` is selected. Warning and critical thresholds appear only on the perfdata label corresponding to the selected period.

**Summary format:** `Load average (<period>m) <value>`

**Examples for each state (4-core node, default thresholds w=4, c=8):**

```
TALOS LOAD OK - Load average (5m) 1.23 | load1=0.98;;;0; load5=1.23;4;8;0; load15=1.45;;;0;
TALOS LOAD WARNING - Load average (5m) 4.56 | load1=5.12;;;0; load5=4.56;4;8;0; load15=3.21;;;0;
TALOS LOAD CRITICAL - Load average (5m) 9.87 | load1=11.02;;;0; load5=9.87;4;8;0; load15=7.65;;;0;
TALOS LOAD OK - Load average (1m) 2.10 | load1=2.10;4;8;0; load5=1.85;;;0; load15=1.45;;;0;
TALOS LOAD CRITICAL - Talos API timeout after 10s
```

When `--period 1` is selected, thresholds move to `load1`; when `--period 15`, they move to `load15`.

### 4.8 Output consistency rules

1. **Prefix is always `TALOS <CHECK> <STATUS>`** — No deviation. Nagios regex-based notification filters, event handlers, and log parsers depend on this predictable prefix.

2. **One status line, always** — Every invocation produces exactly one status line on stdout, regardless of outcome. Even panics are caught and formatted as a valid status line by the `go-nagios` panic handler.

3. **Perfdata is emitted whenever valid metric data was retrieved** — If the API returned data but a structural assertion then failed (etcd no leader), perfdata is still included because the metric data itself is valid and useful for graphing. Perfdata is omitted only when the API was never reached (config errors, connection failures) or when the response was unparseable.

4. **Stderr is not used for monitoring output** — Nagios captures only stdout. Diagnostic warnings (e.g., "warning threshold wider than critical") may go to stderr for human debugging but never affect exit code or Nagios-visible output.

5. **Human-readable values in summary, machine-readable in perfdata** — The summary says `12.5 MB`; the perfdata says `13107200B`. The summary says `94.1% (7.53 GB / 8.00 GB)`; the perfdata says `memory_usage=94.1%;80;90;0;100`. This serves both the operator reading the Nagios web UI and the graphing tool ingesting perfdata.

6. **Exit code always matches the status word** — If the output says `CRITICAL`, exit code is `2`. If it says `WARNING`, exit code is `1`. There must never be a mismatch. The `go-nagios` library enforces this invariant.

7. **No output to stdout before the status line** — No banners, no debug output, no progress indicators. The first (and usually only) line of stdout is the status line. Violations cause Nagios to misparse the output.

---

## 5. Threshold Handling

### Nagios threshold range specification

Follow the [Nagios plugin development guidelines](https://nagios-plugins.org/doc/guidelines.html#THRESHOLDFORMAT):

| Notation | Meaning |
|---|---|
| `10` | Alert if value > 10 (i.e., outside 0..10) |
| `10:` | Alert if value < 10 |
| `~:10` | Alert if value > 10 (same as `10` but explicit "no lower bound") |
| `10:20` | Alert if value < 10 or > 20 |
| `@10:20` | Alert if value >= 10 and <= 20 (inside range) |

### Implementation approach

```go
type Threshold struct {
    Start    float64
    End      float64
    Inside   bool  // true = alert when INSIDE range (@)
    StartInf bool  // true = no lower bound (~)
}

func Parse(s string) (Threshold, error)
func (t Threshold) Violated(value float64) bool
```

### Evaluation flow

```
value 85, warning="80", critical="95"

1. Parse "80" → Threshold{Start:0, End:80, Inside:false}
   → Violated(85) = true (85 > 80, outside 0..80)
   → WARNING

2. Parse "95" → Threshold{Start:0, End:95, Inside:false}
   → Violated(85) = false (85 <= 95, inside 0..95)
   → not CRITICAL

Final status: WARNING
```

### Evaluation order

Critical is checked first. If critical is violated, status = CRITICAL. Else if warning is violated, status = WARNING. Else OK. This is standard Nagios behavior.

### Default thresholds

Each check type defines sensible defaults (80/90 for CPU/memory, 80/90 for disk). Users can override via `-w` and `-c`. Both flags are optional.

### Checks with non-standard threshold semantics

Not all checks fit the simple "metric vs. range" model:

**Services** — No thresholds at all. The check is boolean: all monitored services must be `Running` + `Healthy`. Any service not in that state is CRITICAL. This is intentional — a partially-running kubelet is not a "warning", it's an incident. The `--exclude`/`--include` flags control which services are evaluated, not severity.

**Etcd** — Hybrid model. DB size uses standard Nagios thresholds (`-w`/`-c`). But structural assertions (leader exists, member count >= minimum) are always CRITICAL — there is no useful "warning" for a leaderless etcd cluster. The check evaluates structural assertions first, then DB size thresholds.

**Load** — Standard thresholds, but with runtime-computed defaults. If the user doesn't supply `-w`/`-c`, the check queries `SystemStat` to get the CPU count and sets warning=N, critical=2N. If the user provides explicit values, those are used as-is (raw load values, not per-CPU normalized).

---

## 6. Talos API Authentication and Connection

### Authentication model

Talos uses **mutual TLS (mTLS)** exclusively. There is no token-based auth.  
Three files are required:
- **CA certificate** (`ca.crt`) — the Talos cluster's CA
- **Client certificate** (`admin.crt`) — an admin or reader certificate issued by the Talos CA
- **Client key** (`admin.key`) — corresponding private key

These are typically found in `talosconfig` (Talos's equivalent of kubeconfig).

### Connection setup

```
1. Load CA cert → x509 cert pool
2. Load client cert + key → tls.Certificate
3. Build tls.Config with mutual auth
4. Dial gRPC endpoint with transport credentials + context deadline
5. Return MachineServiceClient
```

### Key design decisions

**Why raw cert paths instead of parsing talosconfig?**

Trade-off:
- **Option A: Accept `--talosconfig` + `--context`** — More user-friendly for Talos admins. Requires parsing the talosconfig YAML format and extracting certs (possibly base64-inline).
- **Option B: Accept explicit cert paths** — Simpler, more predictable for Nagios integration where configs are managed by config management (Puppet/Ansible). Cert files on disk are the norm for Nagios.

**Recommendation: Support both.** Priority to explicit paths (they win if both provided). Add optional `--talosconfig` / `--talos-context` for convenience. This makes the plugin usable both from Nagios (explicit paths) and from an admin's workstation (talosconfig).

**Node targeting:**

The Talos API can be accessed either:
- Directly on each node's port 50000
- Via a load-balancer pointing to control plane nodes, with a `node` metadata header to target a specific machine

The `--node` flag sets the gRPC metadata header `node: <value>`, allowing a single endpoint to reach any node in the cluster.

**Connection pooling:**

Not needed. Each Nagios check invocation is a short-lived process: connect, call one RPC, print, exit. No persistent connections.

### Talos API RPCs used

All checks use the `MachineService` gRPC service (all RPCs are **server-streaming**, even for single-node responses):

| Check | RPC Method | Response data |
|---|---|---|
| CPU | `MachineService.SystemStat` | Per-CPU and aggregate cumulative CPU counters |
| Memory | `MachineService.Memory` | Full `/proc/meminfo` equivalent (48 fields) |
| Disk | `MachineService.Mounts` | Mount point capacity and available space |
| Services | `MachineService.ServiceList` | List of services with ID, state, health, events |
| Etcd | `MachineService.EtcdStatus` + `MachineService.EtcdMemberList` | DB size, leader ID, member list, raft indices, alarms |
| Load | `MachineService.LoadAvg` + `MachineService.SystemStat` | load1/5/15 + CPU count for default threshold computation |

### Detailed endpoint-to-metric mapping (Talos API v1.12)

#### CPU — `MachineService.SystemStat(google.protobuf.Empty) → SystemStatResponse`

The response wraps a `SystemStat` message with these fields:

| Field | Type | Description |
|---|---|---|
| `boot_time` | `uint64` | System boot timestamp (epoch seconds) |
| `cpu_total` | `CPUStat` | Aggregate CPU counters across all cores |
| `cpu` | `repeated CPUStat` | Per-core CPU counters (array length = CPU count) |
| `irq_total` | `uint64` | Total interrupt count |
| `irq` | `repeated uint64` | Per-IRQ counters |
| `context_switches` | `uint64` | Total context switches since boot |
| `process_created` | `uint64` | Total processes/threads created since boot |
| `process_running` | `uint64` | Currently running processes |
| `process_blocked` | `uint64` | Currently blocked processes |
| `soft_irq_total` | `uint64` | Total soft IRQ count |
| `soft_irq` | `SoftIRQStat` | Per-type soft IRQ counters |

`CPUStat` fields (all `uint64`, cumulative jiffies since boot):

| Field | Meaning |
|---|---|
| `user` | Time in user mode |
| `nice` | Time in user mode with low priority |
| `system` | Time in kernel mode |
| `idle` | Idle time |
| `iowait` | Time waiting for I/O |
| `irq` | Time servicing hardware interrupts |
| `soft_irq` | Time servicing soft interrupts |
| `steal` | Time stolen by hypervisor |
| `guest` | Time running virtual CPU for guest OS |
| `guest_nice` | Time running niced guest |

**Metric extraction logic:**

```
total    = user + nice + system + idle + iowait + irq + softirq + steal
active   = total - idle - iowait
usage_pct = (active / total) * 100
```

Since counters are cumulative since boot, a single sample gives the **average since boot**. For "current" usage, two samples with a delta are needed (see CPU measurement trade-off below).

**CPU count** is derived from `len(SystemStat.cpu)` — used by the `load` check to auto-compute default thresholds.

#### Memory — `MachineService.Memory(google.protobuf.Empty) → MemoryResponse`

The response wraps a `Memory` message containing a `MemInfo` struct (mirrors `/proc/meminfo`, all fields `uint64` in bytes):

| Key fields | Description |
|---|---|
| `memtotal` | Total physical RAM |
| `memfree` | Completely unused RAM |
| `memavailable` | Estimated available memory (accounts for reclaimable caches) |
| `buffers` | Kernel buffer cache |
| `cached` | Page cache (file-backed) |
| `swapcached` | Swap pages also in RAM |
| `swaptotal` | Total swap space |
| `swapfree` | Unused swap space |
| `active` | Recently used memory (hard to reclaim) |
| `inactive` | Not recently used (easily reclaimable) |
| `slab` | Kernel slab allocator |
| `sreclaimable` | Reclaimable slab memory |
| `sunreclaim` | Unreclaimable slab memory |
| `dirty` | Memory waiting to be written to disk |
| `hardwarecorrupted` | RAM detected as faulty by ECC |

*(48 fields total — full `/proc/meminfo` parity)*

**Metric extraction logic:**

```
used_pct = ((memtotal - memavailable) / memtotal) * 100
```

Using `memavailable` (not `memfree`) is critical — `memfree` ignores reclaimable buffers/caches and always looks artificially low.

#### Disk — `MachineService.Mounts(google.protobuf.Empty) → MountsResponse`

The response wraps a `Mounts` message containing repeated `MountStat`:

| Field | Type | Description |
|---|---|---|
| `filesystem` | `string` | Device or filesystem name |
| `size` | `uint64` | Total capacity in bytes |
| `available` | `uint64` | Available space in bytes |
| `mounted_on` | `string` | Mount point path |

**Metric extraction logic:**

```
used      = size - available
used_pct  = (used / size) * 100
```

The check filters by `--mount` flag (default `/`) by matching `mounted_on`. Talos Linux mounts include `/`, `/var`, `/system`, `/ephemeral`, and others.

**Note:** `DiskStats` (I/O counters) and `DiskUsage` (directory tree walk) are separate RPCs. The disk check uses only `Mounts` for capacity monitoring. `DiskStats` could power a future disk-io check.

#### Services — `MachineService.ServiceList(google.protobuf.Empty) → ServiceListResponse`

The response wraps a `ServiceList` message containing repeated `ServiceInfo`:

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Service name (e.g., `kubelet`, `etcd`, `apid`, `trustd`, `containerd`) |
| `state` | `string` | Current state: `Running`, `Finished`, `Starting`, `Pre`, `Waiting`, etc. |
| `events` | `ServiceEvents` | History of state transitions |
| `health` | `ServiceHealth` | Health check details |

`ServiceHealth` fields:

| Field | Type | Description |
|---|---|---|
| `unknown` | `bool` | Health check has not run / not applicable |
| `healthy` | `bool` | Service is healthy |
| `last_message` | `string` | Last health check output message |
| `last_change` | `google.protobuf.Timestamp` | When health status last changed |

**Evaluation logic:**

A service is considered healthy when `state == "Running"` AND (`health.healthy == true` OR `health.unknown == true`). The `unknown` case covers services that don't implement a health check endpoint (e.g., `containerd`).

Typical Talos services: `apid`, `containerd`, `cri`, `etcd`, `kubelet`, `machined`, `trustd`, `udevd`.

#### Etcd — `MachineService.EtcdStatus` + `MachineService.EtcdMemberList`

**`EtcdStatus(google.protobuf.Empty) → EtcdStatusResponse`**

Wraps an `EtcdStatus` message containing `EtcdMemberStatus`:

| Field | Type | Description |
|---|---|---|
| `member_id` | `uint64` | This member's ID |
| `leader` | `uint64` | Current leader's member ID (0 = no leader) |
| `raft_index` | `uint64` | Current raft log index |
| `raft_term` | `uint64` | Current raft term |
| `raft_applied_index` | `uint64` | Last applied raft index |
| `db_size` | `int64` | Total allocated DB size in bytes |
| `db_size_in_use` | `int64` | Actual data size in bytes (after compaction) |
| `is_learner` | `bool` | Member is a non-voting learner |
| `protocol_version` | `string` | Etcd protocol version |
| `storage_version` | `string` | Etcd storage backend version |
| `errors` | `repeated string` | Active errors on this member |

**`EtcdMemberList(EtcdMemberListRequest) → EtcdMemberListResponse`**

Wraps `EtcdMembers` containing repeated `EtcdMember`:

| Field | Type | Description |
|---|---|---|
| `id` | `uint64` | Member ID |
| `hostname` | `string` | Member hostname |
| `peer_urls` | `repeated string` | Peer communication URLs |
| `client_urls` | `repeated string` | Client-facing URLs |
| `is_learner` | `bool` | Non-voting learner flag |

**`EtcdAlarmList(google.protobuf.Empty) → EtcdAlarmListResponse`**

Returns active etcd alarms (NOSPACE, CORRUPT, etc.). Used for proactive alerting before cluster degradation.

**Evaluation logic:**

```
1. Call EtcdStatus → check leader != 0, check errors[] is empty
2. Call EtcdMemberList → check len(members) >= --min-members
3. Evaluate db_size against -w/-c thresholds
4. (Optional) Call EtcdAlarmList → any active alarm = CRITICAL
```

**Important:** These RPCs only succeed on **control plane nodes** where etcd runs. Calling on a worker node returns a gRPC error → mapped to UNKNOWN.

#### Load — `MachineService.LoadAvg(google.protobuf.Empty) → LoadAvgResponse`

The response wraps a `LoadAvg` message:

| Field | Type | Description |
|---|---|---|
| `load1` | `double` | 1-minute load average |
| `load5` | `double` | 5-minute load average |
| `load15` | `double` | 15-minute load average |

The `--period` flag selects which field to evaluate. Default thresholds are auto-computed from `len(SystemStat.cpu)` (requires a separate `SystemStat` call).

### Go client library reference

**Official library:** `github.com/siderolabs/talos/pkg/machinery` (MPL-2.0 license, v1.12.x)

| Import path | Purpose |
|---|---|
| `github.com/siderolabs/talos/pkg/machinery/client` | High-level gRPC client with mTLS, node targeting, config loading |
| `github.com/siderolabs/talos/pkg/machinery/client/config` | Talosconfig file parsing |
| `github.com/siderolabs/talos/pkg/machinery/api/machine` | Protobuf types for `MachineService` RPCs |
| `github.com/siderolabs/talos/pkg/machinery/api/storage` | Protobuf types for `StorageService` RPCs |
| `google.golang.org/grpc` | gRPC transport |

**Client struct** exposes typed service clients directly:

```go
type Client struct {
    MachineClient  machineapi.MachineServiceClient  // All monitoring RPCs
    TimeClient     timeapi.TimeServiceClient
    ClusterClient  clusterapi.ClusterServiceClient
    StorageClient  storageapi.StorageServiceClient
    // ...
}
```

**Client creation options:**

| Option | Description |
|---|---|
| `client.WithEndpoints(endpoints ...string)` | Set API endpoints (host:port) |
| `client.WithTLSConfig(tlsConfig *tls.Config)` | Explicit mTLS configuration |
| `client.WithConfigFromFile(path string)` | Load from talosconfig file |
| `client.WithContextName(name string)` | Select named context from talosconfig |
| `client.WithDefaultConfig()` | Use `~/.talos/config` |

**Node targeting:**

```go
ctx = client.WithNode(ctx, "10.0.0.5")  // Target specific node via apid proxy
```

**Convenience methods on Client** (unary-style wrappers over streaming RPCs):

| Method | Signature |
|---|---|
| `Memory()` | `func (c *Client) Memory(ctx) (*machineapi.MemoryResponse, error)` |
| `Mounts()` | `func (c *Client) Mounts(ctx) (*machineapi.MountsResponse, error)` |
| `ServiceList()` | `func (c *Client) ServiceList(ctx) (*machineapi.ServiceListResponse, error)` |
| `EtcdStatus()` | `func (c *Client) EtcdStatus(ctx) (*machineapi.EtcdStatusResponse, error)` |
| `EtcdMemberList()` | `func (c *Client) EtcdMemberList(ctx, req) (*machineapi.EtcdMemberListResponse, error)` |
| `EtcdAlarmList()` | `func (c *Client) EtcdAlarmList(ctx) (*machineapi.EtcdAlarmListResponse, error)` |
| `Version()` | `func (c *Client) Version(ctx) (*machineapi.VersionResponse, error)` |

**RPCs without convenience wrappers** (must use `c.MachineClient` directly):

| RPC | Call pattern |
|---|---|
| `SystemStat` | `c.MachineClient.SystemStat(ctx, &emptypb.Empty{})` → stream, call `Recv()` |
| `LoadAvg` | `c.MachineClient.LoadAvg(ctx, &emptypb.Empty{})` → stream, call `Recv()` |
| `DiskStats` | `c.MachineClient.DiskStats(ctx, &emptypb.Empty{})` → stream, call `Recv()` |
| `CPUInfo` | `c.MachineClient.CPUInfo(ctx, &emptypb.Empty{})` → stream, call `Recv()` |

For these, the stream always returns exactly one message per targeted node, then `io.EOF`.

### Available vs. missing metrics

**Fully available via Talos API:**

| Metric | Source RPC | Notes |
|---|---|---|
| CPU usage (%) | `SystemStat` | Cumulative counters; compute delta for "current" usage |
| CPU count | `SystemStat` | `len(cpu)` per-CPU array |
| Memory usage (%) | `Memory` | `memavailable` is the correct field |
| Memory breakdown | `Memory` | All 48 `/proc/meminfo` fields available |
| Disk capacity (%) | `Mounts` | Per-mount-point size and available |
| Service state & health | `ServiceList` | State + health check + events |
| Etcd leader / members | `EtcdStatus` + `EtcdMemberList` | Full cluster topology |
| Etcd DB size | `EtcdStatus` | Both allocated and in-use sizes |
| Etcd alarms | `EtcdAlarmList` | NOSPACE, CORRUPT detection |
| Load averages | `LoadAvg` | 1/5/15 minute |
| System uptime | `SystemStat` | `boot_time` field |
| Disk I/O counters | `DiskStats` | Read/write ops, sectors, time per device |
| Network counters | `NetworkDeviceStats` | Per-NIC packet/byte/error counters |
| Process count | `SystemStat` | Running + blocked counts |

**Not available via Talos API (must use alternative sources):**

| Metric | Why missing | Alternative |
|---|---|---|
| CPU usage as instant % | API gives cumulative counters only | Two-sample delta or Prometheus node_exporter |
| Per-process CPU/memory | `Processes` RPC exists but is heavyweight | Prometheus cAdvisor / kubelet metrics |
| Disk latency percentiles | Only total I/O time counters, no histograms | Prometheus node_exporter with `diskstats` |
| SMART disk health | Not exposed in the API | Prometheus `smartctl_exporter` |
| Temperature / hardware sensors | Not exposed | IPMI or `node_exporter` hwmon |
| NTP sync status / time drift | `TimeService.Time` gives server time, no drift info | Chrony metrics or `node_exporter` |
| Network bandwidth (%) | Raw counters only, no link speed for % calc | `node_exporter` with `ethtool` |
| GPU metrics | Not applicable to Talos | Specialized exporters |

### Polling pitfalls and limitations

**1. All monitoring RPCs are server-streaming.**
Even single-node queries return a gRPC stream. The client must call `Recv()` to get the response, then handle `io.EOF`. This adds minor complexity but no real overhead for single-node checks.

**2. CPU counters are cumulative since boot.**
A single `SystemStat` call returns total jiffies since boot. For a node up 30 days, this averages over the entire uptime. Options:
- **Single sample (default):** Acceptable for Nagios checks at 1–5 min intervals. If the node has been running normally, the cumulative average closely tracks recent usage.
- **Two-sample delta:** Call `SystemStat` twice with a 1–2s sleep, compute the delta. More accurate but adds latency to every check invocation.
- Recommendation: default to single sample; offer `--sample-duration` for two-sample mode.

**3. No server-side rate limiting.**
The Talos API has no built-in rate limiting or throttling. Polling at very high frequency (sub-second) will generate CPU and I/O load on the target node's `machined` and `apid` processes. For Nagios (typically 1–5 min intervals), this is a non-issue. Avoid deploying multiple independent monitoring systems polling the same node simultaneously at high frequency.

**4. mTLS handshake overhead.**
Each Nagios check invocation is a short-lived process: TLS handshake → gRPC call → exit. The mTLS handshake dominates execution time for fast RPCs. Mitigation: set a reasonable `--timeout` (default 10s); consider connection reuse if building a daemon-mode wrapper in the future.

**5. `DiskUsage` is expensive — avoid for capacity checks.**
`DiskUsage(DiskUsageRequest)` walks a directory tree and streams per-file sizes. This is the wrong RPC for "how full is `/var`?" — use `Mounts` instead (instant, no tree walk). Reserve `DiskUsage` for debugging or directory-level analysis only.

**6. Etcd RPCs only work on control plane nodes.**
`EtcdStatus` and `EtcdMemberList` call into the local etcd instance. On worker nodes (which don't run etcd), these RPCs return a gRPC error. The check must map this to UNKNOWN, not CRITICAL, and the operator must configure Nagios to only schedule `check-talos etcd` against control plane nodes.

**7. API requests go through `apid` proxy.**
All gRPC calls hit the `apid` service on the target node, which proxies to `machined`. If `apid` is unhealthy, no RPC will succeed — including `ServiceList` (so you can't diagnose the problem via the API). The timeout → CRITICAL mapping handles this case.

**8. Multi-node responses require metadata parsing.**
When targeting nodes through a control-plane LB with `--node` metadata, each response message includes a `common.Metadata` field with the responding node's hostname. The client library handles this transparently for single-node targeting, but multi-node queries require iterating over the streamed messages and correlating by `Metadata.hostname`.

**9. No subscription / push model.**
The API is strictly request-response (or server-streaming for bulk data). There is no watch/subscribe mechanism for monitoring metrics. Each check interval requires a full gRPC round-trip. This is fine for Nagios's polling model but means the API cannot replace Prometheus for continuous metric collection.

---

## 7. Error → Nagios Exit Code Mapping

### Principle

| Situation | Exit code | Rationale |
|---|---|---|
| Check ran successfully, value OK | `0` (OK) | Normal |
| Check ran successfully, value above warning | `1` (WARNING) | Threshold breach |
| Check ran successfully, value above critical | `2` (CRITICAL) | Threshold breach |
| Cannot connect to Talos API (timeout, refused, TLS error) | `2` (CRITICAL) | Node unreachable = critical problem |
| Invalid arguments / missing flags | `3` (UNKNOWN) | Configuration error |
| Unexpected API response (nil data, unknown format) | `3` (UNKNOWN) | Cannot determine state |
| Certificate file not found / unreadable | `3` (UNKNOWN) | Configuration error |
| gRPC deadline exceeded | `2` (CRITICAL) | Node likely unhealthy |
| Partial API response (multi-node, some failed) | `2` (CRITICAL) | At least one node has a problem |
| Service not in `Running` state | `2` (CRITICAL) | Service down is always actionable |
| Etcd has no leader | `2` (CRITICAL) | Leaderless cluster = data plane risk |
| Etcd member count below `--min-members` | `2` (CRITICAL) | Quorum at risk |
| Etcd DB size exceeds threshold | `1` or `2` | Standard threshold evaluation |
| Etcd RPC fails on worker node (no etcd) | `3` (UNKNOWN) | Check misconfigured — etcd only runs on control plane |

### Implementation

Errors are caught at two levels:

1. **In `main()`** — Argument parsing errors and client creation errors → print message → `exit(3)`
2. **In `Check.Run()`** — The method returns `(*Result, error)`. If `error != nil`, main maps it:
   - gRPC `Unavailable`, `DeadlineExceeded` → CRITICAL
   - Everything else → UNKNOWN

The key rule: **never exit 0 on error**. If we can't determine the state, it's UNKNOWN. If we know the node is unreachable, it's CRITICAL.

### Output on error

```
TALOS CPU UNKNOWN - Failed to connect to Talos API: connection refused
TALOS DISK CRITICAL - Talos API timeout after 10s
TALOS SERVICES CRITICAL - 1 service not running: kubelet (state: Finished)
TALOS ETCD CRITICAL - No leader elected
TALOS ETCD CRITICAL - Member count 2 below minimum 3
TALOS ETCD UNKNOWN - EtcdStatus RPC failed: etcd not running on this node
TALOS LOAD WARNING - Load average (5m) 4.21 exceeds threshold 4 | load5=4.21;4;8
```

Always prefix with the check name and status so Nagios can parse it.

---

## 8. Additional Checks Feasible via Talos API

The Talos `MachineService` gRPC API exposes significantly more than CPU/memory/disk. Here are realistic additional checks ordered by operational value:

### High value

| Check | RPC | What it monitors |
|---|---|---|
| **Node reboot required** | `Version` + `MachineConfig` | Compare running Talos version/config against desired. Detect config drift. |
| **System uptime** | `SystemStat` | Alert if uptime < N seconds (unexpected reboot detection). |

### Medium value

| Check | RPC | What it monitors |
|---|---|---|
| **Disk I/O** | `DiskStats` | Read/write throughput and IOPS. Detect I/O saturation. |
| **Network interfaces** | `NetworkDeviceStats` | Link status, error counters, packet drops per NIC. |
| **Processes** | `Processes` | Total process count, zombie process detection. |
| **Talos version** | `Version` | Alert if node is running an unexpected/outdated Talos version. |

### Lower priority but useful

| Check | RPC | What it monitors |
|---|---|---|
| **Containers** | `Containers` (CRI namespace) | Running container count, restart counts. |
| **Kernel logs** | `Dmesg` | Parse for OOM kills, hardware errors, filesystem errors. |
| **Mounts** | `Mounts` | Read-only filesystem detection, unexpected mount options. |
| **Time sync** | `NetworkDeviceStats` or system-level | NTP sync status (time drift is critical in distributed systems). |

### Implementation priority recommendation

Phase 1 (initial): **cpu, memory, disk, services, etcd, load**
Phase 2: **uptime, network, disk-io** (standard monitoring)
Phase 3: **version, processes, containers, dmesg** (nice-to-have)

---

## 9. Key Dependencies

| Dependency | Purpose | Module |
|---|---|---|
| `go-arg` | CLI argument parsing with subcommands | `github.com/alexflint/go-arg` |
| `go-nagios` | Nagios output formatting and exit codes | `github.com/atc0005/go-nagios` |
| `talos/machinery` | Official Talos gRPC client and protobuf types | `github.com/siderolabs/talos/pkg/machinery` |
| `grpc` | gRPC transport | `google.golang.org/grpc` |

### go-nagios vs. hand-rolled output

`go-nagios` (`atc0005/go-nagios`) provides:
- Proper exit code handling
- Performance data formatting
- Long plugin output support
- Panic recovery (exits CRITICAL instead of crashing)

This is preferable to hand-rolling because the Nagios output spec has subtle formatting rules (pipe separator, semicolons in perfdata, newline handling) that are easy to get wrong.

However, `go-nagios` uses a "plugin" object pattern that takes over `os.Exit`. Our `Check` interface should return a `Result`, and the `main()` function translates it into `go-nagios` calls. This keeps checks testable (no `os.Exit` in check logic).

---

## 10. Trade-offs and Design Decisions Summary

| Decision | Chosen approach | Alternative | Why |
|---|---|---|---|
| Single binary vs. multiple | Single binary + subcommands | One binary per check | Easier to distribute, version, and maintain |
| Threshold format | Nagios-standard ranges | Simple integer percentages | Industry standard, more flexible |
| CPU measurement | Single sample (cumulative) | Two-sample delta | Lower latency, acceptable for periodic checks |
| Auth config | Explicit cert paths (primary) + talosconfig (optional) | talosconfig only | Better for Nagios/config-management integration |
| Client abstraction | Thin wrapper | Direct gRPC in checks | Testability, single point of change for API upgrades |
| Output library | go-nagios | Custom formatter | Handles edge cases, well-tested |
| Error strategy | CRITICAL for connectivity, UNKNOWN for config | UNKNOWN for everything | Connectivity loss is an actionable alert |
| Service check severity | Always CRITICAL (no WARNING) | WARNING for degraded, CRITICAL for down | A non-running service is always an incident; no useful intermediate state |
| Etcd check model | Structural assertions + DB size thresholds | Thresholds only | No leader / missing members is categorical, not a gradient |
| Load default thresholds | Auto-computed from CPU count | Fixed defaults (e.g. 5/10) | A 2-core and a 64-core node have very different "normal" load |

---

## 11. Testing Strategy

- **`internal/threshold`** — Pure unit tests. No dependencies. Table-driven tests for all range formats.
- **`internal/check`** — Unit tests with a mock Talos client interface. Verify correct status for various metric values and error conditions. Specific scenarios per check:
  - **services**: all running → OK, one stopped → CRITICAL, excluded service stopped → OK, empty service list → UNKNOWN
  - **etcd**: healthy cluster → OK, no leader → CRITICAL, members below min → CRITICAL, DB size over threshold → WARNING/CRITICAL, etcd RPC fails → UNKNOWN
  - **load**: load below threshold → OK, auto-computed defaults match CPU count, explicit overrides respected, invalid `--period` → UNKNOWN
- **`internal/output`** — Unit tests verifying exact Nagios output format strings.
- **`internal/talos`** — Integration test (optional) against a real Talos node or a gRPC test server with canned responses.
- **`cmd/check-talos`** — End-to-end test: build binary, run with mock server, verify exit code and stdout.
