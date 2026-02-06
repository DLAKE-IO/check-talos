package output

import (
	"testing"

	nagios "github.com/atc0005/go-nagios"
)

func TestStatusString(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{OK, "OK"},
		{Warning, "WARNING"},
		{Critical, "CRITICAL"},
		{Unknown, "UNKNOWN"},
		{Status(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestStatusExitCode(t *testing.T) {
	tests := []struct {
		status Status
		want   int
	}{
		{OK, 0},
		{Warning, 1},
		{Critical, 2},
		{Unknown, 3},
	}
	for _, tt := range tests {
		if got := tt.status.ExitCode(); got != tt.want {
			t.Errorf("Status(%d).ExitCode() = %d, want %d", tt.status, got, tt.want)
		}
	}
}

func TestPerfDatumString(t *testing.T) {
	tests := []struct {
		name string
		pd   PerfDatum
		want string
	}{
		{
			name: "cpu usage with percentage UOM",
			pd:   PerfDatum{Label: "cpu_usage", Value: 34.2, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
			want: "cpu_usage=34.2%;80;90;0;100",
		},
		{
			name: "memory used in bytes",
			pd:   PerfDatum{Label: "memory_used", Value: 5348024320, UOM: "B", Warn: "", Crit: "", Min: "0", Max: "8589934592"},
			want: "memory_used=5348024320B;;;0;8589934592",
		},
		{
			name: "memory total in bytes no max",
			pd:   PerfDatum{Label: "memory_total", Value: 8589934592, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
			want: "memory_total=8589934592B;;;0;",
		},
		{
			name: "etcd members dimensionless",
			pd:   PerfDatum{Label: "etcd_members", Value: 3, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
			want: "etcd_members=3;;;0;",
		},
		{
			name: "etcd dbsize with thresholds",
			pd:   PerfDatum{Label: "etcd_dbsize", Value: 13107200, UOM: "B", Warn: "100000000", Crit: "200000000", Min: "0", Max: ""},
			want: "etcd_dbsize=13107200B;100000000;200000000;0;",
		},
		{
			name: "etcd dbsize in use no thresholds",
			pd:   PerfDatum{Label: "etcd_dbsize_in_use", Value: 8388608, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
			want: "etcd_dbsize_in_use=8388608B;;;0;",
		},
		{
			name: "load average with thresholds",
			pd:   PerfDatum{Label: "load5", Value: 1.23, UOM: "", Warn: "4", Crit: "8", Min: "0", Max: ""},
			want: "load5=1.23;4;8;0;",
		},
		{
			name: "load average without thresholds",
			pd:   PerfDatum{Label: "load1", Value: 0.98, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
			want: "load1=0.98;;;0;",
		},
		{
			name: "services count no thresholds",
			pd:   PerfDatum{Label: "services_total", Value: 8, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
			want: "services_total=8;;;0;",
		},
		{
			name: "zero value",
			pd:   PerfDatum{Label: "services_unhealthy", Value: 0, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
			want: "services_unhealthy=0;;;0;",
		},
		{
			name: "disk usage with all fields",
			pd:   PerfDatum{Label: "disk_usage", Value: 84.2, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
			want: "disk_usage=84.2%;80;90;0;100",
		},
		{
			name: "disk total no thresholds",
			pd:   PerfDatum{Label: "disk_total", Value: 21474836480, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
			want: "disk_total=21474836480B;;;0;",
		},
		{
			name: "nagios range threshold in warn/crit",
			pd:   PerfDatum{Label: "etcd_dbsize", Value: 117878784, UOM: "B", Warn: "~:100000000", Crit: "~:200000000", Min: "0", Max: ""},
			want: "etcd_dbsize=117878784B;~:100000000;~:200000000;0;",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pd.String(); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestFormatPerfData(t *testing.T) {
	t.Run("single item", func(t *testing.T) {
		data := []PerfDatum{
			{Label: "cpu_usage", Value: 34.2, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
		}
		want := "cpu_usage=34.2%;80;90;0;100"
		if got := FormatPerfData(data); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("multiple items", func(t *testing.T) {
		data := []PerfDatum{
			{Label: "memory_usage", Value: 62.3, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
			{Label: "memory_used", Value: 5348024320, UOM: "B", Warn: "", Crit: "", Min: "0", Max: "8589934592"},
			{Label: "memory_total", Value: 8589934592, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
		}
		want := "memory_usage=62.3%;80;90;0;100 memory_used=5348024320B;;;0;8589934592 memory_total=8589934592B;;;0;"
		if got := FormatPerfData(data); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("nil slice", func(t *testing.T) {
		if got := FormatPerfData(nil); got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		if got := FormatPerfData([]PerfDatum{}); got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("three load values with selective thresholds", func(t *testing.T) {
		data := []PerfDatum{
			{Label: "load1", Value: 0.98, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
			{Label: "load5", Value: 1.23, UOM: "", Warn: "4", Crit: "8", Min: "0", Max: ""},
			{Label: "load15", Value: 1.45, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
		}
		want := "load1=0.98;;;0; load5=1.23;4;8;0; load15=1.45;;;0;"
		if got := FormatPerfData(data); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})
}

func TestResultString(t *testing.T) {
	tests := []struct {
		name   string
		result Result
		want   string
	}{
		// --- CPU check outputs (DESIGN.md Section 4.7.1) ---
		{
			name: "CPU OK",
			result: Result{
				Status:    OK,
				CheckName: "CPU",
				Summary:   "CPU usage 34.2%",
				PerfData: []PerfDatum{
					{Label: "cpu_usage", Value: 34.2, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
				},
			},
			want: "TALOS CPU OK - CPU usage 34.2% | cpu_usage=34.2%;80;90;0;100",
		},
		{
			name: "CPU WARNING",
			result: Result{
				Status:    Warning,
				CheckName: "CPU",
				Summary:   "CPU usage 82.5%",
				PerfData: []PerfDatum{
					{Label: "cpu_usage", Value: 82.5, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
				},
			},
			want: "TALOS CPU WARNING - CPU usage 82.5% | cpu_usage=82.5%;80;90;0;100",
		},
		{
			name: "CPU CRITICAL",
			result: Result{
				Status:    Critical,
				CheckName: "CPU",
				Summary:   "CPU usage 96.3%",
				PerfData: []PerfDatum{
					{Label: "cpu_usage", Value: 96.3, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
				},
			},
			want: "TALOS CPU CRITICAL - CPU usage 96.3% | cpu_usage=96.3%;80;90;0;100",
		},
		{
			name: "CPU UNKNOWN invalid threshold",
			result: Result{
				Status:    Unknown,
				CheckName: "CPU",
				Summary:   `Invalid warning threshold "abc": expected Nagios range format`,
			},
			want: `TALOS CPU UNKNOWN - Invalid warning threshold "abc": expected Nagios range format`,
		},
		{
			name: "CPU CRITICAL timeout",
			result: Result{
				Status:    Critical,
				CheckName: "CPU",
				Summary:   "Talos API timeout after 10s",
			},
			want: "TALOS CPU CRITICAL - Talos API timeout after 10s",
		},

		// --- MEMORY check outputs (DESIGN.md Section 4.7.2) ---
		{
			name: "MEMORY OK",
			result: Result{
				Status:    OK,
				CheckName: "MEMORY",
				Summary:   "Memory usage 62.3% (4.98 GB / 8.00 GB)",
				PerfData: []PerfDatum{
					{Label: "memory_usage", Value: 62.3, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
					{Label: "memory_used", Value: 5348024320, UOM: "B", Warn: "", Crit: "", Min: "0", Max: "8589934592"},
					{Label: "memory_total", Value: 8589934592, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS MEMORY OK - Memory usage 62.3% (4.98 GB / 8.00 GB) | memory_usage=62.3%;80;90;0;100 memory_used=5348024320B;;;0;8589934592 memory_total=8589934592B;;;0;",
		},
		{
			name: "MEMORY WARNING",
			result: Result{
				Status:    Warning,
				CheckName: "MEMORY",
				Summary:   "Memory usage 83.7% (6.70 GB / 8.00 GB)",
				PerfData: []PerfDatum{
					{Label: "memory_usage", Value: 83.7, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
					{Label: "memory_used", Value: 7193739264, UOM: "B", Warn: "", Crit: "", Min: "0", Max: "8589934592"},
					{Label: "memory_total", Value: 8589934592, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS MEMORY WARNING - Memory usage 83.7% (6.70 GB / 8.00 GB) | memory_usage=83.7%;80;90;0;100 memory_used=7193739264B;;;0;8589934592 memory_total=8589934592B;;;0;",
		},
		{
			name: "MEMORY CRITICAL",
			result: Result{
				Status:    Critical,
				CheckName: "MEMORY",
				Summary:   "Memory usage 94.1% (7.53 GB / 8.00 GB)",
				PerfData: []PerfDatum{
					{Label: "memory_usage", Value: 94.1, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
					{Label: "memory_used", Value: 8083886080, UOM: "B", Warn: "", Crit: "", Min: "0", Max: "8589934592"},
					{Label: "memory_total", Value: 8589934592, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS MEMORY CRITICAL - Memory usage 94.1% (7.53 GB / 8.00 GB) | memory_usage=94.1%;80;90;0;100 memory_used=8083886080B;;;0;8589934592 memory_total=8589934592B;;;0;",
		},
		{
			name: "MEMORY CRITICAL API unavailable",
			result: Result{
				Status:    Critical,
				CheckName: "MEMORY",
				Summary:   "Talos API unavailable: transport is closing",
			},
			want: "TALOS MEMORY CRITICAL - Talos API unavailable: transport is closing",
		},

		// --- DISK check outputs (DESIGN.md Section 4.7.3) ---
		{
			name: "DISK OK root",
			result: Result{
				Status:    OK,
				CheckName: "DISK",
				Summary:   "/ usage 45.1% (9.67 GB / 21.47 GB)",
				PerfData: []PerfDatum{
					{Label: "disk_usage", Value: 45.1, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
					{Label: "disk_used", Value: 9663676416, UOM: "B", Warn: "", Crit: "", Min: "0", Max: "21474836480"},
					{Label: "disk_total", Value: 21474836480, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS DISK OK - / usage 45.1% (9.67 GB / 21.47 GB) | disk_usage=45.1%;80;90;0;100 disk_used=9663676416B;;;0;21474836480 disk_total=21474836480B;;;0;",
		},
		{
			name: "DISK WARNING /var",
			result: Result{
				Status:    Warning,
				CheckName: "DISK",
				Summary:   "/var usage 84.2% (42.10 GB / 50.00 GB)",
				PerfData: []PerfDatum{
					{Label: "disk_usage", Value: 84.2, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
					{Label: "disk_used", Value: 45204377190, UOM: "B", Warn: "", Crit: "", Min: "0", Max: "53687091200"},
					{Label: "disk_total", Value: 53687091200, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS DISK WARNING - /var usage 84.2% (42.10 GB / 50.00 GB) | disk_usage=84.2%;80;90;0;100 disk_used=45204377190B;;;0;53687091200 disk_total=53687091200B;;;0;",
		},
		{
			name: "DISK UNKNOWN mount not found",
			result: Result{
				Status:    Unknown,
				CheckName: "DISK",
				Summary:   "Mount point /data not found",
			},
			want: "TALOS DISK UNKNOWN - Mount point /data not found",
		},
		{
			name: "DISK CRITICAL timeout",
			result: Result{
				Status:    Critical,
				CheckName: "DISK",
				Summary:   "Talos API timeout after 10s",
			},
			want: "TALOS DISK CRITICAL - Talos API timeout after 10s",
		},

		// --- SERVICES check outputs (DESIGN.md Section 4.7.4) ---
		{
			name: "SERVICES OK",
			result: Result{
				Status:    OK,
				CheckName: "SERVICES",
				Summary:   "8/8 services healthy",
				PerfData: []PerfDatum{
					{Label: "services_total", Value: 8, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "services_healthy", Value: 8, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "services_unhealthy", Value: 0, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS SERVICES OK - 8/8 services healthy | services_total=8;;;0; services_healthy=8;;;0; services_unhealthy=0;;;0;",
		},
		{
			name: "SERVICES CRITICAL single unhealthy with details",
			result: Result{
				Status:    Critical,
				CheckName: "SERVICES",
				Summary:   "1/8 services unhealthy: kubelet",
				Details:   `kubelet: state=Finished, health=unhealthy, message="readiness probe failed"`,
				PerfData: []PerfDatum{
					{Label: "services_total", Value: 8, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "services_healthy", Value: 7, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "services_unhealthy", Value: 1, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS SERVICES CRITICAL - 1/8 services unhealthy: kubelet | services_total=8;;;0; services_healthy=7;;;0; services_unhealthy=1;;;0;\n" +
				`kubelet: state=Finished, health=unhealthy, message="readiness probe failed"`,
		},
		{
			name: "SERVICES CRITICAL multiple unhealthy with details",
			result: Result{
				Status:    Critical,
				CheckName: "SERVICES",
				Summary:   "2/8 services unhealthy: kubelet, etcd",
				Details: `kubelet: state=Finished, health=unhealthy, message="readiness probe failed"` + "\n" +
					`etcd: state=Starting, health=unknown, message=""`,
				PerfData: []PerfDatum{
					{Label: "services_total", Value: 8, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "services_healthy", Value: 6, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "services_unhealthy", Value: 2, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS SERVICES CRITICAL - 2/8 services unhealthy: kubelet, etcd | services_total=8;;;0; services_healthy=6;;;0; services_unhealthy=2;;;0;\n" +
				`kubelet: state=Finished, health=unhealthy, message="readiness probe failed"` + "\n" +
				`etcd: state=Starting, health=unknown, message=""`,
		},

		// --- ETCD check outputs (DESIGN.md Section 4.7.5) ---
		{
			name: "ETCD OK",
			result: Result{
				Status:    OK,
				CheckName: "ETCD",
				Summary:   "Leader 1234, 3/3 members, DB 12.50 MB",
				PerfData: []PerfDatum{
					{Label: "etcd_dbsize", Value: 13107200, UOM: "B", Warn: "100000000", Crit: "200000000", Min: "0", Max: ""},
					{Label: "etcd_dbsize_in_use", Value: 8388608, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "etcd_members", Value: 3, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS ETCD OK - Leader 1234, 3/3 members, DB 12.50 MB | etcd_dbsize=13107200B;100000000;200000000;0; etcd_dbsize_in_use=8388608B;;;0; etcd_members=3;;;0;",
		},
		{
			name: "ETCD WARNING DB size",
			result: Result{
				Status:    Warning,
				CheckName: "ETCD",
				Summary:   "Leader 1234, 3/3 members, DB 112.42 MB",
				PerfData: []PerfDatum{
					{Label: "etcd_dbsize", Value: 117878784, UOM: "B", Warn: "100000000", Crit: "200000000", Min: "0", Max: ""},
					{Label: "etcd_dbsize_in_use", Value: 96468992, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "etcd_members", Value: 3, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS ETCD WARNING - Leader 1234, 3/3 members, DB 112.42 MB | etcd_dbsize=117878784B;100000000;200000000;0; etcd_dbsize_in_use=96468992B;;;0; etcd_members=3;;;0;",
		},
		{
			name: "ETCD CRITICAL no leader",
			result: Result{
				Status:    Critical,
				CheckName: "ETCD",
				Summary:   "No leader elected",
				PerfData: []PerfDatum{
					{Label: "etcd_dbsize", Value: 45000000, UOM: "B", Warn: "100000000", Crit: "200000000", Min: "0", Max: ""},
					{Label: "etcd_dbsize_in_use", Value: 40000000, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "etcd_members", Value: 3, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS ETCD CRITICAL - No leader elected | etcd_dbsize=45000000B;100000000;200000000;0; etcd_dbsize_in_use=40000000B;;;0; etcd_members=3;;;0;",
		},
		{
			name: "ETCD CRITICAL member count below minimum",
			result: Result{
				Status:    Critical,
				CheckName: "ETCD",
				Summary:   "Member count 2 below minimum 3",
				PerfData: []PerfDatum{
					{Label: "etcd_dbsize", Value: 13107200, UOM: "B", Warn: "100000000", Crit: "200000000", Min: "0", Max: ""},
					{Label: "etcd_dbsize_in_use", Value: 8388608, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "etcd_members", Value: 2, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS ETCD CRITICAL - Member count 2 below minimum 3 | etcd_dbsize=13107200B;100000000;200000000;0; etcd_dbsize_in_use=8388608B;;;0; etcd_members=2;;;0;",
		},
		{
			name: "ETCD CRITICAL active alarm",
			result: Result{
				Status:    Critical,
				CheckName: "ETCD",
				Summary:   "Active alarm: NOSPACE",
				PerfData: []PerfDatum{
					{Label: "etcd_dbsize", Value: 2147483648, UOM: "B", Warn: "100000000", Crit: "200000000", Min: "0", Max: ""},
					{Label: "etcd_dbsize_in_use", Value: 2000000000, UOM: "B", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "etcd_members", Value: 3, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS ETCD CRITICAL - Active alarm: NOSPACE | etcd_dbsize=2147483648B;100000000;200000000;0; etcd_dbsize_in_use=2000000000B;;;0; etcd_members=3;;;0;",
		},
		{
			name: "ETCD UNKNOWN RPC failed",
			result: Result{
				Status:    Unknown,
				CheckName: "ETCD",
				Summary:   "EtcdStatus RPC failed: etcd not running on this node",
			},
			want: "TALOS ETCD UNKNOWN - EtcdStatus RPC failed: etcd not running on this node",
		},

		// --- LOAD check outputs (DESIGN.md Section 4.7.6) ---
		{
			name: "LOAD OK 5m period",
			result: Result{
				Status:    OK,
				CheckName: "LOAD",
				Summary:   "Load average (5m) 1.23",
				PerfData: []PerfDatum{
					{Label: "load1", Value: 0.98, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "load5", Value: 1.23, UOM: "", Warn: "4", Crit: "8", Min: "0", Max: ""},
					{Label: "load15", Value: 1.45, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS LOAD OK - Load average (5m) 1.23 | load1=0.98;;;0; load5=1.23;4;8;0; load15=1.45;;;0;",
		},
		{
			name: "LOAD WARNING 5m period",
			result: Result{
				Status:    Warning,
				CheckName: "LOAD",
				Summary:   "Load average (5m) 4.56",
				PerfData: []PerfDatum{
					{Label: "load1", Value: 5.12, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "load5", Value: 4.56, UOM: "", Warn: "4", Crit: "8", Min: "0", Max: ""},
					{Label: "load15", Value: 3.21, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS LOAD WARNING - Load average (5m) 4.56 | load1=5.12;;;0; load5=4.56;4;8;0; load15=3.21;;;0;",
		},
		{
			name: "LOAD CRITICAL 5m period",
			result: Result{
				Status:    Critical,
				CheckName: "LOAD",
				Summary:   "Load average (5m) 9.87",
				PerfData: []PerfDatum{
					{Label: "load1", Value: 11.02, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "load5", Value: 9.87, UOM: "", Warn: "4", Crit: "8", Min: "0", Max: ""},
					{Label: "load15", Value: 7.65, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS LOAD CRITICAL - Load average (5m) 9.87 | load1=11.02;;;0; load5=9.87;4;8;0; load15=7.65;;;0;",
		},
		{
			name: "LOAD OK 1m period thresholds on load1",
			result: Result{
				Status:    OK,
				CheckName: "LOAD",
				Summary:   "Load average (1m) 2.1",
				PerfData: []PerfDatum{
					{Label: "load1", Value: 2.1, UOM: "", Warn: "4", Crit: "8", Min: "0", Max: ""},
					{Label: "load5", Value: 1.85, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
					{Label: "load15", Value: 1.45, UOM: "", Warn: "", Crit: "", Min: "0", Max: ""},
				},
			},
			want: "TALOS LOAD OK - Load average (1m) 2.1 | load1=2.1;4;8;0; load5=1.85;;;0; load15=1.45;;;0;",
		},
		{
			name: "LOAD CRITICAL timeout",
			result: Result{
				Status:    Critical,
				CheckName: "LOAD",
				Summary:   "Talos API timeout after 10s",
			},
			want: "TALOS LOAD CRITICAL - Talos API timeout after 10s",
		},

		// --- Edge cases ---
		{
			name: "no perfdata no details",
			result: Result{
				Status:    Unknown,
				CheckName: "CPU",
				Summary:   "No authentication configured",
			},
			want: "TALOS CPU UNKNOWN - No authentication configured",
		},
		{
			name: "connection refused",
			result: Result{
				Status:    Critical,
				CheckName: "CPU",
				Summary:   "Connection refused: 10.0.0.1:50000",
			},
			want: "TALOS CPU CRITICAL - Connection refused: 10.0.0.1:50000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.String()
			if got != tt.want {
				t.Errorf("\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		bytes uint64
		want  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{10240, "10.00 KB"},
		{1048576, "1.00 MB"},
		{13107200, "12.50 MB"},
		{117878784, "112.42 MB"},
		{1073741824, "1.00 GB"},
		{5348024320, "4.98 GB"},
		{7193739264, "6.70 GB"},
		{8083886080, "7.53 GB"},
		{8589934592, "8.00 GB"},
		{9663676416, "9.00 GB"},
		{21474836480, "20.00 GB"},
		{45204377190, "42.10 GB"},
		{53687091200, "50.00 GB"},
		{1099511627776, "1.00 TB"},
		{2199023255552, "2.00 TB"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := HumanBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("HumanBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestApplyToPlugin(t *testing.T) {
	tests := []struct {
		name        string
		result      Result
		wantExit    int
		wantOutput  string
		wantDetails string
	}{
		{
			name: "OK with perfdata",
			result: Result{
				Status:    OK,
				CheckName: "CPU",
				Summary:   "CPU usage 34.2%",
				PerfData: []PerfDatum{
					{Label: "cpu_usage", Value: 34.2, UOM: "%", Warn: "80", Crit: "90", Min: "0", Max: "100"},
				},
			},
			wantExit:   nagios.StateOKExitCode,
			wantOutput: "TALOS CPU OK - CPU usage 34.2%",
		},
		{
			name: "WARNING",
			result: Result{
				Status:    Warning,
				CheckName: "LOAD",
				Summary:   "Load average (5m) 4.56",
			},
			wantExit:   nagios.StateWARNINGExitCode,
			wantOutput: "TALOS LOAD WARNING - Load average (5m) 4.56",
		},
		{
			name: "CRITICAL with details",
			result: Result{
				Status:    Critical,
				CheckName: "SERVICES",
				Summary:   "1/8 services unhealthy: kubelet",
				Details:   `kubelet: state=Finished, health=unhealthy, message="readiness probe failed"`,
			},
			wantExit:    nagios.StateCRITICALExitCode,
			wantOutput:  "TALOS SERVICES CRITICAL - 1/8 services unhealthy: kubelet",
			wantDetails: `kubelet: state=Finished, health=unhealthy, message="readiness probe failed"`,
		},
		{
			name: "UNKNOWN",
			result: Result{
				Status:    Unknown,
				CheckName: "DISK",
				Summary:   "Mount point /data not found",
			},
			wantExit:   nagios.StateUNKNOWNExitCode,
			wantOutput: "TALOS DISK UNKNOWN - Mount point /data not found",
		},
		{
			name: "out of range status maps to UNKNOWN",
			result: Result{
				Status:    Status(99),
				CheckName: "CPU",
				Summary:   "Something unexpected",
			},
			wantExit:   nagios.StateUNKNOWNExitCode,
			wantOutput: "TALOS CPU UNKNOWN - Something unexpected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := nagios.NewPlugin()
			tt.result.ApplyToPlugin(p)

			if p.ExitStatusCode != tt.wantExit {
				t.Errorf("ExitStatusCode = %d, want %d", p.ExitStatusCode, tt.wantExit)
			}
			if p.ServiceOutput != tt.wantOutput {
				t.Errorf("ServiceOutput = %q, want %q", p.ServiceOutput, tt.wantOutput)
			}
			if tt.wantDetails != "" && p.LongServiceOutput != tt.wantDetails {
				t.Errorf("LongServiceOutput = %q, want %q", p.LongServiceOutput, tt.wantDetails)
			}
		})
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		value float64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{3, "3"},
		{8, "8"},
		{100, "100"},
		{13107200, "13107200"},
		{5348024320, "5348024320"},
		{8589934592, "8589934592"},
		{2147483648, "2147483648"},
		{34.2, "34.2"},
		{82.5, "82.5"},
		{94.1, "94.1"},
		{62.3, "62.3"},
		{84.2, "84.2"},
		{96.3, "96.3"},
		{1.23, "1.23"},
		{0.98, "0.98"},
		{4.56, "4.56"},
		{9.87, "9.87"},
		{5.12, "5.12"},
		{11.02, "11.02"},
		{-5, "-5"},
		{-1.5, "-1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatValue(tt.value)
			if got != tt.want {
				t.Errorf("formatValue(%v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
