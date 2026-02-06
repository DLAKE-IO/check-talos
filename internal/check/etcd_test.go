package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// mockEtcdClient implements TalosClient for Etcd check testing.
type mockEtcdClient struct {
	statusResp *machine.EtcdStatusResponse
	statusErr  error
	memberResp *machine.EtcdMemberListResponse
	memberErr  error
	alarmResp  *machine.EtcdAlarmListResponse
	alarmErr   error
}

func (m *mockEtcdClient) SystemStat(context.Context) (*machine.SystemStatResponse, error) {
	return nil, nil
}

func (m *mockEtcdClient) Memory(context.Context) (*machine.MemoryResponse, error) {
	return nil, nil
}

func (m *mockEtcdClient) Mounts(context.Context) (*machine.MountsResponse, error) {
	return nil, nil
}

func (m *mockEtcdClient) ServiceList(context.Context) (*machine.ServiceListResponse, error) {
	return nil, nil
}

func (m *mockEtcdClient) EtcdStatus(_ context.Context) (*machine.EtcdStatusResponse, error) {
	return m.statusResp, m.statusErr
}

func (m *mockEtcdClient) EtcdMemberList(_ context.Context) (*machine.EtcdMemberListResponse, error) {
	return m.memberResp, m.memberErr
}

func (m *mockEtcdClient) EtcdAlarmList(_ context.Context) (*machine.EtcdAlarmListResponse, error) {
	return m.alarmResp, m.alarmErr
}

func (m *mockEtcdClient) LoadAvg(context.Context) (*machine.LoadAvgResponse, error) {
	return nil, nil
}

// Helper to build an EtcdStatusResponse.
func makeEtcdStatusResponse(memberId, leader uint64, dbSize, dbSizeInUse int64) *machine.EtcdStatusResponse {
	return &machine.EtcdStatusResponse{
		Messages: []*machine.EtcdStatus{
			{
				MemberStatus: &machine.EtcdMemberStatus{
					MemberId:    memberId,
					Leader:      leader,
					DbSize:      dbSize,
					DbSizeInUse: dbSizeInUse,
				},
			},
		},
	}
}

// Helper to build an EtcdMemberListResponse with N members.
func makeEtcdMemberListResponse(count int) *machine.EtcdMemberListResponse {
	members := make([]*machine.EtcdMember, count)
	for i := 0; i < count; i++ {
		members[i] = &machine.EtcdMember{
			Id:       uint64(i + 1),
			Hostname: fmt.Sprintf("cp-%d", i+1),
		}
	}
	return &machine.EtcdMemberListResponse{
		Messages: []*machine.EtcdMembers{
			{
				Members: members,
			},
		},
	}
}

// Helper to build an EtcdAlarmListResponse with given alarm types.
func makeEtcdAlarmListResponse(alarms ...machine.EtcdMemberAlarm_AlarmType) *machine.EtcdAlarmListResponse {
	if len(alarms) == 0 {
		return &machine.EtcdAlarmListResponse{
			Messages: []*machine.EtcdAlarm{
				{
					MemberAlarms: nil,
				},
			},
		}
	}
	memberAlarms := make([]*machine.EtcdMemberAlarm, len(alarms))
	for i, a := range alarms {
		memberAlarms[i] = &machine.EtcdMemberAlarm{
			MemberId: 1234,
			Alarm:    a,
		}
	}
	return &machine.EtcdAlarmListResponse{
		Messages: []*machine.EtcdAlarm{
			{
				MemberAlarms: memberAlarms,
			},
		},
	}
}

func TestNewEtcdCheck(t *testing.T) {
	tests := []struct {
		name       string
		warn       string
		crit       string
		minMembers int
		wantErr    bool
	}{
		{name: "valid defaults", warn: "~:100000000", crit: "~:200000000", minMembers: 3, wantErr: false},
		{name: "valid custom ranges", warn: "~:50000000", crit: "~:100000000", minMembers: 5, wantErr: false},
		{name: "invalid warning", warn: "abc", crit: "~:200000000", minMembers: 3, wantErr: true},
		{name: "invalid critical", warn: "~:100000000", crit: "xyz", minMembers: 3, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewEtcdCheck(tt.warn, tt.crit, tt.minMembers)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ch.Name() != "ETCD" {
				t.Errorf("Name() = %q, want %q", ch.Name(), "ETCD")
			}
			if ch.MinMembers != tt.minMembers {
				t.Errorf("MinMembers = %d, want %d", ch.MinMembers, tt.minMembers)
			}
		})
	}
}

func TestEtcdCheckRun(t *testing.T) {
	tests := []struct {
		name       string
		warn       string
		crit       string
		minMembers int
		client     *mockEtcdClient
		wantStatus output.Status
		wantSubstr string
		wantErr    bool
	}{
		{
			name:       "OK - healthy cluster",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.OK,
			wantSubstr: "Leader",
		},
		{
			name:       "CRITICAL - no leader",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(0, 0, 45000000, 40000000),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.Critical,
			wantSubstr: "No leader elected",
		},
		{
			name:       "CRITICAL - member count below minimum",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(2),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.Critical,
			wantSubstr: "Member count 2 below minimum 3",
		},
		{
			name:       "CRITICAL - active NOSPACE alarm",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 2147483648, 2000000000),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(machine.EtcdMemberAlarm_NOSPACE),
			},
			wantStatus: output.Critical,
			wantSubstr: "Active alarm: NOSPACE",
		},
		{
			name:       "CRITICAL - active CORRUPT alarm",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(machine.EtcdMemberAlarm_CORRUPT),
			},
			wantStatus: output.Critical,
			wantSubstr: "Active alarm: CORRUPT",
		},
		{
			name:       "WARNING - DB size exceeds warning threshold",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 117878784, 96468992),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.Warning,
			wantSubstr: "Leader",
		},
		{
			name:       "CRITICAL - DB size exceeds critical threshold",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 250000000, 200000000),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.Critical,
			wantSubstr: "Leader",
		},
		{
			name:       "UNKNOWN - nil status response",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: nil,
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name:       "UNKNOWN - empty status messages",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: &machine.EtcdStatusResponse{},
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name:       "UNKNOWN - nil member status",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: &machine.EtcdStatusResponse{
					Messages: []*machine.EtcdStatus{
						{MemberStatus: nil},
					},
				},
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.Unknown,
			wantSubstr: "No etcd status data in response",
		},
		{
			name:       "error from EtcdStatus",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusErr: fmt.Errorf("etcd not running on this node"),
			},
			wantErr: true,
		},
		{
			name:       "error from EtcdMemberList",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberErr:  fmt.Errorf("connection refused"),
			},
			wantErr: true,
		},
		{
			name:       "error from EtcdAlarmList",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(3),
				alarmErr:   fmt.Errorf("connection refused"),
			},
			wantErr: true,
		},
		{
			name:       "UNKNOWN - nil member list response",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: nil,
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty member list response from Talos API",
		},
		{
			name:       "UNKNOWN - empty member list messages",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: &machine.EtcdMemberListResponse{},
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty member list response from Talos API",
		},
		{
			name:       "OK - DB size at exact warning boundary (not violated)",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 100000000, 80000000),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.OK,
			wantSubstr: "Leader",
		},
		{
			name:       "WARNING - DB size just above warning boundary",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 100000001, 80000000),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.Warning,
			wantSubstr: "Leader",
		},
		{
			name:       "OK - 5 members with min 3",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(5),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.OK,
			wantSubstr: "5/3 members",
		},
		{
			name:       "OK - NONE alarm type is ignored",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(machine.EtcdMemberAlarm_NONE),
			},
			wantStatus: output.OK,
			wantSubstr: "Leader",
		},
		{
			name:       "OK - nil alarm response treated as no alarms",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  nil,
			},
			wantStatus: output.OK,
			wantSubstr: "Leader",
		},
		{
			name:       "OK - follower node reports leader ID",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(5678, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			wantStatus: output.OK,
			wantSubstr: "Follower, leader 1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewEtcdCheck(tt.warn, tt.crit, tt.minMembers)
			if err != nil {
				t.Fatalf("NewEtcdCheck: %v", err)
			}

			result, err := ch.Run(context.Background(), tt.client)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Status != tt.wantStatus {
				t.Errorf("status = %v, want %v", result.Status, tt.wantStatus)
			}

			if result.CheckName != "ETCD" {
				t.Errorf("CheckName = %q, want %q", result.CheckName, "ETCD")
			}

			resultStr := result.String()
			if tt.wantSubstr != "" {
				if !contains(resultStr, tt.wantSubstr) {
					t.Errorf("output %q does not contain %q", resultStr, tt.wantSubstr)
				}
			}
		})
	}
}

func TestEtcdCheckPerfData(t *testing.T) {
	ch, err := NewEtcdCheck("~:100000000", "~:200000000", 3)
	if err != nil {
		t.Fatalf("NewEtcdCheck: %v", err)
	}

	client := &mockEtcdClient{
		statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
		memberResp: makeEtcdMemberListResponse(3),
		alarmResp:  makeEtcdAlarmListResponse(),
	}

	result, err := ch.Run(context.Background(), client)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.PerfData) != 3 {
		t.Fatalf("PerfData length = %d, want 3", len(result.PerfData))
	}

	// etcd_dbsize
	pd := result.PerfData[0]
	if pd.Label != "etcd_dbsize" {
		t.Errorf("PerfData[0].Label = %q, want %q", pd.Label, "etcd_dbsize")
	}
	if pd.Value != 13107200 {
		t.Errorf("PerfData[0].Value = %v, want %v", pd.Value, 13107200)
	}
	if pd.UOM != "B" {
		t.Errorf("PerfData[0].UOM = %q, want %q", pd.UOM, "B")
	}
	if pd.Warn != "~:100000000" {
		t.Errorf("PerfData[0].Warn = %q, want %q", pd.Warn, "~:100000000")
	}
	if pd.Crit != "~:200000000" {
		t.Errorf("PerfData[0].Crit = %q, want %q", pd.Crit, "~:200000000")
	}
	if pd.Min != "0" {
		t.Errorf("PerfData[0].Min = %q, want %q", pd.Min, "0")
	}
	if pd.Max != "" {
		t.Errorf("PerfData[0].Max = %q, want empty", pd.Max)
	}

	// etcd_dbsize_in_use
	pd = result.PerfData[1]
	if pd.Label != "etcd_dbsize_in_use" {
		t.Errorf("PerfData[1].Label = %q, want %q", pd.Label, "etcd_dbsize_in_use")
	}
	if pd.Value != 8388608 {
		t.Errorf("PerfData[1].Value = %v, want %v", pd.Value, 8388608)
	}
	if pd.UOM != "B" {
		t.Errorf("PerfData[1].UOM = %q, want %q", pd.UOM, "B")
	}
	if pd.Warn != "" {
		t.Errorf("PerfData[1].Warn = %q, want empty", pd.Warn)
	}
	if pd.Crit != "" {
		t.Errorf("PerfData[1].Crit = %q, want empty", pd.Crit)
	}
	if pd.Min != "0" {
		t.Errorf("PerfData[1].Min = %q, want %q", pd.Min, "0")
	}

	// etcd_members
	pd = result.PerfData[2]
	if pd.Label != "etcd_members" {
		t.Errorf("PerfData[2].Label = %q, want %q", pd.Label, "etcd_members")
	}
	if pd.Value != 3 {
		t.Errorf("PerfData[2].Value = %v, want %v", pd.Value, 3)
	}
	if pd.UOM != "" {
		t.Errorf("PerfData[2].UOM = %q, want empty", pd.UOM)
	}
	if pd.Warn != "" {
		t.Errorf("PerfData[2].Warn = %q, want empty", pd.Warn)
	}
	if pd.Crit != "" {
		t.Errorf("PerfData[2].Crit = %q, want empty", pd.Crit)
	}
	if pd.Min != "0" {
		t.Errorf("PerfData[2].Min = %q, want %q", pd.Min, "0")
	}
}

func TestEtcdCheckOutputFormat(t *testing.T) {
	tests := []struct {
		name       string
		warn       string
		crit       string
		minMembers int
		client     *mockEtcdClient
		want       string
	}{
		{
			name:       "OK output matches DESIGN.md format",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			want: "TALOS ETCD OK - Leader, 3/3 members, DB 12.50 MB | etcd_dbsize=13107200B;~:100000000;~:200000000;0; etcd_dbsize_in_use=8388608B;;;0; etcd_members=3;;;0;",
		},
		{
			name:       "WARNING output matches DESIGN.md format",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 117878784, 96468992),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			want: "TALOS ETCD WARNING - Leader, 3/3 members, DB 112.42 MB | etcd_dbsize=117878784B;~:100000000;~:200000000;0; etcd_dbsize_in_use=96468992B;;;0; etcd_members=3;;;0;",
		},
		{
			name:       "CRITICAL no leader matches DESIGN.md format",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(0, 0, 45000000, 40000000),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			want: "TALOS ETCD CRITICAL - No leader elected | etcd_dbsize=45000000B;~:100000000;~:200000000;0; etcd_dbsize_in_use=40000000B;;;0; etcd_members=3;;;0;",
		},
		{
			name:       "CRITICAL member count below minimum",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(2),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			want: "TALOS ETCD CRITICAL - Member count 2 below minimum 3 | etcd_dbsize=13107200B;~:100000000;~:200000000;0; etcd_dbsize_in_use=8388608B;;;0; etcd_members=2;;;0;",
		},
		{
			name:       "CRITICAL active NOSPACE alarm",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(1234, 1234, 2147483648, 2000000000),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(machine.EtcdMemberAlarm_NOSPACE),
			},
			want: "TALOS ETCD CRITICAL - Active alarm: NOSPACE | etcd_dbsize=2147483648B;~:100000000;~:200000000;0; etcd_dbsize_in_use=2000000000B;;;0; etcd_members=3;;;0;",
		},
		{
			name:       "OK follower output format",
			warn:       "~:100000000",
			crit:       "~:200000000",
			minMembers: 3,
			client: &mockEtcdClient{
				statusResp: makeEtcdStatusResponse(5678, 1234, 13107200, 8388608),
				memberResp: makeEtcdMemberListResponse(3),
				alarmResp:  makeEtcdAlarmListResponse(),
			},
			want: "TALOS ETCD OK - Follower, leader 1234, 3/3 members, DB 12.50 MB | etcd_dbsize=13107200B;~:100000000;~:200000000;0; etcd_dbsize_in_use=8388608B;;;0; etcd_members=3;;;0;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewEtcdCheck(tt.warn, tt.crit, tt.minMembers)
			if err != nil {
				t.Fatalf("NewEtcdCheck: %v", err)
			}
			result, err := ch.Run(context.Background(), tt.client)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := result.String()
			if got != tt.want {
				t.Errorf("output:\n  got:  %q\n  want: %q", got, tt.want)
			}
		})
	}
}

func TestEtcdCheckStructuralAssertionsBeforeThresholds(t *testing.T) {
	// Verify that structural failures (no leader, low members, alarms)
	// take precedence over threshold evaluation, even when DB size
	// is within normal range.
	t.Run("no leader takes precedence over OK DB size", func(t *testing.T) {
		ch, _ := NewEtcdCheck("~:100000000", "~:200000000", 3)
		client := &mockEtcdClient{
			statusResp: makeEtcdStatusResponse(0, 0, 5000000, 4000000), // Small DB, but no leader
			memberResp: makeEtcdMemberListResponse(3),
			alarmResp:  makeEtcdAlarmListResponse(),
		}
		result, err := ch.Run(context.Background(), client)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Status != output.Critical {
			t.Errorf("status = %v, want CRITICAL", result.Status)
		}
		if !contains(result.Summary, "No leader elected") {
			t.Errorf("summary %q should contain 'No leader elected'", result.Summary)
		}
	})

	t.Run("low members takes precedence over alarm", func(t *testing.T) {
		ch, _ := NewEtcdCheck("~:100000000", "~:200000000", 3)
		client := &mockEtcdClient{
			statusResp: makeEtcdStatusResponse(1234, 1234, 5000000, 4000000),
			memberResp: makeEtcdMemberListResponse(1), // Below minimum
			alarmResp:  makeEtcdAlarmListResponse(machine.EtcdMemberAlarm_NOSPACE),
		}
		result, err := ch.Run(context.Background(), client)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Status != output.Critical {
			t.Errorf("status = %v, want CRITICAL", result.Status)
		}
		if !contains(result.Summary, "Member count 1 below minimum 3") {
			t.Errorf("summary %q should contain member count message", result.Summary)
		}
	})

	t.Run("alarm takes precedence over DB size threshold", func(t *testing.T) {
		ch, _ := NewEtcdCheck("~:100000000", "~:200000000", 3)
		client := &mockEtcdClient{
			statusResp: makeEtcdStatusResponse(1234, 1234, 5000000, 4000000), // Small DB
			memberResp: makeEtcdMemberListResponse(3),
			alarmResp:  makeEtcdAlarmListResponse(machine.EtcdMemberAlarm_CORRUPT),
		}
		result, err := ch.Run(context.Background(), client)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Status != output.Critical {
			t.Errorf("status = %v, want CRITICAL", result.Status)
		}
		if !contains(result.Summary, "Active alarm: CORRUPT") {
			t.Errorf("summary %q should contain alarm message", result.Summary)
		}
	})
}
