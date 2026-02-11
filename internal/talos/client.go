// Package talos provides a thin wrapper around the official Talos machinery
// gRPC client. It handles mTLS setup, connection lifecycle, and node targeting,
// exposing convenience methods used by monitoring checks.
package talos

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Config holds the configuration for connecting to the Talos API.
type Config struct {
	Endpoint     string
	CA           string
	Cert         string
	Key          string
	TalosConfig  string
	TalosContext string
	Node         string
	Timeout      time.Duration
}

// Client wraps the Talos machinery gRPC client and satisfies
// the check.TalosClient interface.
type Client struct {
	inner *talosclient.Client
	node  string
}

// NewClient creates a Talos API client based on the provided configuration.
//
// Authentication precedence:
//  1. Explicit cert paths (--talos-ca, --talos-cert, --talos-key) — all three required
//  2. Talosconfig file (--talosconfig) with optional context selection
//  3. Error if neither is configured
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	var opts []talosclient.OptionFunc

	if cfg.CA != "" && cfg.Cert != "" && cfg.Key != "" {
		tlsConfig, err := buildTLSConfig(cfg.CA, cfg.Cert, cfg.Key)
		if err != nil {
			return nil, err
		}

		opts = append(opts, talosclient.WithTLSConfig(tlsConfig))

		if cfg.Endpoint != "" {
			opts = append(opts, talosclient.WithEndpoints(cfg.Endpoint))
		}
	} else if cfg.TalosConfig != "" {
		opts = append(opts, talosclient.WithConfigFromFile(cfg.TalosConfig))

		if cfg.TalosContext != "" {
			opts = append(opts, talosclient.WithContextName(cfg.TalosContext))
		}

		if cfg.Endpoint != "" {
			opts = append(opts, talosclient.WithEndpoints(cfg.Endpoint))
		}
	} else {
		return nil, fmt.Errorf("no authentication configured")
	}

	c, err := talosclient.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating Talos client: %w", err)
	}

	return &Client{
		inner: c,
		node:  cfg.Node,
	}, nil
}

// Close releases the client's gRPC connection.
func (c *Client) Close() error {
	return c.inner.Close()
}

// nodeCtx returns a context with the target node metadata set, if configured.
// When a node is set, the Talos apid proxy routes the request to that node.
func (c *Client) nodeCtx(ctx context.Context) context.Context {
	if c.node != "" {
		return talosclient.WithNode(ctx, c.node)
	}

	return ctx
}

// SystemStat returns CPU counters and process statistics.
func (c *Client) SystemStat(ctx context.Context) (*machine.SystemStatResponse, error) {
	return c.inner.MachineClient.SystemStat(c.nodeCtx(ctx), &emptypb.Empty{})
}

// Memory returns /proc/meminfo-equivalent memory statistics.
func (c *Client) Memory(ctx context.Context) (*machine.MemoryResponse, error) {
	return c.inner.Memory(c.nodeCtx(ctx))
}

// Mounts returns mount point capacity and availability.
func (c *Client) Mounts(ctx context.Context) (*machine.MountsResponse, error) {
	return c.inner.Mounts(c.nodeCtx(ctx))
}

// ServiceList returns the list of Talos system services with state and health.
func (c *Client) ServiceList(ctx context.Context) (*machine.ServiceListResponse, error) {
	return c.inner.ServiceList(c.nodeCtx(ctx))
}

// EtcdStatus returns etcd member status including leader, DB size, and errors.
func (c *Client) EtcdStatus(ctx context.Context) (*machine.EtcdStatusResponse, error) {
	return c.inner.EtcdStatus(c.nodeCtx(ctx))
}

// EtcdMemberList returns the list of etcd cluster members.
func (c *Client) EtcdMemberList(ctx context.Context) (*machine.EtcdMemberListResponse, error) {
	return c.inner.EtcdMemberList(c.nodeCtx(ctx), &machine.EtcdMemberListRequest{})
}

// EtcdAlarmList returns active etcd alarms (NOSPACE, CORRUPT, etc.).
func (c *Client) EtcdAlarmList(ctx context.Context) (*machine.EtcdAlarmListResponse, error) {
	return c.inner.EtcdAlarmList(c.nodeCtx(ctx))
}

// LoadAvg returns 1/5/15-minute load averages.
func (c *Client) LoadAvg(ctx context.Context) (*machine.LoadAvgResponse, error) {
	return c.inner.MachineClient.LoadAvg(c.nodeCtx(ctx), &emptypb.Empty{})
}

// buildTLSConfig creates a mutual TLS configuration from certificate file paths
// or base64-encoded PEM data.
func buildTLSConfig(caPath, certPath, keyPath string) (*tls.Config, error) {
	caCert, err := loadPEMData(caPath, "CA certificate")
	if err != nil {
		return nil, err
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	certPEM, err := loadPEMData(certPath, "client certificate")
	if err != nil {
		return nil, err
	}

	keyPEM, err := loadPEMData(keyPath, "client key")
	if err != nil {
		return nil, err
	}

	clientCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("loading client certificate/key: %w", err)
	}

	return &tls.Config{
		RootCAs:      caCertPool,
		Certificates: []tls.Certificate{clientCert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// loadPEMData attempts to load PEM data from either a file path or base64-encoded string.
// It first checks if the input is a valid file path. If the file exists, it reads and
// returns the file content. If the file does not exist, it attempts to decode the input
// as base64 and validates that the result is valid PEM data.
func loadPEMData(input, description string) ([]byte, error) {
	// Try to read as file first
	if _, err := os.Stat(input); err == nil {
		data, err := os.ReadFile(input)
		if err != nil {
			return nil, fmt.Errorf("reading %s from file: %w", description, err)
		}
		return data, nil
	}

	// Not a file, try base64 decoding
	decoded, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: not a valid file path or base64-encoded data", description)
	}

	// Validate that decoded data contains at least one PEM block
	if !containsValidPEM(decoded) {
		return nil, fmt.Errorf("invalid %s: decoded data is not valid PEM format", description)
	}

	return decoded, nil
}

// containsValidPEM checks if the data contains at least one valid PEM block.
func containsValidPEM(data []byte) bool {
	block, _ := pem.Decode(data)
	return block != nil
}
