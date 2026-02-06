package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// mockMemoryClient implements TalosClient for Memory check testing.
type mockMemoryClient struct {
	resp *machine.MemoryResponse
	err  error
}

func (m *mockMemoryClient) SystemStat(context.Context) (*machine.SystemStatResponse, error) {
	return nil, nil
}

func (m *mockMemoryClient) Memory(_ context.Context) (*machine.MemoryResponse, error) {
	return m.resp, m.err
}

func (m *mockMemoryClient) Mounts(context.Context) (*machine.MountsResponse, error) {
	return nil, nil
}

func (m *mockMemoryClient) ServiceList(context.Context) (*machine.ServiceListResponse, error) {
	return nil, nil
}

func (m *mockMemoryClient) EtcdStatus(context.Context) (*machine.EtcdStatusResponse, error) {
	return nil, nil
}

func (m *mockMemoryClient) EtcdMemberList(context.Context) (*machine.EtcdMemberListResponse, error) {
	return nil, nil
}

func (m *mockMemoryClient) EtcdAlarmList(context.Context) (*machine.EtcdAlarmListResponse, error) {
	return nil, nil
}

func (m *mockMemoryClient) LoadAvg(context.Context) (*machine.LoadAvgResponse, error) {
	return nil, nil
}

func TestNewMemoryCheck(t *testing.T) {
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
			ch, err := NewMemoryCheck(tt.warn, tt.crit)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ch.Name() != "MEMORY" {
				t.Errorf("Name() = %q, want %q", ch.Name(), "MEMORY")
			}
		})
	}
}

func TestMemoryCheckRun(t *testing.T) {
	// Talos API returns kB (matching /proc/meminfo). Mock values are in kB.
	tests := []struct {
		name       string
		warn       string
		crit       string
		client     *mockMemoryClient
		wantStatus output.Status
		wantSubstr string
		wantErr    bool
	}{
		{
			name: "OK - low usage",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				resp: makeMemoryResponse(8388608, 5222680), // 8 GiB total, ~5 GiB available (kB) → 37.7%
			},
			wantStatus: output.OK,
			wantSubstr: "Memory usage 37.7%",
		},
		{
			name: "WARNING - above warning threshold",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				// 8 GiB total, ~1.3 GiB available (kB) → 83.7% used
				resp: makeMemoryResponse(8388608, 1363472),
			},
			wantStatus: output.Warning,
			wantSubstr: "Memory usage 83.7%",
		},
		{
			name: "CRITICAL - above critical threshold",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				// 8 GiB total, ~0.47 GiB available (kB) → 94.1% used
				resp: makeMemoryResponse(8388608, 494188),
			},
			wantStatus: output.Critical,
			wantSubstr: "Memory usage 94.1%",
		},
		{
			name: "UNKNOWN - memtotal is zero",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				resp: makeMemoryResponse(0, 0),
			},
			wantStatus: output.Unknown,
			wantSubstr: "total memory is zero",
		},
		{
			name: "UNKNOWN - nil response",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				resp: nil,
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name: "UNKNOWN - empty messages",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				resp: &machine.MemoryResponse{},
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name: "UNKNOWN - nil meminfo",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				resp: &machine.MemoryResponse{
					Messages: []*machine.Memory{{}},
				},
			},
			wantStatus: output.Unknown,
			wantSubstr: "No memory data in response",
		},
		{
			name: "error from client",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				err: fmt.Errorf("connection refused"),
			},
			wantErr: true,
		},
		{
			name: "OK - exact boundary (at 80 is not violated for range 0..80)",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				// 10000 kB total, 2000 kB available → exactly 80% used
				resp: makeMemoryResponse(10000, 2000),
			},
			wantStatus: output.OK,
			wantSubstr: "Memory usage 80.0%",
		},
		{
			name: "WARNING - just above 80",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				// 10000 kB total, 1990 kB available → 80.1% used
				resp: makeMemoryResponse(10000, 1990),
			},
			wantStatus: output.Warning,
			wantSubstr: "Memory usage 80.1%",
		},
		{
			name: "OK - all memory available (0% used)",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				resp: makeMemoryResponse(8388608, 8388608), // 8 GiB in kB
			},
			wantStatus: output.OK,
			wantSubstr: "Memory usage 0.0%",
		},
		{
			name: "CRITICAL - fully used (100%)",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				resp: makeMemoryResponse(8388608, 0), // 8 GiB in kB
			},
			wantStatus: output.Critical,
			wantSubstr: "Memory usage 100.0%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewMemoryCheck(tt.warn, tt.crit)
			if err != nil {
				t.Fatalf("NewMemoryCheck: %v", err)
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

			if result.CheckName != "MEMORY" {
				t.Errorf("CheckName = %q, want %q", result.CheckName, "MEMORY")
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

func TestMemoryCheckPerfData(t *testing.T) {
	ch, err := NewMemoryCheck("80", "90")
	if err != nil {
		t.Fatalf("NewMemoryCheck: %v", err)
	}

	// 8 GiB total (kB), ~5 GiB available (kB) → ~37.7% usage
	client := &mockMemoryClient{
		resp: makeMemoryResponse(8388608, 5222680),
	}

	result, err := ch.Run(context.Background(), client)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.PerfData) != 3 {
		t.Fatalf("PerfData length = %d, want 3", len(result.PerfData))
	}

	// memory_usage perfdata
	pd := result.PerfData[0]
	if pd.Label != "memory_usage" {
		t.Errorf("PerfData[0].Label = %q, want %q", pd.Label, "memory_usage")
	}
	if pd.UOM != "" {
		t.Errorf("PerfData[0].UOM = %q, want empty", pd.UOM)
	}
	if pd.Warn != "80" {
		t.Errorf("PerfData[0].Warn = %q, want %q", pd.Warn, "80")
	}
	if pd.Crit != "90" {
		t.Errorf("PerfData[0].Crit = %q, want %q", pd.Crit, "90")
	}
	if pd.Min != "0" {
		t.Errorf("PerfData[0].Min = %q, want %q", pd.Min, "0")
	}
	if pd.Max != "100" {
		t.Errorf("PerfData[0].Max = %q, want %q", pd.Max, "100")
	}

	// memory_used perfdata — values are in bytes after kB→B conversion
	pd = result.PerfData[1]
	if pd.Label != "memory_used" {
		t.Errorf("PerfData[1].Label = %q, want %q", pd.Label, "memory_used")
	}
	if pd.UOM != "B" {
		t.Errorf("PerfData[1].UOM = %q, want %q", pd.UOM, "B")
	}
	usedBytes := (uint64(8388608) - uint64(5222680)) * 1024 // kB difference → bytes
	wantUsed := float64(usedBytes)
	if pd.Value != wantUsed {
		t.Errorf("PerfData[1].Value = %v, want %v", pd.Value, wantUsed)
	}
	if pd.Min != "0" {
		t.Errorf("PerfData[1].Min = %q, want %q", pd.Min, "0")
	}
	if pd.Max != "8589934592" {
		t.Errorf("PerfData[1].Max = %q, want %q", pd.Max, "8589934592")
	}

	// memory_total perfdata
	pd = result.PerfData[2]
	if pd.Label != "memory_total" {
		t.Errorf("PerfData[2].Label = %q, want %q", pd.Label, "memory_total")
	}
	if pd.UOM != "B" {
		t.Errorf("PerfData[2].UOM = %q, want %q", pd.UOM, "B")
	}
	if pd.Value != float64(uint64(8388608)*1024) {
		t.Errorf("PerfData[2].Value = %v, want %v", pd.Value, float64(uint64(8388608)*1024))
	}
	if pd.Min != "0" {
		t.Errorf("PerfData[2].Min = %q, want %q", pd.Min, "0")
	}
	if pd.Max != "" {
		t.Errorf("PerfData[2].Max = %q, want empty", pd.Max)
	}
}

func TestMemoryCheckOutputFormat(t *testing.T) {
	// Mock values are in kB (as returned by Talos API).
	// After kB→B conversion in Run(), output byte values match expectations.
	tests := []struct {
		name   string
		warn   string
		crit   string
		client *mockMemoryClient
		want   string
	}{
		{
			name: "OK output matches DESIGN.md format",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				// 8388608 kB = 8 GiB, 3165928 kB available → 62.3% used
				// After conversion: used = (8388608-3165928)*1024 = 5348024320 B
				resp: makeMemoryResponse(8388608, 3165928),
			},
			want: "TALOS MEMORY OK - Memory usage 62.3% (4.98 GB / 8.00 GB) | memory_usage=62.3;80;90;0;100 memory_used=5348024320B;;;0;8589934592 memory_total=8589934592B;;;0;",
		},
		{
			name: "WARNING output matches DESIGN.md format",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				// 8388608 kB = 8 GiB, 1363472 kB available → 83.7% used
				// After conversion: used = (8388608-1363472)*1024 = 7193739264 B
				resp: makeMemoryResponse(8388608, 1363472),
			},
			want: "TALOS MEMORY WARNING - Memory usage 83.7% (6.70 GB / 8.00 GB) | memory_usage=83.7;80;90;0;100 memory_used=7193739264B;;;0;8589934592 memory_total=8589934592B;;;0;",
		},
		{
			name: "CRITICAL output matches DESIGN.md format",
			warn: "80", crit: "90",
			client: &mockMemoryClient{
				// 8388608 kB = 8 GiB, 494188 kB available → 94.1% used
				// After conversion: used = (8388608-494188)*1024 = 8083886080 B
				resp: makeMemoryResponse(8388608, 494188),
			},
			want: "TALOS MEMORY CRITICAL - Memory usage 94.1% (7.53 GB / 8.00 GB) | memory_usage=94.1;80;90;0;100 memory_used=8083886080B;;;0;8589934592 memory_total=8589934592B;;;0;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewMemoryCheck(tt.warn, tt.crit)
			if err != nil {
				t.Fatalf("NewMemoryCheck: %v", err)
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

// makeMemoryResponse builds a MemoryResponse with the given memtotal and
// memavailable values in kB (matching the Talos API / /proc/meminfo units).
func makeMemoryResponse(memTotal, memAvailable uint64) *machine.MemoryResponse {
	return &machine.MemoryResponse{
		Messages: []*machine.Memory{
			{
				Meminfo: &machine.MemInfo{
					Memtotal:     memTotal,
					Memavailable: memAvailable,
				},
			},
		},
	}
}
