// Package check defines the Check interface and concrete implementations
// for Talos Linux monitoring checks (CPU, memory, disk, services, etcd, load).
// Each check queries the Talos gRPC API and returns a structured Result.
package check

import (
	"context"

	"github.com/DLAKE-IO/check-talos/internal/output"
)

// Check is the interface that all monitoring checks must implement.
type Check interface {
	// Name returns the uppercase check identifier used in the Nagios
	// output prefix (e.g., "CPU", "MEMORY", "DISK", "SERVICES", "ETCD", "LOAD").
	Name() string

	// Run executes the check against the Talos API and returns a Result.
	// The context carries the gRPC deadline set by --timeout.
	Run(ctx context.Context, client TalosClient) (*output.Result, error)
}
