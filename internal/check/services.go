package check

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/DLAKE-IO/check-talos/internal/output"
)

// ServicesCheck monitors Talos system service health via the ServiceList API.
// Services are evaluated as healthy when state == "Running" AND
// (health.healthy || health.unknown). Any unhealthy service produces CRITICAL.
type ServicesCheck struct {
	Include []string
	Exclude []string
}

// NewServicesCheck creates a ServicesCheck with include/exclude filters.
// Include and exclude are mutually exclusive (validated in CLI parsing).
func NewServicesCheck(include, exclude []string) (*ServicesCheck, error) {
	return &ServicesCheck{Include: include, Exclude: exclude}, nil
}

// Name returns the check identifier used in Nagios output.
func (ch *ServicesCheck) Name() string { return "SERVICES" }

// Run executes the services check against the Talos API.
func (ch *ServicesCheck) Run(ctx context.Context, client TalosClient) (*output.Result, error) {
	resp, err := client.ServiceList(ctx)
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

	services := resp.GetMessages()[0].GetServices()
	if len(services) == 0 {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "No services in response",
		}, nil
	}

	// Build filter sets for O(1) lookups.
	includeSet := toSet(ch.Include)
	excludeSet := toSet(ch.Exclude)

	type unhealthyInfo struct {
		id      string
		state   string
		health  string
		message string
	}

	var total, healthy int
	var unhealthyList []unhealthyInfo

	for _, svc := range services {
		id := svc.GetId()

		// Apply include filter: if set, skip services not in the list.
		if len(includeSet) > 0 {
			if _, ok := includeSet[id]; !ok {
				continue
			}
		}

		// Apply exclude filter: skip services in the exclude list.
		if len(excludeSet) > 0 {
			if _, ok := excludeSet[id]; ok {
				continue
			}
		}

		total++

		state := svc.GetState()
		h := svc.GetHealth()

		// A service is healthy when Running AND (healthy OR unknown health).
		if state == "Running" && h != nil && (h.GetHealthy() || h.GetUnknown()) {
			healthy++
			continue
		}

		// Determine the health description for the detail line.
		healthDesc := "unknown"
		msg := ""
		if h != nil {
			if h.GetHealthy() {
				healthDesc = "healthy"
			} else if h.GetUnknown() {
				healthDesc = "unknown"
			} else {
				healthDesc = "unhealthy"
			}
			msg = h.GetLastMessage()
		}

		unhealthyList = append(unhealthyList, unhealthyInfo{
			id:      id,
			state:   state,
			health:  healthDesc,
			message: msg,
		})
	}

	unhealthyCount := total - healthy

	// Build perfdata (no thresholds — assertion-based check).
	perfData := []output.PerfDatum{
		{Label: "services_total", Value: float64(total), Min: "0"},
		{Label: "services_healthy", Value: float64(healthy), Min: "0"},
		{Label: "services_unhealthy", Value: float64(unhealthyCount), Min: "0"},
	}

	if unhealthyCount == 0 {
		return &output.Result{
			Status:    output.OK,
			CheckName: ch.Name(),
			Summary:   fmt.Sprintf("%d/%d services healthy", healthy, total),
			PerfData:  perfData,
		}, nil
	}

	// Sort unhealthy services by name for deterministic output.
	sort.Slice(unhealthyList, func(i, j int) bool {
		return unhealthyList[i].id < unhealthyList[j].id
	})

	// Build the summary line with unhealthy service names.
	names := make([]string, len(unhealthyList))
	for i, u := range unhealthyList {
		names[i] = u.id
	}
	summary := fmt.Sprintf("%d/%d services unhealthy: %s",
		unhealthyCount, total, strings.Join(names, ", "))

	// Build long text with per-service details.
	var details strings.Builder
	for i, u := range unhealthyList {
		if i > 0 {
			details.WriteByte('\n')
		}
		fmt.Fprintf(&details, "%s: state=%s, health=%s, message=%q",
			u.id, u.state, u.health, u.message)
	}

	return &output.Result{
		Status:    output.Critical,
		CheckName: ch.Name(),
		Summary:   summary,
		Details:   details.String(),
		PerfData:  perfData,
	}, nil
}

// toSet converts a string slice to a map for O(1) membership checks.
func toSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	s := make(map[string]struct{}, len(items))
	for _, item := range items {
		s[item] = struct{}{}
	}
	return s
}
