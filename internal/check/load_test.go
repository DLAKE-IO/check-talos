package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// mockLoadClient implements TalosClient for Load check testing.
type mockLoadClient struct {
	loadResp *machine.LoadAvgResponse
	loadErr  error
	statResp *machine.SystemStatResponse
	statErr  error
}

func (m *mockLoadClient) SystemStat(_ context.Context) (*machine.SystemStatResponse, error) {
	return m.statResp, m.statErr
}

func (m *mockLoadClient) Memory(context.Context) (*machine.MemoryResponse, error) {
	return nil, nil
}

func (m *mockLoadClient) Mounts(context.Context) (*machine.MountsResponse, error) {
	return nil, nil
}

func (m *mockLoadClient) ServiceList(context.Context) (*machine.ServiceListResponse, error) {
	return nil, nil
}

func (m *mockLoadClient) EtcdStatus(context.Context) (*machine.EtcdStatusResponse, error) {
	return nil, nil
}

func (m *mockLoadClient) EtcdMemberList(context.Context) (*machine.EtcdMemberListResponse, error) {
	return nil, nil
}

func (m *mockLoadClient) EtcdAlarmList(context.Context) (*machine.EtcdAlarmListResponse, error) {
	return nil, nil
}

func (m *mockLoadClient) LoadAvg(_ context.Context) (*machine.LoadAvgResponse, error) {
	return m.loadResp, m.loadErr
}

// makeLoadAvgResponse builds a LoadAvgResponse with the given values.
func makeLoadAvgResponse(load1, load5, load15 float64) *machine.LoadAvgResponse {
	return &machine.LoadAvgResponse{
		Messages: []*machine.LoadAvg{
			{
				Load1:  load1,
				Load5:  load5,
				Load15: load15,
			},
		},
	}
}

// makeSystemStatWithCPUs builds a SystemStatResponse with a given CPU count.
func makeSystemStatWithCPUs(cpuCount int) *machine.SystemStatResponse {
	cpus := make([]*machine.CPUStat, cpuCount)
	for i := range cpus {
		cpus[i] = &machine.CPUStat{User: 1000, Idle: 9000}
	}
	return &machine.SystemStatResponse{
		Messages: []*machine.SystemStat{
			{
				CpuTotal: &machine.CPUStat{User: 1000, Idle: 9000},
				Cpu:      cpus,
			},
		},
	}
}

func TestNewLoadCheck(t *testing.T) {
	tests := []struct {
		name    string
		warn    string
		crit    string
		period  string
		wantErr bool
		wantW   bool // expect Warning to be non-nil
		wantC   bool // expect Critical to be non-nil
	}{
		{name: "valid explicit thresholds", warn: "4", crit: "8", period: "5", wantErr: false, wantW: true, wantC: true},
		{name: "empty thresholds (auto-compute)", warn: "", crit: "", period: "5", wantErr: false, wantW: false, wantC: false},
		{name: "valid ranges", warn: "~:6", crit: "~:12", period: "1", wantErr: false, wantW: true, wantC: true},
		{name: "invalid warning", warn: "abc", crit: "8", period: "5", wantErr: true},
		{name: "invalid critical", warn: "4", crit: "xyz", period: "5", wantErr: true},
		{name: "only warning provided", warn: "4", crit: "", period: "5", wantErr: false, wantW: true, wantC: false},
		{name: "only critical provided", warn: "", crit: "8", period: "5", wantErr: false, wantW: false, wantC: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewLoadCheck(tt.warn, tt.crit, tt.period)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ch.Name() != "LOAD" {
				t.Errorf("Name() = %q, want %q", ch.Name(), "LOAD")
			}
			if ch.Period != tt.period {
				t.Errorf("Period = %q, want %q", ch.Period, tt.period)
			}
			if (ch.Warning != nil) != tt.wantW {
				t.Errorf("Warning nil = %v, want nil = %v", ch.Warning == nil, !tt.wantW)
			}
			if (ch.Critical != nil) != tt.wantC {
				t.Errorf("Critical nil = %v, want nil = %v", ch.Critical == nil, !tt.wantC)
			}
		})
	}
}

func TestLoadCheckRun(t *testing.T) {
	tests := []struct {
		name       string
		warn       string
		crit       string
		period     string
		client     *mockLoadClient
		wantStatus output.Status
		wantSubstr string
		wantErr    bool
	}{
		{
			name: "OK - low load with explicit thresholds",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(0.98, 1.23, 1.45),
			},
			wantStatus: output.OK,
			wantSubstr: "Load average (5m) 1.23",
		},
		{
			name: "WARNING - above warning threshold",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(5.12, 4.56, 3.21),
			},
			wantStatus: output.Warning,
			wantSubstr: "Load average (5m) 4.56",
		},
		{
			name: "CRITICAL - above critical threshold",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(11.02, 9.87, 7.65),
			},
			wantStatus: output.Critical,
			wantSubstr: "Load average (5m) 9.87",
		},
		{
			name: "OK - auto-computed thresholds (4 CPUs, load < 4)",
			warn: "", crit: "", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(0.98, 1.23, 1.45),
				statResp: makeSystemStatWithCPUs(4),
			},
			wantStatus: output.OK,
			wantSubstr: "Load average (5m) 1.23",
		},
		{
			name: "WARNING - auto-computed thresholds (4 CPUs, load between 4 and 8)",
			warn: "", crit: "", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(5.12, 4.56, 3.21),
				statResp: makeSystemStatWithCPUs(4),
			},
			wantStatus: output.Warning,
			wantSubstr: "Load average (5m) 4.56",
		},
		{
			name: "CRITICAL - auto-computed thresholds (4 CPUs, load > 8)",
			warn: "", crit: "", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(11.02, 9.87, 7.65),
				statResp: makeSystemStatWithCPUs(4),
			},
			wantStatus: output.Critical,
			wantSubstr: "Load average (5m) 9.87",
		},
		{
			name: "OK - period 1 selects load1",
			warn: "4", crit: "8", period: "1",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(2.34, 1.85, 1.45),
			},
			wantStatus: output.OK,
			wantSubstr: "Load average (1m) 2.34",
		},
		{
			name: "OK - period 15 selects load15",
			warn: "4", crit: "8", period: "15",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(5.12, 4.56, 3.21),
			},
			wantStatus: output.OK,
			wantSubstr: "Load average (15m) 3.21",
		},
		{
			name: "OK - exact boundary (at 4 is not violated for range 0..4)",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(1.0, 4.0, 2.0),
			},
			wantStatus: output.OK,
			wantSubstr: "Load average (5m) 4.00",
		},
		{
			name: "WARNING - just above warning boundary",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(1.0, 4.01, 2.0),
			},
			wantStatus: output.Warning,
			wantSubstr: "Load average (5m) 4.01",
		},
		{
			name: "UNKNOWN - nil LoadAvg response",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: nil,
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name: "UNKNOWN - empty LoadAvg messages",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: &machine.LoadAvgResponse{},
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name: "error from LoadAvg client",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadErr: fmt.Errorf("connection refused"),
			},
			wantErr: true,
		},
		{
			name: "error from SystemStat (auto-compute)",
			warn: "", crit: "", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(1.23, 2.34, 3.45),
				statErr:  fmt.Errorf("connection refused"),
			},
			wantErr: true,
		},
		{
			name: "UNKNOWN - nil SystemStat response (auto-compute)",
			warn: "", crit: "", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(1.23, 2.34, 3.45),
				statResp: nil,
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty SystemStat response from Talos API",
		},
		{
			name: "UNKNOWN - empty SystemStat messages (auto-compute)",
			warn: "", crit: "", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(1.23, 2.34, 3.45),
				statResp: &machine.SystemStatResponse{},
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty SystemStat response from Talos API",
		},
		{
			name: "UNKNOWN - zero CPU count (auto-compute)",
			warn: "", crit: "", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(1.23, 2.34, 3.45),
				statResp: makeSystemStatWithCPUs(0),
			},
			wantStatus: output.Unknown,
			wantSubstr: "CPU count is zero",
		},
		{
			name: "OK - auto-compute with 2 CPUs (warn=2, crit=4)",
			warn: "", crit: "", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(0.5, 1.5, 1.0),
				statResp: makeSystemStatWithCPUs(2),
			},
			wantStatus: output.OK,
			wantSubstr: "Load average (5m) 1.50",
		},
		{
			name: "WARNING - auto-compute with 2 CPUs (warn=2, crit=4, load=2.5)",
			warn: "", crit: "", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(0.5, 2.5, 1.0),
				statResp: makeSystemStatWithCPUs(2),
			},
			wantStatus: output.Warning,
			wantSubstr: "Load average (5m) 2.50",
		},
		{
			name: "CRITICAL - auto-compute with 2 CPUs (warn=2, crit=4, load=5.0)",
			warn: "", crit: "", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(0.5, 5.0, 1.0),
				statResp: makeSystemStatWithCPUs(2),
			},
			wantStatus: output.Critical,
			wantSubstr: "Load average (5m) 5.00",
		},
		{
			name: "no SystemStat call when both thresholds explicit",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(0.98, 1.23, 1.45),
				statErr:  fmt.Errorf("should not be called"),
			},
			wantStatus: output.OK,
			wantSubstr: "Load average (5m) 1.23",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewLoadCheck(tt.warn, tt.crit, tt.period)
			if err != nil {
				t.Fatalf("NewLoadCheck: %v", err)
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

			if result.CheckName != "LOAD" {
				t.Errorf("CheckName = %q, want %q", result.CheckName, "LOAD")
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

func TestLoadCheckPerfData(t *testing.T) {
	t.Run("period 5 - thresholds on load5 only", func(t *testing.T) {
		ch, err := NewLoadCheck("4", "8", "5")
		if err != nil {
			t.Fatalf("NewLoadCheck: %v", err)
		}

		client := &mockLoadClient{
			loadResp: makeLoadAvgResponse(0.98, 1.23, 1.45),
		}

		result, err := ch.Run(context.Background(), client)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		if len(result.PerfData) != 3 {
			t.Fatalf("PerfData length = %d, want 3", len(result.PerfData))
		}

		// load1: no thresholds
		pd := result.PerfData[0]
		if pd.Label != "load1" {
			t.Errorf("PerfData[0].Label = %q, want %q", pd.Label, "load1")
		}
		if pd.Value != 0.98 {
			t.Errorf("PerfData[0].Value = %v, want %v", pd.Value, 0.98)
		}
		if pd.UOM != "" {
			t.Errorf("PerfData[0].UOM = %q, want empty", pd.UOM)
		}
		if pd.Warn != "" {
			t.Errorf("PerfData[0].Warn = %q, want empty", pd.Warn)
		}
		if pd.Crit != "" {
			t.Errorf("PerfData[0].Crit = %q, want empty", pd.Crit)
		}
		if pd.Min != "0" {
			t.Errorf("PerfData[0].Min = %q, want %q", pd.Min, "0")
		}

		// load5: with thresholds
		pd = result.PerfData[1]
		if pd.Label != "load5" {
			t.Errorf("PerfData[1].Label = %q, want %q", pd.Label, "load5")
		}
		if pd.Value != 1.23 {
			t.Errorf("PerfData[1].Value = %v, want %v", pd.Value, 1.23)
		}
		if pd.Warn != "4" {
			t.Errorf("PerfData[1].Warn = %q, want %q", pd.Warn, "4")
		}
		if pd.Crit != "8" {
			t.Errorf("PerfData[1].Crit = %q, want %q", pd.Crit, "8")
		}
		if pd.Min != "0" {
			t.Errorf("PerfData[1].Min = %q, want %q", pd.Min, "0")
		}

		// load15: no thresholds
		pd = result.PerfData[2]
		if pd.Label != "load15" {
			t.Errorf("PerfData[2].Label = %q, want %q", pd.Label, "load15")
		}
		if pd.Value != 1.45 {
			t.Errorf("PerfData[2].Value = %v, want %v", pd.Value, 1.45)
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
	})

	t.Run("period 1 - thresholds on load1 only", func(t *testing.T) {
		ch, err := NewLoadCheck("4", "8", "1")
		if err != nil {
			t.Fatalf("NewLoadCheck: %v", err)
		}

		client := &mockLoadClient{
			loadResp: makeLoadAvgResponse(2.34, 1.85, 1.45),
		}

		result, err := ch.Run(context.Background(), client)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		if result.PerfData[0].Warn != "4" {
			t.Errorf("load1 Warn = %q, want %q", result.PerfData[0].Warn, "4")
		}
		if result.PerfData[0].Crit != "8" {
			t.Errorf("load1 Crit = %q, want %q", result.PerfData[0].Crit, "8")
		}
		if result.PerfData[1].Warn != "" {
			t.Errorf("load5 Warn = %q, want empty", result.PerfData[1].Warn)
		}
		if result.PerfData[2].Warn != "" {
			t.Errorf("load15 Warn = %q, want empty", result.PerfData[2].Warn)
		}
	})

	t.Run("period 15 - thresholds on load15 only", func(t *testing.T) {
		ch, err := NewLoadCheck("4", "8", "15")
		if err != nil {
			t.Fatalf("NewLoadCheck: %v", err)
		}

		client := &mockLoadClient{
			loadResp: makeLoadAvgResponse(5.12, 4.56, 3.21),
		}

		result, err := ch.Run(context.Background(), client)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		if result.PerfData[0].Warn != "" {
			t.Errorf("load1 Warn = %q, want empty", result.PerfData[0].Warn)
		}
		if result.PerfData[1].Warn != "" {
			t.Errorf("load5 Warn = %q, want empty", result.PerfData[1].Warn)
		}
		if result.PerfData[2].Warn != "4" {
			t.Errorf("load15 Warn = %q, want %q", result.PerfData[2].Warn, "4")
		}
		if result.PerfData[2].Crit != "8" {
			t.Errorf("load15 Crit = %q, want %q", result.PerfData[2].Crit, "8")
		}
	})

	t.Run("auto-computed thresholds in perfdata", func(t *testing.T) {
		ch, err := NewLoadCheck("", "", "5")
		if err != nil {
			t.Fatalf("NewLoadCheck: %v", err)
		}

		client := &mockLoadClient{
			loadResp: makeLoadAvgResponse(0.98, 1.23, 1.45),
			statResp: makeSystemStatWithCPUs(4),
		}

		result, err := ch.Run(context.Background(), client)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		// Auto-computed: warn=4 (cpuCount), crit=8 (2*cpuCount)
		pd := result.PerfData[1] // load5
		if pd.Warn != "4" {
			t.Errorf("load5 Warn = %q, want %q", pd.Warn, "4")
		}
		if pd.Crit != "8" {
			t.Errorf("load5 Crit = %q, want %q", pd.Crit, "8")
		}

		// Other periods: no thresholds
		if result.PerfData[0].Warn != "" {
			t.Errorf("load1 Warn = %q, want empty", result.PerfData[0].Warn)
		}
		if result.PerfData[2].Warn != "" {
			t.Errorf("load15 Warn = %q, want empty", result.PerfData[2].Warn)
		}
	})
}

func TestLoadCheckOutputFormat(t *testing.T) {
	tests := []struct {
		name   string
		warn   string
		crit   string
		period string
		client *mockLoadClient
		want   string
	}{
		{
			name: "OK output matches DESIGN.md format",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(0.98, 1.23, 1.45),
			},
			want: "TALOS LOAD OK - Load average (5m) 1.23 | load1=0.98;;;0; load5=1.23;4;8;0; load15=1.45;;;0;",
		},
		{
			name: "WARNING output matches DESIGN.md format",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(5.12, 4.56, 3.21),
			},
			want: "TALOS LOAD WARNING - Load average (5m) 4.56 | load1=5.12;;;0; load5=4.56;4;8;0; load15=3.21;;;0;",
		},
		{
			name: "CRITICAL output matches DESIGN.md format",
			warn: "4", crit: "8", period: "5",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(11.02, 9.87, 7.65),
			},
			want: "TALOS LOAD CRITICAL - Load average (5m) 9.87 | load1=11.02;;;0; load5=9.87;4;8;0; load15=7.65;;;0;",
		},
		{
			name: "period 1 output format",
			warn: "4", crit: "8", period: "1",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(2.34, 1.85, 1.45),
			},
			want: "TALOS LOAD OK - Load average (1m) 2.34 | load1=2.34;4;8;0; load5=1.85;;;0; load15=1.45;;;0;",
		},
		{
			name: "period 15 output format",
			warn: "4", crit: "8", period: "15",
			client: &mockLoadClient{
				loadResp: makeLoadAvgResponse(5.12, 4.56, 3.21),
			},
			want: "TALOS LOAD OK - Load average (15m) 3.21 | load1=5.12;;;0; load5=4.56;;;0; load15=3.21;4;8;0;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewLoadCheck(tt.warn, tt.crit, tt.period)
			if err != nil {
				t.Fatalf("NewLoadCheck: %v", err)
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

func TestLoadCheckAutoThreshold(t *testing.T) {
	tests := []struct {
		name     string
		cpuCount int
		wantWarn string
		wantCrit string
	}{
		{name: "2 CPUs", cpuCount: 2, wantWarn: "2", wantCrit: "4"},
		{name: "4 CPUs", cpuCount: 4, wantWarn: "4", wantCrit: "8"},
		{name: "8 CPUs", cpuCount: 8, wantWarn: "8", wantCrit: "16"},
		{name: "1 CPU", cpuCount: 1, wantWarn: "1", wantCrit: "2"},
		{name: "64 CPUs", cpuCount: 64, wantWarn: "64", wantCrit: "128"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewLoadCheck("", "", "5")
			if err != nil {
				t.Fatalf("NewLoadCheck: %v", err)
			}

			client := &mockLoadClient{
				loadResp: makeLoadAvgResponse(0.5, 0.5, 0.5),
				statResp: makeSystemStatWithCPUs(tt.cpuCount),
			}

			result, err := ch.Run(context.Background(), client)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			// Check that thresholds are on load5 (selected period).
			pd := result.PerfData[1] // load5
			if pd.Warn != tt.wantWarn {
				t.Errorf("auto-computed Warn = %q, want %q", pd.Warn, tt.wantWarn)
			}
			if pd.Crit != tt.wantCrit {
				t.Errorf("auto-computed Crit = %q, want %q", pd.Crit, tt.wantCrit)
			}
		})
	}
}
