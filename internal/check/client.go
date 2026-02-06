package check

import (
	"context"

	"github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// TalosClient defines the Talos API operations used by monitoring checks.
// This interface is satisfied by the wrapper in internal/talos and allows
// checks to be unit-tested with mock implementations.
type TalosClient interface {
	// SystemStat returns CPU counters and process statistics.
	// Used by: CPU check (usage calculation), Load check (CPU count for auto-thresholds).
	SystemStat(ctx context.Context) (*machine.SystemStatResponse, error)

	// Memory returns /proc/meminfo-equivalent memory statistics.
	// Used by: Memory check.
	Memory(ctx context.Context) (*machine.MemoryResponse, error)

	// Mounts returns mount point capacity and availability.
	// Used by: Disk check.
	Mounts(ctx context.Context) (*machine.MountsResponse, error)

	// ServiceList returns the list of Talos system services with state and health.
	// Used by: Services check.
	ServiceList(ctx context.Context) (*machine.ServiceListResponse, error)

	// EtcdStatus returns etcd member status including leader, DB size, and errors.
	// Used by: Etcd check.
	EtcdStatus(ctx context.Context) (*machine.EtcdStatusResponse, error)

	// EtcdMemberList returns the list of etcd cluster members.
	// Used by: Etcd check.
	EtcdMemberList(ctx context.Context) (*machine.EtcdMemberListResponse, error)

	// EtcdAlarmList returns active etcd alarms (NOSPACE, CORRUPT, etc.).
	// Used by: Etcd check.
	EtcdAlarmList(ctx context.Context) (*machine.EtcdAlarmListResponse, error)

	// LoadAvg returns 1/5/15-minute load averages.
	// Used by: Load check.
	LoadAvg(ctx context.Context) (*machine.LoadAvgResponse, error)
}
