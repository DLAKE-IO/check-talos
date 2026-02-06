package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// mockDiskClient implements TalosClient for Disk check testing.
type mockDiskClient struct {
	resp *machine.MountsResponse
	err  error
}

func (m *mockDiskClient) SystemStat(context.Context) (*machine.SystemStatResponse, error) {
	return nil, nil
}

func (m *mockDiskClient) Memory(context.Context) (*machine.MemoryResponse, error) {
	return nil, nil
}

func (m *mockDiskClient) Mounts(_ context.Context) (*machine.MountsResponse, error) {
	return m.resp, m.err
}

func (m *mockDiskClient) ServiceList(context.Context) (*machine.ServiceListResponse, error) {
	return nil, nil
}

func (m *mockDiskClient) EtcdStatus(context.Context) (*machine.EtcdStatusResponse, error) {
	return nil, nil
}

func (m *mockDiskClient) EtcdMemberList(context.Context) (*machine.EtcdMemberListResponse, error) {
	return nil, nil
}

func (m *mockDiskClient) EtcdAlarmList(context.Context) (*machine.EtcdAlarmListResponse, error) {
	return nil, nil
}

func (m *mockDiskClient) LoadAvg(context.Context) (*machine.LoadAvgResponse, error) {
	return nil, nil
}

func TestNewDiskCheck(t *testing.T) {
	tests := []struct {
		name    string
		warn    string
		crit    string
		mount   string
		wantErr bool
	}{
		{name: "valid defaults", warn: "80", crit: "90", mount: "/", wantErr: false},
		{name: "valid ranges", warn: "~:75", crit: "~:95", mount: "/var", wantErr: false},
		{name: "invalid warning", warn: "abc", crit: "90", mount: "/", wantErr: true},
		{name: "invalid critical", warn: "80", crit: "xyz", mount: "/", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewDiskCheck(tt.warn, tt.crit, tt.mount)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ch.Name() != "DISK" {
				t.Errorf("Name() = %q, want %q", ch.Name(), "DISK")
			}
			if ch.Mount != tt.mount {
				t.Errorf("Mount = %q, want %q", ch.Mount, tt.mount)
			}
		})
	}
}

func TestDiskCheckRun(t *testing.T) {
	tests := []struct {
		name       string
		warn       string
		crit       string
		mount      string
		client     *mockDiskClient
		wantStatus output.Status
		wantSubstr string
		wantErr    bool
	}{
		{
			name: "OK - low usage",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				resp: makeMountsResponse("/", 21474836480, 11811160064), // 20 GB total, ~11 GB avail → 45%
			},
			wantStatus: output.OK,
			wantSubstr: "/ usage 45.0%",
		},
		{
			name: "WARNING - above warning threshold",
			warn: "80", crit: "90", mount: "/var",
			client: &mockDiskClient{
				// 50 GB total, ~7.9 GB avail → 84.2% used
				resp: makeMountsResponse("/var", 53687091200, 8482714010),
			},
			wantStatus: output.Warning,
			wantSubstr: "/var usage 84.2%",
		},
		{
			name: "CRITICAL - above critical threshold",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				// 20 GB total, ~1.24 GB avail → 93.8% used
				// used = 20 GB - avail = 93.8%
				// avail = 20GB * (1-0.938) = 20GB * 0.062 = 1331459769.6
				resp: makeMountsResponse("/", 21474836480, 1331459770),
			},
			wantStatus: output.Critical,
			wantSubstr: "/ usage 93.8%",
		},
		{
			name: "UNKNOWN - size is zero",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				resp: makeMountsResponse("/", 0, 0),
			},
			wantStatus: output.Unknown,
			wantSubstr: "total capacity is zero",
		},
		{
			name: "UNKNOWN - mount point not found",
			warn: "80", crit: "90", mount: "/data",
			client: &mockDiskClient{
				resp: makeMountsResponse("/", 21474836480, 11811160064),
			},
			wantStatus: output.Unknown,
			wantSubstr: "Mount point /data not found",
		},
		{
			name: "UNKNOWN - nil response",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				resp: nil,
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name: "UNKNOWN - empty messages",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				resp: &machine.MountsResponse{},
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name: "UNKNOWN - empty stats",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				resp: &machine.MountsResponse{
					Messages: []*machine.Mounts{{}},
				},
			},
			wantStatus: output.Unknown,
			wantSubstr: "No mount data in response",
		},
		{
			name: "error from client",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				err: fmt.Errorf("connection refused"),
			},
			wantErr: true,
		},
		{
			name: "OK - exact boundary (at 80 is not violated for range 0..80)",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				// 10000 total, 2000 avail → exactly 80% used
				resp: makeMountsResponse("/", 10000, 2000),
			},
			wantStatus: output.OK,
			wantSubstr: "/ usage 80.0%",
		},
		{
			name: "WARNING - just above 80",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				// 10000 total, 1990 avail → 80.1% used
				resp: makeMountsResponse("/", 10000, 1990),
			},
			wantStatus: output.Warning,
			wantSubstr: "/ usage 80.1%",
		},
		{
			name: "OK - all available (0% used)",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				resp: makeMountsResponse("/", 21474836480, 21474836480),
			},
			wantStatus: output.OK,
			wantSubstr: "/ usage 0.0%",
		},
		{
			name: "CRITICAL - fully used (100%)",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				resp: makeMountsResponse("/", 21474836480, 0),
			},
			wantStatus: output.Critical,
			wantSubstr: "/ usage 100.0%",
		},
		{
			name: "OK - multiple mounts, selects correct one",
			warn: "80", crit: "90", mount: "/var",
			client: &mockDiskClient{
				resp: makeMultiMountsResponse(
					mountEntry{path: "/", size: 21474836480, available: 0},              // 100% used (would be CRITICAL)
					mountEntry{path: "/var", size: 53687091200, available: 40265318400}, // 25% used → OK
				),
			},
			wantStatus: output.OK,
			wantSubstr: "/var usage 25.0%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewDiskCheck(tt.warn, tt.crit, tt.mount)
			if err != nil {
				t.Fatalf("NewDiskCheck: %v", err)
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

			if result.CheckName != "DISK" {
				t.Errorf("CheckName = %q, want %q", result.CheckName, "DISK")
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

func TestDiskCheckPerfData(t *testing.T) {
	ch, err := NewDiskCheck("80", "90", "/")
	if err != nil {
		t.Fatalf("NewDiskCheck: %v", err)
	}

	// 20 GB total, ~11 GB avail → 45% used
	client := &mockDiskClient{
		resp: makeMountsResponse("/", 21474836480, 11811160064),
	}

	result, err := ch.Run(context.Background(), client)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.PerfData) != 3 {
		t.Fatalf("PerfData length = %d, want 3", len(result.PerfData))
	}

	// disk_usage perfdata
	pd := result.PerfData[0]
	if pd.Label != "disk_usage" {
		t.Errorf("PerfData[0].Label = %q, want %q", pd.Label, "disk_usage")
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

	// disk_used perfdata
	pd = result.PerfData[1]
	if pd.Label != "disk_used" {
		t.Errorf("PerfData[1].Label = %q, want %q", pd.Label, "disk_used")
	}
	if pd.UOM != "B" {
		t.Errorf("PerfData[1].UOM = %q, want %q", pd.UOM, "B")
	}
	usedBytes := uint64(21474836480) - uint64(11811160064)
	wantUsed := float64(usedBytes)
	if pd.Value != wantUsed {
		t.Errorf("PerfData[1].Value = %v, want %v", pd.Value, wantUsed)
	}
	if pd.Min != "0" {
		t.Errorf("PerfData[1].Min = %q, want %q", pd.Min, "0")
	}
	if pd.Max != "21474836480" {
		t.Errorf("PerfData[1].Max = %q, want %q", pd.Max, "21474836480")
	}

	// disk_total perfdata
	pd = result.PerfData[2]
	if pd.Label != "disk_total" {
		t.Errorf("PerfData[2].Label = %q, want %q", pd.Label, "disk_total")
	}
	if pd.UOM != "B" {
		t.Errorf("PerfData[2].UOM = %q, want %q", pd.UOM, "B")
	}
	if pd.Value != float64(21474836480) {
		t.Errorf("PerfData[2].Value = %v, want %v", pd.Value, float64(21474836480))
	}
	if pd.Min != "0" {
		t.Errorf("PerfData[2].Min = %q, want %q", pd.Min, "0")
	}
	if pd.Max != "" {
		t.Errorf("PerfData[2].Max = %q, want empty", pd.Max)
	}
}

func TestDiskCheckOutputFormat(t *testing.T) {
	tests := []struct {
		name   string
		warn   string
		crit   string
		mount  string
		client *mockDiskClient
		want   string
	}{
		{
			name: "OK output matches DESIGN.md format",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				// 20 GB total, ~11 GB avail → 45.0% used
				// used = 21474836480 - 11811160064 = 9663676416
				// 9663676416 / 21474836480 = 0.44999... → rounds to 45.0%
				resp: makeMountsResponse("/", 21474836480, 11811160064),
			},
			want: "TALOS DISK OK - / usage 45.0% (9.00 GB / 20.00 GB) | disk_usage=45;80;90;0;100 disk_used=9663676416B;;;0;21474836480 disk_total=21474836480B;;;0;",
		},
		{
			name: "WARNING output matches DESIGN.md format",
			warn: "80", crit: "90", mount: "/var",
			client: &mockDiskClient{
				// 50 GB total, ~7.9 GB avail → 84.2% used
				// used = 53687091200 - 8482714010 = 45204377190
				// 45204377190 / 53687091200 = 0.84200... → 84.2%
				resp: makeMountsResponse("/var", 53687091200, 8482714010),
			},
			want: "TALOS DISK WARNING - /var usage 84.2% (42.10 GB / 50.00 GB) | disk_usage=84.2;80;90;0;100 disk_used=45204377190B;;;0;53687091200 disk_total=53687091200B;;;0;",
		},
		{
			name: "CRITICAL output matches DESIGN.md format",
			warn: "80", crit: "90", mount: "/",
			client: &mockDiskClient{
				// 20 GB total, ~1.24 GB avail → 93.8% used
				// used = 21474836480 - 1331459770 = 20143376710
				// 20143376710 / 21474836480 = 0.93800... → 93.8%
				resp: makeMountsResponse("/", 21474836480, 1331459770),
			},
			want: "TALOS DISK CRITICAL - / usage 93.8% (18.76 GB / 20.00 GB) | disk_usage=93.8;80;90;0;100 disk_used=20143376710B;;;0;21474836480 disk_total=21474836480B;;;0;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewDiskCheck(tt.warn, tt.crit, tt.mount)
			if err != nil {
				t.Fatalf("NewDiskCheck: %v", err)
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

// mountEntry describes a single mount point for building test responses.
type mountEntry struct {
	path      string
	size      uint64
	available uint64
}

// makeMountsResponse builds a MountsResponse with a single mount point.
func makeMountsResponse(mountedOn string, size, available uint64) *machine.MountsResponse {
	return &machine.MountsResponse{
		Messages: []*machine.Mounts{
			{
				Stats: []*machine.MountStat{
					{
						Filesystem: "/dev/sda1",
						Size:       size,
						Available:  available,
						MountedOn:  mountedOn,
					},
				},
			},
		},
	}
}

// makeMultiMountsResponse builds a MountsResponse with multiple mount points.
func makeMultiMountsResponse(entries ...mountEntry) *machine.MountsResponse {
	stats := make([]*machine.MountStat, len(entries))
	for i, e := range entries {
		stats[i] = &machine.MountStat{
			Filesystem: "/dev/sda1",
			Size:       e.size,
			Available:  e.available,
			MountedOn:  e.path,
		}
	}
	return &machine.MountsResponse{
		Messages: []*machine.Mounts{
			{
				Stats: stats,
			},
		},
	}
}
