package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/DLAKE-IO/check-talos/internal/check"
	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/DLAKE-IO/check-talos/internal/talos"
	"github.com/DLAKE-IO/check-talos/internal/threshold"
	arg "github.com/alexflint/go-arg"
	nagios "github.com/atc0005/go-nagios"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CpuCmd defines flags for the cpu subcommand.
type CpuCmd struct {
	Warning  string `arg:"-w,--warning" default:"80" help:"Warning threshold (Nagios range, %)"`
	Critical string `arg:"-c,--critical" default:"90" help:"Critical threshold (Nagios range, %)"`
}

// MemCmd defines flags for the memory subcommand.
type MemCmd struct {
	Warning  string `arg:"-w,--warning" default:"80" help:"Warning threshold (Nagios range, %)"`
	Critical string `arg:"-c,--critical" default:"90" help:"Critical threshold (Nagios range, %)"`
}

// DiskCmd defines flags for the disk subcommand.
type DiskCmd struct {
	Warning  string `arg:"-w,--warning" default:"80" help:"Warning threshold (Nagios range, %)"`
	Critical string `arg:"-c,--critical" default:"90" help:"Critical threshold (Nagios range, %)"`
	Mount    string `arg:"-m,--mount" default:"/var" help:"Mount point to check"`
}

// ServicesCmd defines flags for the services subcommand.
type ServicesCmd struct {
	Exclude []string `arg:"--exclude,separate" help:"Service IDs to ignore (repeatable)"`
	Include []string `arg:"--include,separate" help:"Only check these service IDs (repeatable)"`
}

// EtcdCmd defines flags for the etcd subcommand.
type EtcdCmd struct {
	Warning    string `arg:"-w,--warning" default:"~:100000000" help:"Warning threshold for DB size in bytes"`
	Critical   string `arg:"-c,--critical" default:"~:200000000" help:"Critical threshold for DB size in bytes"`
	MinMembers int    `arg:"--min-members" default:"3" help:"Minimum expected etcd member count"`
}

// LoadCmd defines flags for the load subcommand.
type LoadCmd struct {
	Warning  string `arg:"-w,--warning" help:"Warning threshold (raw load average)"`
	Critical string `arg:"-c,--critical" help:"Critical threshold (raw load average)"`
	Period   string `arg:"--period" default:"5" help:"Load average period: 1, 5, or 15 (minutes)"`
}

// Args holds all CLI flags and subcommand pointers for check-talos.
// When a subcommand pointer is non-nil, that check was selected.
type Args struct {
	Cpu      *CpuCmd      `arg:"subcommand:cpu" help:"Check CPU usage"`
	Mem      *MemCmd      `arg:"subcommand:memory" help:"Check memory usage"`
	Disk     *DiskCmd     `arg:"subcommand:disk" help:"Check disk usage"`
	Services *ServicesCmd `arg:"subcommand:services" help:"Check Talos system service health"`
	Etcd     *EtcdCmd     `arg:"subcommand:etcd" help:"Check etcd cluster health"`
	Load     *LoadCmd     `arg:"subcommand:load" help:"Check load average"`

	Endpoint string        `arg:"-e,--talos-endpoint" help:"Talos API endpoint (host:port)"`
	CA       string        `arg:"--talos-ca" help:"Path to Talos CA certificate"`
	Cert     string        `arg:"--talos-cert" help:"Path to client certificate"`
	Key      string        `arg:"--talos-key" help:"Path to client private key"`
	Config   string        `arg:"--talosconfig" help:"Path to talosconfig file"`
	Context  string        `arg:"--talos-context" help:"Named context within talosconfig"`
	Timeout  time.Duration `arg:"-t,--timeout" default:"10s" help:"gRPC call timeout"`
	Node     string        `arg:"-n,--node" help:"Target node hostname or IP"`
}

// Description returns the program description for go-arg help output.
func (Args) Description() string {
	return "Nagios-compatible monitoring plugin for Talos Linux nodes"
}

func main() {
	plugin := nagios.NewPlugin()
	defer plugin.ReturnCheckResults()

	var args Args
	parser, err := arg.NewParser(arg.Config{Program: "check-talos"}, &args)
	if err != nil {
		plugin.ServiceOutput = fmt.Sprintf("TALOS UNKNOWN - Internal error: %s", err)
		plugin.ExitStatusCode = nagios.StateUNKNOWNExitCode
		return
	}

	if err := parser.Parse(os.Args[1:]); err != nil {
		switch {
		case errors.Is(err, arg.ErrHelp):
			// Nagios convention: --help exits UNKNOWN (3).
			parser.WriteHelp(os.Stdout)
			os.Exit(nagios.StateUNKNOWNExitCode)
		case errors.Is(err, arg.ErrVersion):
			os.Exit(nagios.StateUNKNOWNExitCode)
		default:
			plugin.ServiceOutput = fmt.Sprintf("TALOS UNKNOWN - %s", err)
			plugin.ExitStatusCode = nagios.StateUNKNOWNExitCode
			return
		}
	}

	// V1: Exactly one subcommand must be specified.
	if parser.Subcommand() == nil {
		plugin.ServiceOutput = "TALOS UNKNOWN - No check specified. Usage: check-talos <cpu|memory|disk|services|etcd|load> [flags]"
		plugin.ExitStatusCode = nagios.StateUNKNOWNExitCode
		return
	}

	checkName := resolveCheckName(&args)

	if err := validate(&args); err != nil {
		plugin.ServiceOutput = fmt.Sprintf("TALOS %s UNKNOWN - %s", checkName, err)
		plugin.ExitStatusCode = nagios.StateUNKNOWNExitCode
		return
	}

	// Create a context with the configured timeout for gRPC calls.
	ctx, cancel := context.WithTimeout(context.Background(), args.Timeout)
	defer cancel()

	// Create the Talos API client.
	talosClient, err := talos.NewClient(ctx, talos.Config{
		Endpoint:     args.Endpoint,
		CA:           args.CA,
		Cert:         args.Cert,
		Key:          args.Key,
		TalosConfig:  args.Config,
		TalosContext: args.Context,
		Node:         args.Node,
		Timeout:      args.Timeout,
	})
	if err != nil {
		result := mapGRPCError(checkName, err, args.Timeout)
		result.ApplyToPlugin(plugin)
		return
	}
	defer talosClient.Close()

	// Instantiate the check from CLI flags.
	var chk check.Check
	switch {
	case args.Cpu != nil:
		chk, err = check.NewCPUCheck(args.Cpu.Warning, args.Cpu.Critical)
	case args.Mem != nil:
		chk, err = check.NewMemoryCheck(args.Mem.Warning, args.Mem.Critical)
	case args.Disk != nil:
		chk, err = check.NewDiskCheck(args.Disk.Warning, args.Disk.Critical, args.Disk.Mount)
	case args.Services != nil:
		chk, err = check.NewServicesCheck(args.Services.Include, args.Services.Exclude)
	case args.Etcd != nil:
		chk, err = check.NewEtcdCheck(args.Etcd.Warning, args.Etcd.Critical, args.Etcd.MinMembers)
	case args.Load != nil:
		chk, err = check.NewLoadCheck(args.Load.Warning, args.Load.Critical, args.Load.Period)
	}
	if err != nil {
		plugin.ServiceOutput = fmt.Sprintf("TALOS %s UNKNOWN - %s", checkName, err)
		plugin.ExitStatusCode = nagios.StateUNKNOWNExitCode
		return
	}

	// Run the check against the Talos API.
	result, err := chk.Run(ctx, talosClient)
	if err != nil {
		errResult := mapGRPCError(checkName, err, args.Timeout)
		errResult.ApplyToPlugin(plugin)
		return
	}

	// Format the result and set exit code via go-nagios Plugin.
	result.ApplyToPlugin(plugin)
}

// resolveCheckName returns the uppercase check name for the selected subcommand.
func resolveCheckName(args *Args) string {
	switch {
	case args.Cpu != nil:
		return "CPU"
	case args.Mem != nil:
		return "MEMORY"
	case args.Disk != nil:
		return "DISK"
	case args.Services != nil:
		return "SERVICES"
	case args.Etcd != nil:
		return "ETCD"
	case args.Load != nil:
		return "LOAD"
	default:
		return "UNKNOWN"
	}
}

// validate implements validation rules V2–V12 from DESIGN.md Section 2.5.
// V1 (subcommand presence) is checked before this function is called.
// Validation stops at the first failure; errors are not accumulated.
func validate(args *Args) error {
	// V2/V3: Authentication must be configured.
	hasCA := args.CA != ""
	hasCert := args.Cert != ""
	hasKey := args.Key != ""
	hasExplicitCerts := hasCA || hasCert || hasKey
	hasConfig := args.Config != ""

	if hasExplicitCerts {
		// V3: All three cert paths must be present.
		var missing []string
		if !hasCA {
			missing = append(missing, "--talos-ca")
		}
		if !hasCert {
			missing = append(missing, "--talos-cert")
		}
		if !hasKey {
			missing = append(missing, "--talos-key")
		}
		if len(missing) > 0 {
			return fmt.Errorf("Incomplete cert auth: missing %s", strings.Join(missing, ", "))
		}
	} else if !hasConfig {
		// V2: No authentication at all.
		return fmt.Errorf("No authentication configured. Provide --talos-ca/--talos-cert/--talos-key or --talosconfig")
	}

	// V4: Certificate/key/config files must exist and be readable.
	if hasCA && hasCert && hasKey {
		if err := checkFileReadable("--talos-ca", args.CA); err != nil {
			return err
		}
		if err := checkFileReadable("--talos-cert", args.Cert); err != nil {
			return err
		}
		if err := checkFileReadable("--talos-key", args.Key); err != nil {
			return err
		}
	}
	if hasConfig {
		if err := checkFileReadable("--talosconfig", args.Config); err != nil {
			return err
		}
	}

	// V5: Endpoint must be resolvable.
	if hasExplicitCerts && args.Endpoint == "" {
		return fmt.Errorf("No endpoint configured. Provide --talos-endpoint or use --talosconfig")
	}

	// V6: Timeout must be > 0 and <= 120s.
	if args.Timeout <= 0 || args.Timeout > 120*time.Second {
		return fmt.Errorf("Invalid timeout %q: must be between 1s and 120s", args.Timeout)
	}

	// V7–V12: Subcommand-specific validation.
	switch {
	case args.Cpu != nil:
		return validateThresholds(args.Cpu.Warning, args.Cpu.Critical)
	case args.Mem != nil:
		return validateThresholds(args.Mem.Warning, args.Mem.Critical)
	case args.Disk != nil:
		// V12: --mount must be an absolute path.
		if args.Disk.Mount == "" || args.Disk.Mount[0] != '/' {
			return fmt.Errorf("Invalid --mount %q: must be an absolute path", args.Disk.Mount)
		}
		return validateThresholds(args.Disk.Warning, args.Disk.Critical)
	case args.Services != nil:
		// V9: --include and --exclude are mutually exclusive.
		if len(args.Services.Include) > 0 && len(args.Services.Exclude) > 0 {
			return fmt.Errorf("Cannot use both --include and --exclude")
		}
	case args.Etcd != nil:
		// V11: --min-members must be >= 1.
		if args.Etcd.MinMembers < 1 {
			return fmt.Errorf("Invalid --min-members %q: must be >= 1", fmt.Sprintf("%d", args.Etcd.MinMembers))
		}
		return validateThresholds(args.Etcd.Warning, args.Etcd.Critical)
	case args.Load != nil:
		// V10: --period must be 1, 5, or 15.
		switch args.Load.Period {
		case "1", "5", "15":
		default:
			return fmt.Errorf("Invalid --period %q: must be 1, 5, or 15", args.Load.Period)
		}
		// Load thresholds are optional (auto-computed at runtime from CPU count).
		return validateOptionalThresholds(args.Load.Warning, args.Load.Critical)
	}

	return nil
}

// validateThresholds parses warning and critical thresholds (V7) and checks
// their ordering (V8). Both thresholds are required.
func validateThresholds(warnStr, critStr string) error {
	warnT, err := threshold.Parse(warnStr)
	if err != nil {
		return fmt.Errorf("Invalid warning threshold %q: expected Nagios range format", warnStr)
	}
	critT, err := threshold.Parse(critStr)
	if err != nil {
		return fmt.Errorf("Invalid critical threshold %q: expected Nagios range format", critStr)
	}
	warnThresholdOrdering(warnT, critT)
	return nil
}

// validateOptionalThresholds parses thresholds that may be empty (V7) and
// checks ordering if both are provided (V8). Used by load check where
// thresholds are auto-computed at runtime if not specified.
func validateOptionalThresholds(warnStr, critStr string) error {
	var warnT, critT *threshold.Threshold

	if warnStr != "" {
		t, err := threshold.Parse(warnStr)
		if err != nil {
			return fmt.Errorf("Invalid warning threshold %q: expected Nagios range format", warnStr)
		}
		warnT = &t
	}
	if critStr != "" {
		t, err := threshold.Parse(critStr)
		if err != nil {
			return fmt.Errorf("Invalid critical threshold %q: expected Nagios range format", critStr)
		}
		critT = &t
	}
	if warnT != nil && critT != nil {
		warnThresholdOrdering(*warnT, *critT)
	}

	return nil
}

// warnThresholdOrdering prints a warning to stderr if the warning range
// appears wider than the critical range (V8). This is informational only —
// Nagios convention allows it but it often indicates a configuration mistake.
func warnThresholdOrdering(warn, crit threshold.Threshold) {
	// Only compare simple, non-inverted, bounded ranges.
	if warn.Inside || crit.Inside || warn.StartInf || crit.StartInf {
		return
	}
	if math.IsInf(warn.End, 1) || math.IsInf(crit.End, 1) {
		return
	}
	if warn.End > crit.End {
		fmt.Fprintln(os.Stderr, "Warning: -w range is wider than -c range")
	}
}

// checkFileReadable verifies that a file exists and is not a directory.
func checkFileReadable(flagName, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			return fmt.Errorf("Cannot read %s: %s: %s", flagName, path, pe.Err)
		}
		return fmt.Errorf("Cannot read %s: %s: %s", flagName, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("Cannot read %s: %s: is a directory", flagName, path)
	}
	return nil
}

// mapGRPCError converts a gRPC or connection error into a Nagios Result.
// This implements the error-to-exit-code mapping from DESIGN.md Section 7.
//
// Mapping:
//   - DeadlineExceeded → CRITICAL (node likely unhealthy)
//   - Unavailable → CRITICAL (node down)
//   - Unimplemented → UNKNOWN (API version mismatch)
//   - PermissionDenied → UNKNOWN (cert lacks required role)
//   - Non-gRPC errors → CRITICAL (connection refused, TLS failure)
//   - All other gRPC errors → UNKNOWN
func mapGRPCError(checkName string, err error, timeout time.Duration) *output.Result {
	st, ok := status.FromError(err)
	if !ok {
		// Non-gRPC error (connection refused, TLS failure, etc.) → CRITICAL.
		return &output.Result{
			Status:    output.Critical,
			CheckName: checkName,
			Summary:   err.Error(),
		}
	}

	switch st.Code() {
	case codes.DeadlineExceeded:
		return &output.Result{
			Status:    output.Critical,
			CheckName: checkName,
			Summary:   fmt.Sprintf("Talos API timeout after %s", timeout),
		}
	case codes.Unavailable:
		return &output.Result{
			Status:    output.Critical,
			CheckName: checkName,
			Summary:   fmt.Sprintf("Talos API unavailable: %s", st.Message()),
		}
	case codes.Unimplemented:
		return &output.Result{
			Status:    output.Unknown,
			CheckName: checkName,
			Summary:   "RPC not supported (API version mismatch?)",
		}
	case codes.PermissionDenied:
		return &output.Result{
			Status:    output.Unknown,
			CheckName: checkName,
			Summary:   fmt.Sprintf("Permission denied: %s", st.Message()),
		}
	default:
		return &output.Result{
			Status:    output.Unknown,
			CheckName: checkName,
			Summary:   fmt.Sprintf("Talos API error: %s", st.Message()),
		}
	}
}
