package check

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/DLAKE-IO/check-talos/internal/threshold"
)

// MemoryCheck monitors memory utilization via the Talos Memory API.
type MemoryCheck struct {
	Warning  threshold.Threshold
	Critical threshold.Threshold
}

// NewMemoryCheck creates a MemoryCheck from warning and critical threshold strings.
func NewMemoryCheck(w, c string) (*MemoryCheck, error) {
	wt, err := threshold.Parse(w)
	if err != nil {
		return nil, fmt.Errorf("invalid warning threshold: %w", err)
	}
	ct, err := threshold.Parse(c)
	if err != nil {
		return nil, fmt.Errorf("invalid critical threshold: %w", err)
	}
	return &MemoryCheck{Warning: wt, Critical: ct}, nil
}

// Name returns the check identifier used in Nagios output.
func (ch *MemoryCheck) Name() string { return "MEMORY" }

// Run executes the memory check against the Talos API.
func (ch *MemoryCheck) Run(ctx context.Context, client TalosClient) (*output.Result, error) {
	resp, err := client.Memory(ctx)
	if err != nil {
		return nil, err
	}

	if resp == nil || len(resp.GetMessages()) == 0 {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "Empty response from Talos API",
		}, nil
	}

	mem := resp.GetMessages()[0]
	meminfo := mem.GetMeminfo()
	if meminfo == nil {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "No memory data in response",
		}, nil
	}

	memTotal := meminfo.GetMemtotal()
	memAvailable := meminfo.GetMemavailable()

	if memTotal == 0 {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "Invalid data: total memory is zero",
		}, nil
	}

	// Talos API returns kB (matching /proc/meminfo); convert to bytes.
	memTotal *= 1024
	memAvailable *= 1024

	usedBytes := memTotal - memAvailable
	usagePct := (float64(usedBytes) / float64(memTotal)) * 100

	// Round to 1 decimal place for display consistency.
	usagePct = math.Round(usagePct*10) / 10

	status := output.OK
	if ch.Critical.Violated(usagePct) {
		status = output.Critical
	} else if ch.Warning.Violated(usagePct) {
		status = output.Warning
	}

	memTotalStr := strconv.FormatUint(memTotal, 10)

	return &output.Result{
		Status:    status,
		CheckName: ch.Name(),
		Summary: fmt.Sprintf("Memory usage %.1f%% (%s / %s)",
			usagePct, output.HumanBytes(usedBytes), output.HumanBytes(memTotal)),
		PerfData: []output.PerfDatum{
			{
				Label: "memory_usage",
				Value: usagePct,
				UOM:   "",
				Warn:  ch.Warning.String(),
				Crit:  ch.Critical.String(),
				Min:   "0",
				Max:   "100",
			},
			{
				Label: "memory_used",
				Value: float64(usedBytes),
				UOM:   "B",
				Warn:  "",
				Crit:  "",
				Min:   "0",
				Max:   memTotalStr,
			},
			{
				Label: "memory_total",
				Value: float64(memTotal),
				UOM:   "B",
				Warn:  "",
				Crit:  "",
				Min:   "0",
				Max:   "",
			},
		},
	}, nil
}
