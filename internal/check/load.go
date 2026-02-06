package check

import (
	"context"
	"fmt"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/DLAKE-IO/check-talos/internal/threshold"
)

// LoadCheck monitors system load averages via the Talos LoadAvg and
// SystemStat APIs. If thresholds are not provided, they are auto-computed
// from the CPU count: warning = cpuCount, critical = 2 * cpuCount.
type LoadCheck struct {
	Warning  *threshold.Threshold
	Critical *threshold.Threshold
	Period   string // "1", "5", or "15"
}

// NewLoadCheck creates a LoadCheck from optional warning/critical threshold
// strings and a period. Empty threshold strings result in auto-computed
// thresholds at runtime based on the CPU count.
func NewLoadCheck(w, c, period string) (*LoadCheck, error) {
	ch := &LoadCheck{Period: period}

	if w != "" {
		wt, err := threshold.Parse(w)
		if err != nil {
			return nil, fmt.Errorf("invalid warning threshold: %w", err)
		}
		ch.Warning = &wt
	}

	if c != "" {
		ct, err := threshold.Parse(c)
		if err != nil {
			return nil, fmt.Errorf("invalid critical threshold: %w", err)
		}
		ch.Critical = &ct
	}

	return ch, nil
}

// Name returns the check identifier used in Nagios output.
func (ch *LoadCheck) Name() string { return "LOAD" }

// Run executes the load check against the Talos API.
func (ch *LoadCheck) Run(ctx context.Context, client TalosClient) (*output.Result, error) {
	// Get load averages.
	loadResp, err := client.LoadAvg(ctx)
	if err != nil {
		return nil, err
	}

	if loadResp == nil || len(loadResp.GetMessages()) == 0 {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "Empty response from Talos API",
		}, nil
	}

	loadAvg := loadResp.GetMessages()[0]
	load1 := loadAvg.GetLoad1()
	load5 := loadAvg.GetLoad5()
	load15 := loadAvg.GetLoad15()

	// Determine effective thresholds.
	warn := ch.Warning
	crit := ch.Critical

	// Auto-compute thresholds from CPU count if not provided.
	if warn == nil || crit == nil {
		statResp, err := client.SystemStat(ctx)
		if err != nil {
			return nil, err
		}

		if statResp == nil || len(statResp.GetMessages()) == 0 {
			return &output.Result{
				Status:    output.Unknown,
				CheckName: ch.Name(),
				Summary:   "Empty SystemStat response from Talos API",
			}, nil
		}

		cpuCount := len(statResp.GetMessages()[0].GetCpu())
		if cpuCount == 0 {
			return &output.Result{
				Status:    output.Unknown,
				CheckName: ch.Name(),
				Summary:   "Invalid data: CPU count is zero",
			}, nil
		}

		if warn == nil {
			wt := threshold.Threshold{Start: 0, End: float64(cpuCount)}
			warn = &wt
		}
		if crit == nil {
			ct := threshold.Threshold{Start: 0, End: float64(2 * cpuCount)}
			crit = &ct
		}
	}

	// Select the load value based on period.
	var selectedLoad float64
	switch ch.Period {
	case "1":
		selectedLoad = load1
	case "5":
		selectedLoad = load5
	case "15":
		selectedLoad = load15
	default:
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   fmt.Sprintf("Invalid period: %s", ch.Period),
		}, nil
	}

	// Evaluate thresholds.
	status := output.OK
	if crit.Violated(selectedLoad) {
		status = output.Critical
	} else if warn.Violated(selectedLoad) {
		status = output.Warning
	}

	// Build perfdata for all three load values.
	// Thresholds only on the selected period.
	warnStr := warn.String()
	critStr := crit.String()

	perfData := []output.PerfDatum{
		{Label: "load1", Value: load1, Min: "0", Max: ""},
		{Label: "load5", Value: load5, Min: "0", Max: ""},
		{Label: "load15", Value: load15, Min: "0", Max: ""},
	}

	switch ch.Period {
	case "1":
		perfData[0].Warn = warnStr
		perfData[0].Crit = critStr
	case "5":
		perfData[1].Warn = warnStr
		perfData[1].Crit = critStr
	case "15":
		perfData[2].Warn = warnStr
		perfData[2].Crit = critStr
	}

	return &output.Result{
		Status:    status,
		CheckName: ch.Name(),
		Summary:   fmt.Sprintf("Load average (%sm) %.2f", ch.Period, selectedLoad),
		PerfData:  perfData,
	}, nil
}
