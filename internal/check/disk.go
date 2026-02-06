package check

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/DLAKE-IO/check-talos/internal/threshold"
)

// DiskCheck monitors disk utilization for a specific mount point via the Talos Mounts API.
type DiskCheck struct {
	Warning  threshold.Threshold
	Critical threshold.Threshold
	Mount    string
}

// NewDiskCheck creates a DiskCheck from warning and critical threshold strings and a mount point.
func NewDiskCheck(w, c, mount string) (*DiskCheck, error) {
	wt, err := threshold.Parse(w)
	if err != nil {
		return nil, fmt.Errorf("invalid warning threshold: %w", err)
	}
	ct, err := threshold.Parse(c)
	if err != nil {
		return nil, fmt.Errorf("invalid critical threshold: %w", err)
	}
	return &DiskCheck{Warning: wt, Critical: ct, Mount: mount}, nil
}

// Name returns the check identifier used in Nagios output.
func (ch *DiskCheck) Name() string { return "DISK" }

// Run executes the disk check against the Talos API.
func (ch *DiskCheck) Run(ctx context.Context, client TalosClient) (*output.Result, error) {
	resp, err := client.Mounts(ctx)
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

	mounts := resp.GetMessages()[0]
	stats := mounts.GetStats()
	if len(stats) == 0 {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "No mount data in response",
		}, nil
	}

	// Find the mount point matching the requested path.
	for _, ms := range stats {
		if ms.GetMountedOn() != ch.Mount {
			continue
		}

		size := ms.GetSize()
		available := ms.GetAvailable()

		if size == 0 {
			return &output.Result{
				Status:    output.Unknown,
				CheckName: ch.Name(),
				Summary:   fmt.Sprintf("Invalid data: total capacity is zero for %s", ch.Mount),
			}, nil
		}

		used := size - available
		usagePct := (float64(used) / float64(size)) * 100

		// Round to 1 decimal place for display consistency.
		usagePct = math.Round(usagePct*10) / 10

		status := output.OK
		if ch.Critical.Violated(usagePct) {
			status = output.Critical
		} else if ch.Warning.Violated(usagePct) {
			status = output.Warning
		}

		sizeStr := strconv.FormatUint(size, 10)

		return &output.Result{
			Status:    status,
			CheckName: ch.Name(),
			Summary: fmt.Sprintf("%s usage %.1f%% (%s / %s)",
				ch.Mount, usagePct, output.HumanBytes(used), output.HumanBytes(size)),
			PerfData: []output.PerfDatum{
				{
					Label: "disk_usage",
					Value: usagePct,
					UOM:   "",
					Warn:  ch.Warning.String(),
					Crit:  ch.Critical.String(),
					Min:   "0",
					Max:   "100",
				},
				{
					Label: "disk_used",
					Value: float64(used),
					UOM:   "B",
					Warn:  "",
					Crit:  "",
					Min:   "0",
					Max:   sizeStr,
				},
				{
					Label: "disk_total",
					Value: float64(size),
					UOM:   "B",
					Warn:  "",
					Crit:  "",
					Min:   "0",
					Max:   "",
				},
			},
		}, nil
	}

	// Mount point not found in response.
	return &output.Result{
		Status:    output.Unknown,
		CheckName: ch.Name(),
		Summary:   fmt.Sprintf("Mount point %s not found", ch.Mount),
	}, nil
}
