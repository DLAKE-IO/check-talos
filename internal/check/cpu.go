package check

import (
	"context"
	"fmt"
	"math"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/DLAKE-IO/check-talos/internal/threshold"
)

// CPUCheck monitors aggregate CPU utilization via the Talos SystemStat API.
type CPUCheck struct {
	Warning  threshold.Threshold
	Critical threshold.Threshold
}

// NewCPUCheck creates a CPUCheck from warning and critical threshold strings.
func NewCPUCheck(w, c string) (*CPUCheck, error) {
	wt, err := threshold.Parse(w)
	if err != nil {
		return nil, fmt.Errorf("invalid warning threshold: %w", err)
	}
	ct, err := threshold.Parse(c)
	if err != nil {
		return nil, fmt.Errorf("invalid critical threshold: %w", err)
	}
	return &CPUCheck{Warning: wt, Critical: ct}, nil
}

// Name returns the check identifier used in Nagios output.
func (ch *CPUCheck) Name() string { return "CPU" }

// Run executes the CPU check against the Talos API.
func (ch *CPUCheck) Run(ctx context.Context, client TalosClient) (*output.Result, error) {
	resp, err := client.SystemStat(ctx)
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

	stat := resp.GetMessages()[0]
	cpu := stat.GetCpuTotal()
	if cpu == nil {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "No CPU data in response",
		}, nil
	}

	total := cpu.GetUser() + cpu.GetNice() + cpu.GetSystem() +
		cpu.GetIdle() + cpu.GetIowait() + cpu.GetIrq() +
		cpu.GetSoftIrq() + cpu.GetSteal()

	if total == 0 {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "Invalid data: total CPU time is zero",
		}, nil
	}

	active := total - cpu.GetIdle() - cpu.GetIowait()
	usagePct := (active / total) * 100

	// Round to 1 decimal place for display consistency.
	usagePct = math.Round(usagePct*10) / 10

	status := output.OK
	if ch.Critical.Violated(usagePct) {
		status = output.Critical
	} else if ch.Warning.Violated(usagePct) {
		status = output.Warning
	}

	return &output.Result{
		Status:    status,
		CheckName: ch.Name(),
		Summary:   fmt.Sprintf("CPU usage %.1f%%", usagePct),
		PerfData: []output.PerfDatum{
			{
				Label: "cpu_usage",
				Value: usagePct,
				UOM:   "",
				Warn:  ch.Warning.String(),
				Crit:  ch.Critical.String(),
				Min:   "0",
				Max:   "100",
			},
		},
	}, nil
}
