package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// mockCPUClient implements TalosClient for CPU check testing.
type mockCPUClient struct {
	resp *machine.SystemStatResponse
	err  error
}

func (m *mockCPUClient) SystemStat(_ context.Context) (*machine.SystemStatResponse, error) {
	return m.resp, m.err
}

func (m *mockCPUClient) Memory(context.Context) (*machine.MemoryResponse, error) {
	return nil, nil
}

func (m *mockCPUClient) Mounts(context.Context) (*machine.MountsResponse, error) {
	return nil, nil
}

func (m *mockCPUClient) ServiceList(context.Context) (*machine.ServiceListResponse, error) {
	return nil, nil
}

func (m *mockCPUClient) EtcdStatus(context.Context) (*machine.EtcdStatusResponse, error) {
	return nil, nil
}

func (m *mockCPUClient) EtcdMemberList(context.Context) (*machine.EtcdMemberListResponse, error) {
	return nil, nil
}

func (m *mockCPUClient) EtcdAlarmList(context.Context) (*machine.EtcdAlarmListResponse, error) {
	return nil, nil
}

func (m *mockCPUClient) LoadAvg(context.Context) (*machine.LoadAvgResponse, error) {
	return nil, nil
}

func TestNewCPUCheck(t *testing.T) {
	tests := []struct {
		name    string
		warn    string
		crit    string
		wantErr bool
	}{
		{name: "valid defaults", warn: "80", crit: "90", wantErr: false},
		{name: "valid ranges", warn: "~:75", crit: "~:95", wantErr: false},
		{name: "invalid warning", warn: "abc", crit: "90", wantErr: true},
		{name: "invalid critical", warn: "80", crit: "xyz", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewCPUCheck(tt.warn, tt.crit)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ch.Name() != "CPU" {
				t.Errorf("Name() = %q, want %q", ch.Name(), "CPU")
			}
		})
	}
}

func TestCPUCheckRun(t *testing.T) {
	tests := []struct {
		name       string
		warn       string
		crit       string
		client     *mockCPUClient
		wantStatus output.Status
		wantSubstr string
	}{
		{
			name: "OK - low usage",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				resp: makeSystemStatResponse(3000, 200, 500, 6000, 100, 50, 50, 100),
			},
			wantStatus: output.OK,
			wantSubstr: "CPU usage 39.0%",
		},
		{
			name: "WARNING - above warning threshold",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				// 85% usage: active=8500, idle+iowait=1500, total=10000
				resp: makeSystemStatResponse(5500, 500, 2000, 1000, 500, 100, 100, 300),
			},
			wantStatus: output.Warning,
			wantSubstr: "CPU usage 85.0%",
		},
		{
			name: "CRITICAL - above critical threshold",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				resp: makeSystemStatResponse(7000, 500, 1500, 500, 200, 100, 100, 100),
			},
			wantStatus: output.Critical,
			wantSubstr: "CPU usage 93.0%",
		},
		{
			name: "UNKNOWN - zero total CPU time",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				resp: makeSystemStatResponse(0, 0, 0, 0, 0, 0, 0, 0),
			},
			wantStatus: output.Unknown,
			wantSubstr: "total CPU time is zero",
		},
		{
			name: "UNKNOWN - nil response",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				resp: nil,
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name: "UNKNOWN - empty messages",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				resp: &machine.SystemStatResponse{},
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name: "UNKNOWN - nil CpuTotal",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				resp: &machine.SystemStatResponse{
					Messages: []*machine.SystemStat{{}},
				},
			},
			wantStatus: output.Unknown,
			wantSubstr: "No CPU data in response",
		},
		{
			name: "error from client",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				err: fmt.Errorf("connection refused"),
			},
			wantStatus: -1, // not checked; error path
		},
		{
			name: "OK - exact boundary (at 80 is not violated for range 0..80)",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				// active/total = 80% → user=8000, idle=2000, total=10000
				resp: makeSystemStatResponse(8000, 0, 0, 2000, 0, 0, 0, 0),
			},
			wantStatus: output.OK,
			wantSubstr: "CPU usage 80.0%",
		},
		{
			name: "WARNING - just above 80",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				// active/total = 80.1%
				resp: makeSystemStatResponse(801, 0, 0, 199, 0, 0, 0, 0),
			},
			wantStatus: output.Warning,
			wantSubstr: "CPU usage 80.1%",
		},
		{
			name: "OK - all idle",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				resp: makeSystemStatResponse(0, 0, 0, 10000, 0, 0, 0, 0),
			},
			wantStatus: output.OK,
			wantSubstr: "CPU usage 0.0%",
		},
		{
			name: "CRITICAL - fully saturated",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				resp: makeSystemStatResponse(10000, 0, 0, 0, 0, 0, 0, 0),
			},
			wantStatus: output.Critical,
			wantSubstr: "CPU usage 100.0%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewCPUCheck(tt.warn, tt.crit)
			if err != nil {
				t.Fatalf("NewCPUCheck: %v", err)
			}

			result, err := ch.Run(context.Background(), tt.client)

			// Error path: client returns error.
			if tt.client.err != nil {
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

			if result.CheckName != "CPU" {
				t.Errorf("CheckName = %q, want %q", result.CheckName, "CPU")
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

func TestCPUCheckPerfData(t *testing.T) {
	ch, err := NewCPUCheck("80", "90")
	if err != nil {
		t.Fatalf("NewCPUCheck: %v", err)
	}

	client := &mockCPUClient{
		resp: makeSystemStatResponse(3000, 200, 500, 6000, 100, 50, 50, 100),
	}

	result, err := ch.Run(context.Background(), client)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.PerfData) != 1 {
		t.Fatalf("PerfData length = %d, want 1", len(result.PerfData))
	}

	pd := result.PerfData[0]
	if pd.Label != "cpu_usage" {
		t.Errorf("Label = %q, want %q", pd.Label, "cpu_usage")
	}
	if pd.UOM != "" {
		t.Errorf("UOM = %q, want empty", pd.UOM)
	}
	if pd.Warn != "80" {
		t.Errorf("Warn = %q, want %q", pd.Warn, "80")
	}
	if pd.Crit != "90" {
		t.Errorf("Crit = %q, want %q", pd.Crit, "90")
	}
	if pd.Min != "0" {
		t.Errorf("Min = %q, want %q", pd.Min, "0")
	}
	if pd.Max != "100" {
		t.Errorf("Max = %q, want %q", pd.Max, "100")
	}

	// Verify the full perfdata string.
	want := "cpu_usage=39;80;90;0;100"
	got := pd.String()
	if got != want {
		t.Errorf("PerfDatum.String() = %q, want %q", got, want)
	}
}

func TestCPUCheckOutputFormat(t *testing.T) {
	tests := []struct {
		name   string
		warn   string
		crit   string
		client *mockCPUClient
		want   string
	}{
		{
			name: "OK output matches DESIGN.md format",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				// 34.2% usage: active=342, idle+iowait=658, total=1000
				resp: makeSystemStatResponse(342, 0, 0, 608, 50, 0, 0, 0),
			},
			want: "TALOS CPU OK - CPU usage 34.2% | cpu_usage=34.2;80;90;0;100",
		},
		{
			name: "WARNING output matches DESIGN.md format",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				// 82.5% usage: active=825, idle+iowait=175, total=1000
				resp: makeSystemStatResponse(825, 0, 0, 150, 25, 0, 0, 0),
			},
			want: "TALOS CPU WARNING - CPU usage 82.5% | cpu_usage=82.5;80;90;0;100",
		},
		{
			name: "CRITICAL output matches DESIGN.md format",
			warn: "80", crit: "90",
			client: &mockCPUClient{
				// 96.3% usage: active=963, idle+iowait=37, total=1000
				resp: makeSystemStatResponse(963, 0, 0, 30, 7, 0, 0, 0),
			},
			want: "TALOS CPU CRITICAL - CPU usage 96.3% | cpu_usage=96.3;80;90;0;100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewCPUCheck(tt.warn, tt.crit)
			if err != nil {
				t.Fatalf("NewCPUCheck: %v", err)
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

// makeSystemStatResponse builds a SystemStatResponse with a single aggregate CPUStat.
func makeSystemStatResponse(user, nice, system, idle, iowait, irq, softirq, steal float64) *machine.SystemStatResponse {
	return &machine.SystemStatResponse{
		Messages: []*machine.SystemStat{
			{
				CpuTotal: &machine.CPUStat{
					User:    user,
					Nice:    nice,
					System:  system,
					Idle:    idle,
					Iowait:  iowait,
					Irq:     irq,
					SoftIrq: softirq,
					Steal:   steal,
				},
			},
		},
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
