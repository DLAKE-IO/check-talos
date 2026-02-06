package check

import (
	"context"
	"fmt"
	"strings"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/DLAKE-IO/check-talos/internal/threshold"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// EtcdCheck monitors etcd cluster health via the Talos API.
// It verifies leader presence, member count, active alarms, and DB size
// against configurable thresholds.
type EtcdCheck struct {
	Warning    threshold.Threshold
	Critical   threshold.Threshold
	MinMembers int
}

// NewEtcdCheck creates an EtcdCheck from warning/critical threshold strings
// and a minimum member count.
func NewEtcdCheck(w, c string, minMembers int) (*EtcdCheck, error) {
	wt, err := threshold.Parse(w)
	if err != nil {
		return nil, fmt.Errorf("invalid warning threshold: %w", err)
	}
	ct, err := threshold.Parse(c)
	if err != nil {
		return nil, fmt.Errorf("invalid critical threshold: %w", err)
	}
	return &EtcdCheck{Warning: wt, Critical: ct, MinMembers: minMembers}, nil
}

// Name returns the check identifier used in Nagios output.
func (ch *EtcdCheck) Name() string { return "ETCD" }

// Run executes the etcd check against the Talos API.
//
// Evaluation order per DESIGN.md Section 4.5:
//  1. EtcdStatus — leader != 0, errors[] empty
//  2. EtcdMemberList — len(members) >= MinMembers
//  3. EtcdAlarmList — any active alarm → CRITICAL
//  4. db_size against thresholds
func (ch *EtcdCheck) Run(ctx context.Context, client TalosClient) (*output.Result, error) {
	// Step 1: Get etcd status.
	statusResp, err := client.EtcdStatus(ctx)
	if err != nil {
		return nil, err
	}

	if statusResp == nil || len(statusResp.GetMessages()) == 0 {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "Empty response from Talos API",
		}, nil
	}

	etcdStatus := statusResp.GetMessages()[0]
	memberStatus := etcdStatus.GetMemberStatus()
	if memberStatus == nil {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "No etcd status data in response",
		}, nil
	}

	memberId := memberStatus.GetMemberId()
	leader := memberStatus.GetLeader()
	dbSize := memberStatus.GetDbSize()
	dbSizeInUse := memberStatus.GetDbSizeInUse()

	// Step 2: Get member list.
	memberResp, err := client.EtcdMemberList(ctx)
	if err != nil {
		return nil, err
	}

	if memberResp == nil || len(memberResp.GetMessages()) == 0 {
		return &output.Result{
			Status:    output.Unknown,
			CheckName: ch.Name(),
			Summary:   "Empty member list response from Talos API",
		}, nil
	}

	members := memberResp.GetMessages()[0].GetMembers()
	memberCount := len(members)

	// Step 3: Get alarm list.
	alarmResp, err := client.EtcdAlarmList(ctx)
	if err != nil {
		return nil, err
	}

	activeAlarms := collectAlarms(alarmResp)

	// Build perfdata (always emitted when data was retrieved).
	perfData := []output.PerfDatum{
		{
			Label: "etcd_dbsize",
			Value: float64(dbSize),
			UOM:   "B",
			Warn:  ch.Warning.String(),
			Crit:  ch.Critical.String(),
			Min:   "0",
			Max:   "",
		},
		{
			Label: "etcd_dbsize_in_use",
			Value: float64(dbSizeInUse),
			UOM:   "B",
			Min:   "0",
			Max:   "",
		},
		{
			Label: "etcd_members",
			Value: float64(memberCount),
			Min:   "0",
			Max:   "",
		},
	}

	// Evaluation order: structural assertions first, then thresholds.

	// Check 1: Leader must exist.
	if leader == 0 {
		return &output.Result{
			Status:    output.Critical,
			CheckName: ch.Name(),
			Summary:   "No leader elected",
			PerfData:  perfData,
		}, nil
	}

	// Check 2: Member count must meet minimum.
	if memberCount < ch.MinMembers {
		return &output.Result{
			Status:    output.Critical,
			CheckName: ch.Name(),
			Summary:   fmt.Sprintf("Member count %d below minimum %d", memberCount, ch.MinMembers),
			PerfData:  perfData,
		}, nil
	}

	// Check 3: No active alarms.
	if len(activeAlarms) > 0 {
		return &output.Result{
			Status:    output.Critical,
			CheckName: ch.Name(),
			Summary:   fmt.Sprintf("Active alarm: %s", strings.Join(activeAlarms, ", ")),
			PerfData:  perfData,
		}, nil
	}

	// Check 4: DB size against thresholds.
	dbSizeFloat := float64(dbSize)

	status := output.OK
	if ch.Critical.Violated(dbSizeFloat) {
		status = output.Critical
	} else if ch.Warning.Violated(dbSizeFloat) {
		status = output.Warning
	}

	var role string
	if memberId == leader {
		role = "Leader"
	} else {
		role = fmt.Sprintf("Follower, leader %d", leader)
	}

	summary := fmt.Sprintf("%s, %d/%d members, DB %s",
		role, memberCount, ch.MinMembers, output.HumanBytes(uint64(dbSize)))

	return &output.Result{
		Status:    status,
		CheckName: ch.Name(),
		Summary:   summary,
		PerfData:  perfData,
	}, nil
}

// collectAlarms extracts active alarm type names from an EtcdAlarmListResponse.
// Only non-NONE alarms are returned.
func collectAlarms(resp *machine.EtcdAlarmListResponse) []string {
	if resp == nil {
		return nil
	}

	var alarms []string
	for _, msg := range resp.GetMessages() {
		for _, ma := range msg.GetMemberAlarms() {
			alarm := ma.GetAlarm()
			if alarm != machine.EtcdMemberAlarm_NONE {
				alarms = append(alarms, alarm.String())
			}
		}
	}
	return alarms
}
