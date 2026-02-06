package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/DLAKE-IO/check-talos/internal/output"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// mockServicesClient implements TalosClient for Services check testing.
type mockServicesClient struct {
	resp *machine.ServiceListResponse
	err  error
}

func (m *mockServicesClient) SystemStat(context.Context) (*machine.SystemStatResponse, error) {
	return nil, nil
}

func (m *mockServicesClient) Memory(context.Context) (*machine.MemoryResponse, error) {
	return nil, nil
}

func (m *mockServicesClient) Mounts(context.Context) (*machine.MountsResponse, error) {
	return nil, nil
}

func (m *mockServicesClient) ServiceList(_ context.Context) (*machine.ServiceListResponse, error) {
	return m.resp, m.err
}

func (m *mockServicesClient) EtcdStatus(context.Context) (*machine.EtcdStatusResponse, error) {
	return nil, nil
}

func (m *mockServicesClient) EtcdMemberList(context.Context) (*machine.EtcdMemberListResponse, error) {
	return nil, nil
}

func (m *mockServicesClient) EtcdAlarmList(context.Context) (*machine.EtcdAlarmListResponse, error) {
	return nil, nil
}

func (m *mockServicesClient) LoadAvg(context.Context) (*machine.LoadAvgResponse, error) {
	return nil, nil
}

func TestNewServicesCheck(t *testing.T) {
	tests := []struct {
		name    string
		include []string
		exclude []string
		wantErr bool
	}{
		{name: "no filters", include: nil, exclude: nil, wantErr: false},
		{name: "with include", include: []string{"kubelet", "etcd"}, exclude: nil, wantErr: false},
		{name: "with exclude", include: nil, exclude: []string{"apid"}, wantErr: false},
		{name: "empty slices", include: []string{}, exclude: []string{}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewServicesCheck(tt.include, tt.exclude)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ch.Name() != "SERVICES" {
				t.Errorf("Name() = %q, want %q", ch.Name(), "SERVICES")
			}
		})
	}
}

func TestServicesCheckRun(t *testing.T) {
	tests := []struct {
		name       string
		include    []string
		exclude    []string
		client     *mockServicesClient
		wantStatus output.Status
		wantSubstr string
		wantErr    bool
	}{
		{
			name: "OK - all services healthy",
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "apid", state: "Running", healthy: true},
					svcEntry{id: "containerd", state: "Running", unknown: true},
					svcEntry{id: "cri", state: "Running", healthy: true},
					svcEntry{id: "etcd", state: "Running", healthy: true},
					svcEntry{id: "kubelet", state: "Running", healthy: true},
					svcEntry{id: "machined", state: "Running", unknown: true},
					svcEntry{id: "trustd", state: "Running", healthy: true},
					svcEntry{id: "udevd", state: "Running", healthy: true},
				),
			},
			wantStatus: output.OK,
			wantSubstr: "8/8 services healthy",
		},
		{
			name: "CRITICAL - one service unhealthy",
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "apid", state: "Running", healthy: true},
					svcEntry{id: "containerd", state: "Running", unknown: true},
					svcEntry{id: "cri", state: "Running", healthy: true},
					svcEntry{id: "etcd", state: "Running", healthy: true},
					svcEntry{id: "kubelet", state: "Finished", healthy: false, message: "readiness probe failed"},
					svcEntry{id: "machined", state: "Running", unknown: true},
					svcEntry{id: "trustd", state: "Running", healthy: true},
					svcEntry{id: "udevd", state: "Running", healthy: true},
				),
			},
			wantStatus: output.Critical,
			wantSubstr: "1/8 services unhealthy: kubelet",
		},
		{
			name: "CRITICAL - two services unhealthy",
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "apid", state: "Running", healthy: true},
					svcEntry{id: "containerd", state: "Running", unknown: true},
					svcEntry{id: "cri", state: "Running", healthy: true},
					svcEntry{id: "etcd", state: "Starting", unknown: true},
					svcEntry{id: "kubelet", state: "Finished", healthy: false, message: "readiness probe failed"},
					svcEntry{id: "machined", state: "Running", unknown: true},
					svcEntry{id: "trustd", state: "Running", healthy: true},
					svcEntry{id: "udevd", state: "Running", healthy: true},
				),
			},
			wantStatus: output.Critical,
			wantSubstr: "2/8 services unhealthy: etcd, kubelet",
		},
		{
			name:    "OK - excluded service is down",
			exclude: []string{"kubelet"},
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "apid", state: "Running", healthy: true},
					svcEntry{id: "containerd", state: "Running", unknown: true},
					svcEntry{id: "kubelet", state: "Finished", healthy: false, message: "readiness probe failed"},
					svcEntry{id: "trustd", state: "Running", healthy: true},
				),
			},
			wantStatus: output.OK,
			wantSubstr: "3/3 services healthy",
		},
		{
			name:    "OK - include filter only checks specified services",
			include: []string{"apid", "trustd"},
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "apid", state: "Running", healthy: true},
					svcEntry{id: "containerd", state: "Running", unknown: true},
					svcEntry{id: "kubelet", state: "Finished", healthy: false, message: "readiness probe failed"},
					svcEntry{id: "trustd", state: "Running", healthy: true},
				),
			},
			wantStatus: output.OK,
			wantSubstr: "2/2 services healthy",
		},
		{
			name:    "CRITICAL - include filter catches unhealthy service",
			include: []string{"kubelet", "apid"},
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "apid", state: "Running", healthy: true},
					svcEntry{id: "kubelet", state: "Finished", healthy: false, message: "readiness probe failed"},
				),
			},
			wantStatus: output.Critical,
			wantSubstr: "1/2 services unhealthy: kubelet",
		},
		{
			name: "CRITICAL - service Running but unhealthy",
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "kubelet", state: "Running", healthy: false, message: "readiness probe failed"},
				),
			},
			wantStatus: output.Critical,
			wantSubstr: "1/1 services unhealthy: kubelet",
		},
		{
			name: "CRITICAL - service not Running with healthy flag (state takes precedence)",
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "kubelet", state: "Finished", healthy: true},
				),
			},
			wantStatus: output.Critical,
			wantSubstr: "1/1 services unhealthy: kubelet",
		},
		{
			name: "OK - service Running with unknown health",
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "containerd", state: "Running", unknown: true},
				),
			},
			wantStatus: output.OK,
			wantSubstr: "1/1 services healthy",
		},
		{
			name: "UNKNOWN - nil response",
			client: &mockServicesClient{
				resp: nil,
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name: "UNKNOWN - empty messages",
			client: &mockServicesClient{
				resp: &machine.ServiceListResponse{},
			},
			wantStatus: output.Unknown,
			wantSubstr: "Empty response from Talos API",
		},
		{
			name: "UNKNOWN - empty services list",
			client: &mockServicesClient{
				resp: &machine.ServiceListResponse{
					Messages: []*machine.ServiceList{
						{Services: []*machine.ServiceInfo{}},
					},
				},
			},
			wantStatus: output.Unknown,
			wantSubstr: "No services in response",
		},
		{
			name: "error from client",
			client: &mockServicesClient{
				err: fmt.Errorf("connection refused"),
			},
			wantErr: true,
		},
		{
			name: "CRITICAL - nil health on Running service",
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "kubelet", state: "Running", nilHealth: true},
				),
			},
			wantStatus: output.Critical,
			wantSubstr: "1/1 services unhealthy: kubelet",
		},
		{
			name:    "OK - exclude multiple services",
			exclude: []string{"apid", "trustd"},
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "apid", state: "Finished", healthy: false},
					svcEntry{id: "kubelet", state: "Running", healthy: true},
					svcEntry{id: "trustd", state: "Finished", healthy: false},
				),
			},
			wantStatus: output.OK,
			wantSubstr: "1/1 services healthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewServicesCheck(tt.include, tt.exclude)
			if err != nil {
				t.Fatalf("NewServicesCheck: %v", err)
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

			if result.CheckName != "SERVICES" {
				t.Errorf("CheckName = %q, want %q", result.CheckName, "SERVICES")
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

func TestServicesCheckPerfData(t *testing.T) {
	ch, err := NewServicesCheck(nil, nil)
	if err != nil {
		t.Fatalf("NewServicesCheck: %v", err)
	}

	client := &mockServicesClient{
		resp: makeServiceListResponse(
			svcEntry{id: "apid", state: "Running", healthy: true},
			svcEntry{id: "containerd", state: "Running", unknown: true},
			svcEntry{id: "cri", state: "Running", healthy: true},
			svcEntry{id: "etcd", state: "Running", healthy: true},
			svcEntry{id: "kubelet", state: "Finished", healthy: false, message: "readiness probe failed"},
			svcEntry{id: "machined", state: "Running", unknown: true},
			svcEntry{id: "trustd", state: "Running", healthy: true},
			svcEntry{id: "udevd", state: "Running", healthy: true},
		),
	}

	result, err := ch.Run(context.Background(), client)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.PerfData) != 3 {
		t.Fatalf("PerfData length = %d, want 3", len(result.PerfData))
	}

	// services_total
	pd := result.PerfData[0]
	if pd.Label != "services_total" {
		t.Errorf("PerfData[0].Label = %q, want %q", pd.Label, "services_total")
	}
	if pd.Value != 8 {
		t.Errorf("PerfData[0].Value = %v, want %v", pd.Value, 8)
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

	// services_healthy
	pd = result.PerfData[1]
	if pd.Label != "services_healthy" {
		t.Errorf("PerfData[1].Label = %q, want %q", pd.Label, "services_healthy")
	}
	if pd.Value != 7 {
		t.Errorf("PerfData[1].Value = %v, want %v", pd.Value, 7)
	}
	if pd.Min != "0" {
		t.Errorf("PerfData[1].Min = %q, want %q", pd.Min, "0")
	}

	// services_unhealthy
	pd = result.PerfData[2]
	if pd.Label != "services_unhealthy" {
		t.Errorf("PerfData[2].Label = %q, want %q", pd.Label, "services_unhealthy")
	}
	if pd.Value != 1 {
		t.Errorf("PerfData[2].Value = %v, want %v", pd.Value, 1)
	}
	if pd.Min != "0" {
		t.Errorf("PerfData[2].Min = %q, want %q", pd.Min, "0")
	}
}

func TestServicesCheckOutputFormat(t *testing.T) {
	tests := []struct {
		name    string
		include []string
		exclude []string
		client  *mockServicesClient
		want    string
	}{
		{
			name: "OK output matches DESIGN.md format",
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "apid", state: "Running", healthy: true},
					svcEntry{id: "containerd", state: "Running", unknown: true},
					svcEntry{id: "cri", state: "Running", healthy: true},
					svcEntry{id: "etcd", state: "Running", healthy: true},
					svcEntry{id: "kubelet", state: "Running", healthy: true},
					svcEntry{id: "machined", state: "Running", unknown: true},
					svcEntry{id: "trustd", state: "Running", healthy: true},
					svcEntry{id: "udevd", state: "Running", healthy: true},
				),
			},
			want: "TALOS SERVICES OK - 8/8 services healthy | services_total=8;;;0; services_healthy=8;;;0; services_unhealthy=0;;;0;",
		},
		{
			name: "CRITICAL single unhealthy matches DESIGN.md format",
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "apid", state: "Running", healthy: true},
					svcEntry{id: "containerd", state: "Running", unknown: true},
					svcEntry{id: "cri", state: "Running", healthy: true},
					svcEntry{id: "etcd", state: "Running", healthy: true},
					svcEntry{id: "kubelet", state: "Finished", healthy: false, message: "readiness probe failed"},
					svcEntry{id: "machined", state: "Running", unknown: true},
					svcEntry{id: "trustd", state: "Running", healthy: true},
					svcEntry{id: "udevd", state: "Running", healthy: true},
				),
			},
			want: "TALOS SERVICES CRITICAL - 1/8 services unhealthy: kubelet | services_total=8;;;0; services_healthy=7;;;0; services_unhealthy=1;;;0;\nkubelet: state=Finished, health=unhealthy, message=\"readiness probe failed\"",
		},
		{
			name: "CRITICAL two unhealthy matches DESIGN.md format",
			client: &mockServicesClient{
				resp: makeServiceListResponse(
					svcEntry{id: "apid", state: "Running", healthy: true},
					svcEntry{id: "containerd", state: "Running", unknown: true},
					svcEntry{id: "cri", state: "Running", healthy: true},
					svcEntry{id: "etcd", state: "Starting", unknown: true},
					svcEntry{id: "kubelet", state: "Finished", healthy: false, message: "readiness probe failed"},
					svcEntry{id: "machined", state: "Running", unknown: true},
					svcEntry{id: "trustd", state: "Running", healthy: true},
					svcEntry{id: "udevd", state: "Running", healthy: true},
				),
			},
			want: "TALOS SERVICES CRITICAL - 2/8 services unhealthy: etcd, kubelet | services_total=8;;;0; services_healthy=6;;;0; services_unhealthy=2;;;0;\netcd: state=Starting, health=unknown, message=\"\"\nkubelet: state=Finished, health=unhealthy, message=\"readiness probe failed\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewServicesCheck(tt.include, tt.exclude)
			if err != nil {
				t.Fatalf("NewServicesCheck: %v", err)
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

func TestServicesCheckDetails(t *testing.T) {
	// Verify that long text details are only present for unhealthy results.
	t.Run("OK has no details", func(t *testing.T) {
		ch, _ := NewServicesCheck(nil, nil)
		client := &mockServicesClient{
			resp: makeServiceListResponse(
				svcEntry{id: "apid", state: "Running", healthy: true},
			),
		}
		result, err := ch.Run(context.Background(), client)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Details != "" {
			t.Errorf("OK result should have no details, got %q", result.Details)
		}
	})

	t.Run("CRITICAL has details", func(t *testing.T) {
		ch, _ := NewServicesCheck(nil, nil)
		client := &mockServicesClient{
			resp: makeServiceListResponse(
				svcEntry{id: "kubelet", state: "Finished", healthy: false, message: "readiness probe failed"},
			),
		}
		result, err := ch.Run(context.Background(), client)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Details == "" {
			t.Error("CRITICAL result should have details")
		}
		wantDetail := `kubelet: state=Finished, health=unhealthy, message="readiness probe failed"`
		if result.Details != wantDetail {
			t.Errorf("details:\n  got:  %q\n  want: %q", result.Details, wantDetail)
		}
	})
}

// svcEntry describes a service for building test responses.
type svcEntry struct {
	id        string
	state     string
	healthy   bool
	unknown   bool
	message   string
	nilHealth bool
}

// makeServiceListResponse builds a ServiceListResponse from service entries.
func makeServiceListResponse(entries ...svcEntry) *machine.ServiceListResponse {
	services := make([]*machine.ServiceInfo, len(entries))
	for i, e := range entries {
		svc := &machine.ServiceInfo{
			Id:    e.id,
			State: e.state,
		}
		if !e.nilHealth {
			svc.Health = &machine.ServiceHealth{
				Healthy:     e.healthy,
				Unknown:     e.unknown,
				LastMessage: e.message,
			}
		}
		services[i] = svc
	}
	return &machine.ServiceListResponse{
		Messages: []*machine.ServiceList{
			{
				Services: services,
			},
		},
	}
}
