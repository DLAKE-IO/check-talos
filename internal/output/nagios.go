// Package output builds Nagios-compliant plugin output: status line,
// optional long text, and performance data. Handles OK, WARNING,
// CRITICAL, and UNKNOWN formatting.
package output

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	nagios "github.com/atc0005/go-nagios"
)

// Status represents a Nagios check exit status.
type Status int

const (
	OK       Status = 0
	Warning  Status = 1
	Critical Status = 2
	Unknown  Status = 3
)

// String returns the uppercase Nagios status label.
func (s Status) String() string {
	switch s {
	case OK:
		return "OK"
	case Warning:
		return "WARNING"
	case Critical:
		return "CRITICAL"
	case Unknown:
		return "UNKNOWN"
	default:
		return "UNKNOWN"
	}
}

// ExitCode returns the Nagios-compatible exit code for this status.
func (s Status) ExitCode() int {
	return int(s)
}

// PerfDatum represents a single Nagios performance data metric.
type PerfDatum struct {
	Label string  // Metric name (lowercase, underscore-separated, no spaces)
	Value float64 // Metric value
	UOM   string  // Unit of measurement: %, B, s, c, or empty
	Warn  string  // Warning threshold (Nagios range string)
	Crit  string  // Critical threshold (Nagios range string)
	Min   string  // Minimum possible value
	Max   string  // Maximum possible value
}

// String formats the PerfDatum as a Nagios performance data entry.
//
// Format: label=value[UOM];[warn];[crit];[min];[max]
func (pd PerfDatum) String() string {
	return fmt.Sprintf("%s=%s%s;%s;%s;%s;%s",
		pd.Label,
		formatValue(pd.Value),
		pd.UOM,
		pd.Warn,
		pd.Crit,
		pd.Min,
		pd.Max,
	)
}

// Result represents the structured output of a check execution.
type Result struct {
	Status    Status      // Nagios status (OK, Warning, Critical, Unknown)
	CheckName string      // Uppercase check name: CPU, MEMORY, DISK, SERVICES, ETCD, LOAD
	Summary   string      // One-line human-readable summary
	Details   string      // Optional multi-line long text (visible in extended detail view)
	PerfData  []PerfDatum // Performance data metrics
}

// String formats the Result as Nagios-compliant output.
//
// Format:
//
//	TALOS <CHECK> <STATUS> - <summary> | <perfdata>
//	<optional long text>
func (r *Result) String() string {
	var b strings.Builder

	// Status line.
	fmt.Fprintf(&b, "TALOS %s %s - %s", r.CheckName, r.Status, r.Summary)

	// Performance data (after the pipe separator).
	if len(r.PerfData) > 0 {
		b.WriteString(" | ")
		b.WriteString(FormatPerfData(r.PerfData))
	}

	// Long text (details) on subsequent lines.
	if r.Details != "" {
		b.WriteByte('\n')
		b.WriteString(r.Details)
	}

	return b.String()
}

// FormatPerfData formats a slice of PerfDatum as a space-separated string.
func FormatPerfData(data []PerfDatum) string {
	parts := make([]string, len(data))
	for i, pd := range data {
		parts[i] = pd.String()
	}
	return strings.Join(parts, " ")
}

// HumanBytes formats a byte count as a human-readable string with 2 decimal
// places (e.g., "7.53 GB", "8.00 GB", "12.50 MB").
func HumanBytes(bytes uint64) string {
	const (
		kb = 1024.0
		mb = kb * 1024
		gb = mb * 1024
		tb = gb * 1024
	)

	b := float64(bytes)
	switch {
	case b >= tb:
		return fmt.Sprintf("%.2f TB", b/tb)
	case b >= gb:
		return fmt.Sprintf("%.2f GB", b/gb)
	case b >= mb:
		return fmt.Sprintf("%.2f MB", b/mb)
	case b >= kb:
		return fmt.Sprintf("%.2f KB", b/kb)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// ApplyToPlugin populates a go-nagios Plugin from this Result.
// This bridges the output.Result type to go-nagios for exit code
// handling and panic recovery via Plugin.ReturnCheckResults().
func (r *Result) ApplyToPlugin(p *nagios.Plugin) {
	// Status line (go-nagios adds perfdata after this).
	p.ServiceOutput = fmt.Sprintf("TALOS %s %s - %s",
		r.CheckName, r.Status, r.Summary)

	// Exit code.
	switch r.Status {
	case OK:
		p.ExitStatusCode = nagios.StateOKExitCode
	case Warning:
		p.ExitStatusCode = nagios.StateWARNINGExitCode
	case Critical:
		p.ExitStatusCode = nagios.StateCRITICALExitCode
	default:
		p.ExitStatusCode = nagios.StateUNKNOWNExitCode
	}

	// Long text (multi-line details).
	if r.Details != "" {
		p.LongServiceOutput = r.Details
	}

	// Performance data.
	for _, pd := range r.PerfData {
		_ = p.AddPerfData(false, nagios.PerformanceData{
			Label:             pd.Label,
			Value:             formatValue(pd.Value),
			UnitOfMeasurement: pd.UOM,
			Warn:              pd.Warn,
			Crit:              pd.Crit,
			Min:               pd.Min,
			Max:               pd.Max,
		})
	}
}

// formatValue formats a float64 for performance data output.
// Integers are formatted without decimals (e.g., "45"); non-integers
// use the shortest decimal representation (e.g., "34.2", "1.23").
func formatValue(v float64) string {
	if v == math.Trunc(v) && !math.IsInf(v, 0) && !math.IsNaN(v) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
