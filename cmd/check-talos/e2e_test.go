//go:build e2e

package main_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"context"

	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ---------------------------------------------------------------------------
// Global test state (set up in TestMain)
// ---------------------------------------------------------------------------

var (
	binaryPath string   // path to compiled check-talos binary
	serverAddr string   // mock gRPC server address (127.0.0.1:<port>)
	caPath     string   // test CA certificate path
	certPath   string   // test client certificate path
	keyPath    string   // test client key path
	mock       *mockSrv // shared mock gRPC server
)

// ---------------------------------------------------------------------------
// Mock gRPC server implementing MachineServiceServer
// ---------------------------------------------------------------------------

type mockSrv struct {
	machine.UnimplementedMachineServiceServer
	mu sync.Mutex

	systemStatResp  *machine.SystemStatResponse
	systemStatErr   error
	memoryResp      *machine.MemoryResponse
	memoryErr       error
	mountsResp      *machine.MountsResponse
	mountsErr       error
	serviceListResp *machine.ServiceListResponse
	serviceListErr  error
	etcdStatusResp  *machine.EtcdStatusResponse
	etcdStatusErr   error
	etcdMemberResp  *machine.EtcdMemberListResponse
	etcdMemberErr   error
	etcdAlarmResp   *machine.EtcdAlarmListResponse
	etcdAlarmErr    error
	loadAvgResp     *machine.LoadAvgResponse
	loadAvgErr      error
}

func (s *mockSrv) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systemStatResp = nil
	s.systemStatErr = nil
	s.memoryResp = nil
	s.memoryErr = nil
	s.mountsResp = nil
	s.mountsErr = nil
	s.serviceListResp = nil
	s.serviceListErr = nil
	s.etcdStatusResp = nil
	s.etcdStatusErr = nil
	s.etcdMemberResp = nil
	s.etcdMemberErr = nil
	s.etcdAlarmResp = nil
	s.etcdAlarmErr = nil
	s.loadAvgResp = nil
	s.loadAvgErr = nil
}

func (s *mockSrv) SystemStat(_ context.Context, _ *emptypb.Empty) (*machine.SystemStatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.systemStatResp, s.systemStatErr
}

func (s *mockSrv) Memory(_ context.Context, _ *emptypb.Empty) (*machine.MemoryResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryResp, s.memoryErr
}

func (s *mockSrv) Mounts(_ context.Context, _ *emptypb.Empty) (*machine.MountsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mountsResp, s.mountsErr
}

func (s *mockSrv) ServiceList(_ context.Context, _ *emptypb.Empty) (*machine.ServiceListResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serviceListResp, s.serviceListErr
}

func (s *mockSrv) EtcdStatus(_ context.Context, _ *emptypb.Empty) (*machine.EtcdStatusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.etcdStatusResp, s.etcdStatusErr
}

func (s *mockSrv) EtcdMemberList(_ context.Context, _ *machine.EtcdMemberListRequest) (*machine.EtcdMemberListResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.etcdMemberResp, s.etcdMemberErr
}

func (s *mockSrv) EtcdAlarmList(_ context.Context, _ *emptypb.Empty) (*machine.EtcdAlarmListResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.etcdAlarmResp, s.etcdAlarmErr
}

func (s *mockSrv) LoadAvg(_ context.Context, _ *emptypb.Empty) (*machine.LoadAvgResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadAvgResp, s.loadAvgErr
}

// ---------------------------------------------------------------------------
// TestMain — build binary, generate certs, start mock gRPC server
// ---------------------------------------------------------------------------

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "check-talos-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	// Build the binary.
	binaryPath = filepath.Join(tmpDir, "check-talos")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build binary: %v\n", err)
		os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	// Generate test certificates for mTLS.
	certDir := filepath.Join(tmpDir, "certs")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create cert dir: %v\n", err)
		os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	serverTLS, err := generateTestCerts(certDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate test certs: %v\n", err)
		os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	caPath = filepath.Join(certDir, "ca.crt")
	certPath = filepath.Join(certDir, "client.crt")
	keyPath = filepath.Join(certDir, "client.key")

	// Start mock gRPC server.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to listen: %v\n", err)
		os.RemoveAll(tmpDir)
		os.Exit(1)
	}
	serverAddr = lis.Addr().String()

	mock = &mockSrv{}
	creds := credentials.NewTLS(serverTLS)
	grpcServer := grpc.NewServer(grpc.Creds(creds))
	machine.RegisterMachineServiceServer(grpcServer, mock)
	go grpcServer.Serve(lis) //nolint:errcheck

	code := m.Run()

	grpcServer.Stop()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// TLS certificate generation
// ---------------------------------------------------------------------------

func generateTestCerts(dir string) (*tls.Config, error) {
	// CA key pair.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating CA key: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating CA cert: %w", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, fmt.Errorf("parsing CA cert: %w", err)
	}

	if err := writePEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", caCertDER); err != nil {
		return nil, err
	}

	// Server key pair.
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating server key: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:     []string{"localhost"},
	}

	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating server cert: %w", err)
	}

	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling server key: %w", err)
	}

	// Client key pair.
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating client key: %w", err)
	}

	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	clientCertDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating client cert: %w", err)
	}

	if err := writePEM(filepath.Join(dir, "client.crt"), "CERTIFICATE", clientCertDER); err != nil {
		return nil, err
	}

	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling client key: %w", err)
	}

	if err := writePEM(filepath.Join(dir, "client.key"), "EC PRIVATE KEY", clientKeyDER); err != nil {
		return nil, err
	}

	// Build server TLS config for the mock gRPC server.
	serverTLSCert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER}),
	)
	if err != nil {
		return nil, fmt.Errorf("loading server key pair: %w", err)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AddCert(caCert)

	return &tls.Config{
		Certificates: []tls.Certificate{serverTLSCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caCertPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func writePEM(path, pemType string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: pemType, Bytes: data})
}

// ---------------------------------------------------------------------------
// Helper: run binary and capture output + exit code
// ---------------------------------------------------------------------------

type runResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func run(t *testing.T, args ...string) runResult {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := runResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run binary: %v", err)
		}
	}

	return res
}

// authArgs returns the global authentication flags pointing to the mock server.
func authArgs() []string {
	return []string{
		"-e", serverAddr,
		"--talos-ca", caPath,
		"--talos-cert", certPath,
		"--talos-key", keyPath,
	}
}

// assertResult checks exit code and that stdout contains all expected substrings.
func assertResult(t *testing.T, res runResult, wantExit int, wantContains ...string) {
	t.Helper()
	if res.exitCode != wantExit {
		t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s",
			res.exitCode, wantExit, res.stdout, res.stderr)
	}
	for _, s := range wantContains {
		if !strings.Contains(res.stdout, s) {
			t.Errorf("stdout does not contain %q\nstdout: %s", s, res.stdout)
		}
	}
}

// assertNotContains checks that stdout does NOT contain any of the given substrings.
func assertNotContains(t *testing.T, res runResult, notWant ...string) {
	t.Helper()
	for _, s := range notWant {
		if strings.Contains(res.stdout, s) {
			t.Errorf("stdout should not contain %q\nstdout: %s", s, res.stdout)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: --help
// ---------------------------------------------------------------------------

func TestE2E_Help(t *testing.T) {
	res := run(t, "--help")
	assertResult(t, res, 3)
	// go-arg help should include the description.
	if !strings.Contains(res.stdout, "Nagios-compatible monitoring plugin") {
		t.Errorf("help output missing description\nstdout: %s", res.stdout)
	}
}

// ---------------------------------------------------------------------------
// Test: No subcommand
// ---------------------------------------------------------------------------

func TestE2E_NoSubcommand(t *testing.T) {
	res := run(t, append(authArgs())...)
	assertResult(t, res, 3, "TALOS UNKNOWN", "No check specified")
}

// ---------------------------------------------------------------------------
// Test: Validation errors (V1-V12)
// ---------------------------------------------------------------------------

func TestE2E_Validation(t *testing.T) {
	t.Run("V2 - no auth configured", func(t *testing.T) {
		res := run(t, "-e", "127.0.0.1:50000", "cpu")
		assertResult(t, res, 3, "TALOS CPU UNKNOWN", "No authentication configured")
	})

	t.Run("V3 - incomplete cert auth missing key", func(t *testing.T) {
		res := run(t, "-e", "127.0.0.1:50000",
			"--talos-ca", caPath,
			"--talos-cert", certPath,
			"cpu")
		assertResult(t, res, 3, "TALOS CPU UNKNOWN", "Incomplete cert auth", "--talos-key")
	})

	t.Run("V3 - incomplete cert auth missing cert and key", func(t *testing.T) {
		res := run(t, "-e", "127.0.0.1:50000",
			"--talos-ca", caPath,
			"cpu")
		assertResult(t, res, 3, "TALOS CPU UNKNOWN", "Incomplete cert auth", "--talos-cert", "--talos-key")
	})

	t.Run("V4 - cert file not found", func(t *testing.T) {
		res := run(t, "-e", "127.0.0.1:50000",
			"--talos-ca", "/nonexistent/ca.crt",
			"--talos-cert", certPath,
			"--talos-key", keyPath,
			"cpu")
		assertResult(t, res, 3, "TALOS CPU UNKNOWN", "Cannot read --talos-ca")
	})

	t.Run("V4 - talosconfig file not found", func(t *testing.T) {
		res := run(t, "--talosconfig", "/nonexistent/talosconfig", "cpu")
		assertResult(t, res, 3, "TALOS CPU UNKNOWN", "Cannot read --talosconfig")
	})

	t.Run("V5 - no endpoint with cert auth", func(t *testing.T) {
		res := run(t,
			"--talos-ca", caPath,
			"--talos-cert", certPath,
			"--talos-key", keyPath,
			"cpu")
		assertResult(t, res, 3, "TALOS CPU UNKNOWN", "No endpoint configured")
	})

	t.Run("V6 - invalid timeout zero", func(t *testing.T) {
		args := append(authArgs(), "-t", "0s", "cpu")
		res := run(t, args...)
		assertResult(t, res, 3, "TALOS CPU UNKNOWN", "Invalid timeout")
	})

	t.Run("V6 - invalid timeout too large", func(t *testing.T) {
		args := append(authArgs(), "-t", "121s", "cpu")
		res := run(t, args...)
		assertResult(t, res, 3, "TALOS CPU UNKNOWN", "Invalid timeout")
	})

	t.Run("V7 - invalid warning threshold for cpu", func(t *testing.T) {
		args := append(authArgs(), "cpu", "-w", "abc")
		res := run(t, args...)
		assertResult(t, res, 3, "TALOS CPU UNKNOWN", "Invalid warning threshold")
	})

	t.Run("V7 - invalid critical threshold for memory", func(t *testing.T) {
		args := append(authArgs(), "memory", "-c", "xyz")
		res := run(t, args...)
		assertResult(t, res, 3, "TALOS MEMORY UNKNOWN", "Invalid critical threshold")
	})

	t.Run("V9 - services include and exclude", func(t *testing.T) {
		args := append(authArgs(), "services", "--include", "apid", "--exclude", "etcd")
		res := run(t, args...)
		assertResult(t, res, 3, "TALOS SERVICES UNKNOWN", "Cannot use both --include and --exclude")
	})

	t.Run("V10 - invalid period", func(t *testing.T) {
		args := append(authArgs(), "load", "--period", "10")
		res := run(t, args...)
		assertResult(t, res, 3, "TALOS LOAD UNKNOWN", "Invalid --period")
	})

	t.Run("V12 - invalid mount not absolute", func(t *testing.T) {
		args := append(authArgs(), "disk", "-m", "var")
		res := run(t, args...)
		assertResult(t, res, 3, "TALOS DISK UNKNOWN", "Invalid --mount", "must be an absolute path")
	})
}

// ---------------------------------------------------------------------------
// Test: Connection error (unreachable endpoint)
// ---------------------------------------------------------------------------

func TestE2E_ConnectionError(t *testing.T) {
	// Get a port that nothing is listening on.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get unused port: %v", err)
	}
	deadAddr := l.Addr().String()
	l.Close()

	res := run(t,
		"-e", deadAddr,
		"--talos-ca", caPath,
		"--talos-cert", certPath,
		"--talos-key", keyPath,
		"-t", "2s",
		"cpu")
	// Connection failure should produce CRITICAL (exit 2).
	assertResult(t, res, 2, "TALOS CPU CRITICAL")
}

// ---------------------------------------------------------------------------
// Test: CPU check via mock gRPC server
// ---------------------------------------------------------------------------

func TestE2E_CPU(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.systemStatResp = &machine.SystemStatResponse{
			Messages: []*machine.SystemStat{{
				CpuTotal: &machine.CPUStat{
					User: 342, Nice: 0, System: 0,
					Idle: 608, Iowait: 50, Irq: 0, SoftIrq: 0, Steal: 0,
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "cpu", "-w", "80", "-c", "90")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS CPU OK", "CPU usage 34.2%", "'cpu_usage'=34.2;80;90;0;100")
	})

	t.Run("WARNING", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.systemStatResp = &machine.SystemStatResponse{
			Messages: []*machine.SystemStat{{
				CpuTotal: &machine.CPUStat{
					User: 825, Idle: 150, Iowait: 25,
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "cpu", "-w", "80", "-c", "90")
		res := run(t, args...)
		assertResult(t, res, 1, "TALOS CPU WARNING", "CPU usage 82.5%", "'cpu_usage'=82.5;80;90;0;100")
	})

	t.Run("CRITICAL", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.systemStatResp = &machine.SystemStatResponse{
			Messages: []*machine.SystemStat{{
				CpuTotal: &machine.CPUStat{
					User: 963, Idle: 30, Iowait: 7,
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "cpu", "-w", "80", "-c", "90")
		res := run(t, args...)
		assertResult(t, res, 2, "TALOS CPU CRITICAL", "CPU usage 96.3%", "'cpu_usage'=96.3;80;90;0;100")
	})

	t.Run("custom thresholds", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.systemStatResp = &machine.SystemStatResponse{
			Messages: []*machine.SystemStat{{
				CpuTotal: &machine.CPUStat{
					User: 700, Idle: 250, Iowait: 50,
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "cpu", "-w", "60", "-c", "75")
		res := run(t, args...)
		assertResult(t, res, 1, "TALOS CPU WARNING", "CPU usage 70.0%", "'cpu_usage'=70;60;75;0;100")
	})
}

// ---------------------------------------------------------------------------
// Test: Memory check via mock gRPC server
// ---------------------------------------------------------------------------

func TestE2E_Memory(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.memoryResp = &machine.MemoryResponse{
			Messages: []*machine.Memory{{
				Meminfo: &machine.MemInfo{
					Memtotal:     8388608,
					Memavailable: 3145728, // kB; ~62.5% used
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "memory")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS MEMORY OK", "Memory usage", "'memory_usage'=")
	})

	t.Run("WARNING", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.memoryResp = &machine.MemoryResponse{
			Messages: []*machine.Memory{{
				Meminfo: &machine.MemInfo{
					Memtotal:     8388608,
					Memavailable: 1363149, // kB; ~83.8% used
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "memory")
		res := run(t, args...)
		assertResult(t, res, 1, "TALOS MEMORY WARNING", "Memory usage 83.8%")
	})

	t.Run("CRITICAL", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.memoryResp = &machine.MemoryResponse{
			Messages: []*machine.Memory{{
				Meminfo: &machine.MemInfo{
					Memtotal:     8388608,
					Memavailable: 494188, // kB; ~94.1% used
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "memory")
		res := run(t, args...)
		assertResult(t, res, 2, "TALOS MEMORY CRITICAL", "Memory usage 94.1%", "'memory_usage'=94.1;80;90;0;100")
	})
}

// ---------------------------------------------------------------------------
// Test: Disk check via mock gRPC server
// ---------------------------------------------------------------------------

func TestE2E_Disk(t *testing.T) {
	t.Run("OK - var mount default", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.mountsResp = &machine.MountsResponse{
			Messages: []*machine.Mounts{{
				Stats: []*machine.MountStat{
					{Filesystem: "/dev/sda5", MountedOn: "/var", Size: 21474836480, Available: 11811160064},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "disk")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS DISK OK", "/var usage 45.0%", "'disk_usage'=45;80;90;0;100")
	})

	t.Run("WARNING - var mount", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.mountsResp = &machine.MountsResponse{
			Messages: []*machine.Mounts{{
				Stats: []*machine.MountStat{
					{Filesystem: "/dev/sda1", MountedOn: "/", Size: 21474836480, Available: 11811160064},
					{Filesystem: "/dev/sda2", MountedOn: "/var", Size: 53687091200, Available: 8482714010},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "disk", "-m", "/var")
		res := run(t, args...)
		assertResult(t, res, 1, "TALOS DISK WARNING", "/var usage 84.2%", "'disk_usage'=84.2;80;90;0;100")
	})

	t.Run("CRITICAL", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.mountsResp = &machine.MountsResponse{
			Messages: []*machine.Mounts{{
				Stats: []*machine.MountStat{
					{Filesystem: "/dev/sda5", MountedOn: "/var", Size: 21474836480, Available: 1330642170},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "disk")
		res := run(t, args...)
		assertResult(t, res, 2, "TALOS DISK CRITICAL", "/var usage 93.8%")
	})

	t.Run("UNKNOWN - mount not found", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.mountsResp = &machine.MountsResponse{
			Messages: []*machine.Mounts{{
				Stats: []*machine.MountStat{
					{Filesystem: "/dev/sda1", MountedOn: "/", Size: 21474836480, Available: 11811160064},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "disk", "-m", "/data")
		res := run(t, args...)
		assertResult(t, res, 3, "TALOS DISK UNKNOWN", "Mount point /data not found")
	})
}

// ---------------------------------------------------------------------------
// Test: Services check via mock gRPC server
// ---------------------------------------------------------------------------

func TestE2E_Services(t *testing.T) {
	t.Run("OK - all healthy", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.serviceListResp = &machine.ServiceListResponse{
			Messages: []*machine.ServiceList{{
				Services: []*machine.ServiceInfo{
					{Id: "apid", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
					{Id: "containerd", State: "Running", Health: &machine.ServiceHealth{Unknown: true}},
					{Id: "kubelet", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
					{Id: "etcd", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "services")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS SERVICES OK", "4/4 services healthy",
			"'services_total'=4", "'services_healthy'=4", "'services_unhealthy'=0")
	})

	t.Run("CRITICAL - one unhealthy", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.serviceListResp = &machine.ServiceListResponse{
			Messages: []*machine.ServiceList{{
				Services: []*machine.ServiceInfo{
					{Id: "apid", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
					{Id: "containerd", State: "Running", Health: &machine.ServiceHealth{Unknown: true}},
					{Id: "kubelet", State: "Finished", Health: &machine.ServiceHealth{Healthy: false, LastMessage: "readiness probe failed"}},
					{Id: "etcd", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "services")
		res := run(t, args...)
		assertResult(t, res, 2, "TALOS SERVICES CRITICAL", "1/4 services unhealthy", "kubelet",
			"'services_unhealthy'=1")
		// Verify long text details.
		assertResult(t, res, 2, "kubelet: state=Finished")
	})

	t.Run("OK - excluded service down", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.serviceListResp = &machine.ServiceListResponse{
			Messages: []*machine.ServiceList{{
				Services: []*machine.ServiceInfo{
					{Id: "apid", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
					{Id: "kubelet", State: "Finished", Health: &machine.ServiceHealth{Healthy: false}},
					{Id: "etcd", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "services", "--exclude", "kubelet")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS SERVICES OK", "2/2 services healthy")
	})

	t.Run("CRITICAL - include filter catches unhealthy", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.serviceListResp = &machine.ServiceListResponse{
			Messages: []*machine.ServiceList{{
				Services: []*machine.ServiceInfo{
					{Id: "apid", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
					{Id: "kubelet", State: "Finished", Health: &machine.ServiceHealth{Healthy: false, LastMessage: "crash loop"}},
					{Id: "etcd", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "services", "--include", "kubelet")
		res := run(t, args...)
		assertResult(t, res, 2, "TALOS SERVICES CRITICAL", "1/1 services unhealthy", "kubelet")
	})
}

// ---------------------------------------------------------------------------
// Test: Etcd check via mock gRPC server
// ---------------------------------------------------------------------------

func TestE2E_Etcd(t *testing.T) {
	t.Run("OK - healthy cluster", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.etcdStatusResp = &machine.EtcdStatusResponse{
			Messages: []*machine.EtcdStatus{{
				MemberStatus: &machine.EtcdMemberStatus{
					MemberId: 1234, Leader: 1234,
					DbSize: 13107200, DbSizeInUse: 8388608,
				},
			}},
		}
		mock.etcdMemberResp = &machine.EtcdMemberListResponse{
			Messages: []*machine.EtcdMembers{{
				Members: []*machine.EtcdMember{
					{Id: 1, Hostname: "cp-1"},
					{Id: 2, Hostname: "cp-2"},
					{Id: 3, Hostname: "cp-3"},
				},
			}},
		}
		mock.etcdAlarmResp = &machine.EtcdAlarmListResponse{
			Messages: []*machine.EtcdAlarm{{MemberAlarms: nil}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "etcd")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS ETCD OK", "Leader", "3/3 members", "DB 12.50 MB",
			"'etcd_dbsize'=13107200B;~:100000000;~:200000000;0;",
			"'etcd_dbsize_in_use'=8388608B",
			"'etcd_members'=3")
	})

	t.Run("WARNING - DB size exceeds warning", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.etcdStatusResp = &machine.EtcdStatusResponse{
			Messages: []*machine.EtcdStatus{{
				MemberStatus: &machine.EtcdMemberStatus{
					MemberId: 1234, Leader: 1234,
					DbSize: 117878784, DbSizeInUse: 96468992,
				},
			}},
		}
		mock.etcdMemberResp = &machine.EtcdMemberListResponse{
			Messages: []*machine.EtcdMembers{{
				Members: []*machine.EtcdMember{
					{Id: 1, Hostname: "cp-1"},
					{Id: 2, Hostname: "cp-2"},
					{Id: 3, Hostname: "cp-3"},
				},
			}},
		}
		mock.etcdAlarmResp = &machine.EtcdAlarmListResponse{
			Messages: []*machine.EtcdAlarm{{MemberAlarms: nil}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "etcd")
		res := run(t, args...)
		assertResult(t, res, 1, "TALOS ETCD WARNING", "Leader", "3/3 members")
	})

	t.Run("CRITICAL - no leader", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.etcdStatusResp = &machine.EtcdStatusResponse{
			Messages: []*machine.EtcdStatus{{
				MemberStatus: &machine.EtcdMemberStatus{
					MemberId: 1234, Leader: 0,
					DbSize: 45000000, DbSizeInUse: 40000000,
				},
			}},
		}
		mock.etcdMemberResp = &machine.EtcdMemberListResponse{
			Messages: []*machine.EtcdMembers{{
				Members: []*machine.EtcdMember{
					{Id: 1, Hostname: "cp-1"},
					{Id: 2, Hostname: "cp-2"},
					{Id: 3, Hostname: "cp-3"},
				},
			}},
		}
		mock.etcdAlarmResp = &machine.EtcdAlarmListResponse{
			Messages: []*machine.EtcdAlarm{{MemberAlarms: nil}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "etcd")
		res := run(t, args...)
		assertResult(t, res, 2, "TALOS ETCD CRITICAL", "No leader elected",
			"'etcd_dbsize'=45000000B")
	})

	t.Run("CRITICAL - member count below minimum", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.etcdStatusResp = &machine.EtcdStatusResponse{
			Messages: []*machine.EtcdStatus{{
				MemberStatus: &machine.EtcdMemberStatus{
					MemberId: 1234, Leader: 1234,
					DbSize: 13107200, DbSizeInUse: 8388608,
				},
			}},
		}
		mock.etcdMemberResp = &machine.EtcdMemberListResponse{
			Messages: []*machine.EtcdMembers{{
				Members: []*machine.EtcdMember{
					{Id: 1, Hostname: "cp-1"},
					{Id: 2, Hostname: "cp-2"},
				},
			}},
		}
		mock.etcdAlarmResp = &machine.EtcdAlarmListResponse{
			Messages: []*machine.EtcdAlarm{{MemberAlarms: nil}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "etcd")
		res := run(t, args...)
		assertResult(t, res, 2, "TALOS ETCD CRITICAL", "Member count 2 below minimum 3")
	})

	t.Run("CRITICAL - NOSPACE alarm", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.etcdStatusResp = &machine.EtcdStatusResponse{
			Messages: []*machine.EtcdStatus{{
				MemberStatus: &machine.EtcdMemberStatus{
					MemberId: 1234, Leader: 1234,
					DbSize: 2147483648, DbSizeInUse: 2000000000,
				},
			}},
		}
		mock.etcdMemberResp = &machine.EtcdMemberListResponse{
			Messages: []*machine.EtcdMembers{{
				Members: []*machine.EtcdMember{
					{Id: 1, Hostname: "cp-1"},
					{Id: 2, Hostname: "cp-2"},
					{Id: 3, Hostname: "cp-3"},
				},
			}},
		}
		mock.etcdAlarmResp = &machine.EtcdAlarmListResponse{
			Messages: []*machine.EtcdAlarm{{
				MemberAlarms: []*machine.EtcdMemberAlarm{
					{MemberId: 1234, Alarm: machine.EtcdMemberAlarm_NOSPACE},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "etcd")
		res := run(t, args...)
		assertResult(t, res, 2, "TALOS ETCD CRITICAL", "Active alarm: NOSPACE")
	})

	t.Run("custom min-members", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.etcdStatusResp = &machine.EtcdStatusResponse{
			Messages: []*machine.EtcdStatus{{
				MemberStatus: &machine.EtcdMemberStatus{
					MemberId: 1234, Leader: 1234,
					DbSize: 13107200, DbSizeInUse: 8388608,
				},
			}},
		}
		mock.etcdMemberResp = &machine.EtcdMemberListResponse{
			Messages: []*machine.EtcdMembers{{
				Members: []*machine.EtcdMember{
					{Id: 1, Hostname: "cp-1"},
					{Id: 2, Hostname: "cp-2"},
					{Id: 3, Hostname: "cp-3"},
					{Id: 4, Hostname: "cp-4"},
					{Id: 5, Hostname: "cp-5"},
				},
			}},
		}
		mock.etcdAlarmResp = &machine.EtcdAlarmListResponse{
			Messages: []*machine.EtcdAlarm{{MemberAlarms: nil}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "etcd", "--min-members", "5")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS ETCD OK", "5/5 members")
	})
}

// ---------------------------------------------------------------------------
// Test: Load check via mock gRPC server
// ---------------------------------------------------------------------------

func TestE2E_Load(t *testing.T) {
	t.Run("OK - explicit thresholds", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.loadAvgResp = &machine.LoadAvgResponse{
			Messages: []*machine.LoadAvg{{
				Load1: 0.98, Load5: 1.23, Load15: 1.45,
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "load", "-w", "4", "-c", "8")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS LOAD OK", "Load average (5m) 1.23",
			"'load5'=1.23;4;8;0;")
	})

	t.Run("WARNING - explicit thresholds", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.loadAvgResp = &machine.LoadAvgResponse{
			Messages: []*machine.LoadAvg{{
				Load1: 5.12, Load5: 4.56, Load15: 3.21,
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "load", "-w", "4", "-c", "8")
		res := run(t, args...)
		assertResult(t, res, 1, "TALOS LOAD WARNING", "Load average (5m) 4.56",
			"'load5'=4.56;4;8;0;")
	})

	t.Run("CRITICAL - explicit thresholds", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.loadAvgResp = &machine.LoadAvgResponse{
			Messages: []*machine.LoadAvg{{
				Load1: 11.02, Load5: 9.87, Load15: 7.65,
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "load", "-w", "4", "-c", "8")
		res := run(t, args...)
		assertResult(t, res, 2, "TALOS LOAD CRITICAL", "Load average (5m) 9.87",
			"'load5'=9.87;4;8;0;")
	})

	t.Run("OK - auto-computed thresholds (4 CPUs)", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.loadAvgResp = &machine.LoadAvgResponse{
			Messages: []*machine.LoadAvg{{
				Load1: 0.98, Load5: 1.23, Load15: 1.45,
			}},
		}
		mock.systemStatResp = &machine.SystemStatResponse{
			Messages: []*machine.SystemStat{{
				CpuTotal: &machine.CPUStat{User: 1000, Idle: 9000},
				Cpu: []*machine.CPUStat{
					{User: 250, Idle: 2250},
					{User: 250, Idle: 2250},
					{User: 250, Idle: 2250},
					{User: 250, Idle: 2250},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "load")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS LOAD OK", "Load average (5m) 1.23",
			"'load5'=1.23;4;8;0;")
	})

	t.Run("period 1 selects load1", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.loadAvgResp = &machine.LoadAvgResponse{
			Messages: []*machine.LoadAvg{{
				Load1: 2.10, Load5: 1.85, Load15: 1.45,
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "load", "-w", "4", "-c", "8", "--period", "1")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS LOAD OK", "Load average (1m) 2.10",
			"'load1'=2.1;4;8;0;")
	})

	t.Run("period 15 selects load15", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.loadAvgResp = &machine.LoadAvgResponse{
			Messages: []*machine.LoadAvg{{
				Load1: 5.12, Load5: 4.56, Load15: 3.21,
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "load", "-w", "4", "-c", "8", "--period", "15")
		res := run(t, args...)
		assertResult(t, res, 0, "TALOS LOAD OK", "Load average (15m) 3.21",
			"'load15'=3.21;4;8;0;")
	})
}

// ---------------------------------------------------------------------------
// Test: Perfdata always present for successful checks
// ---------------------------------------------------------------------------

func TestE2E_PerfDataPresent(t *testing.T) {
	t.Run("cpu has perfdata", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.systemStatResp = &machine.SystemStatResponse{
			Messages: []*machine.SystemStat{{
				CpuTotal: &machine.CPUStat{User: 500, Idle: 500},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "cpu")
		res := run(t, args...)
		assertResult(t, res, 0, "'cpu_usage'=")
	})

	t.Run("memory has 3 perfdata metrics", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.memoryResp = &machine.MemoryResponse{
			Messages: []*machine.Memory{{
				Meminfo: &machine.MemInfo{
					Memtotal: 8388608, Memavailable: 5000000, // kB
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "memory")
		res := run(t, args...)
		assertResult(t, res, 0, "'memory_usage'=", "'memory_used'=", "'memory_total'=")
	})

	t.Run("disk has 3 perfdata metrics", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.mountsResp = &machine.MountsResponse{
			Messages: []*machine.Mounts{{
				Stats: []*machine.MountStat{
					{MountedOn: "/var", Size: 21474836480, Available: 11000000000},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "disk")
		res := run(t, args...)
		assertResult(t, res, 0, "'disk_usage'=", "'disk_used'=", "'disk_total'=")
	})

	t.Run("services has 3 perfdata metrics", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.serviceListResp = &machine.ServiceListResponse{
			Messages: []*machine.ServiceList{{
				Services: []*machine.ServiceInfo{
					{Id: "apid", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
				},
			}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "services")
		res := run(t, args...)
		assertResult(t, res, 0, "'services_total'=", "'services_healthy'=", "'services_unhealthy'=")
	})

	t.Run("etcd has 3 perfdata metrics", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.etcdStatusResp = &machine.EtcdStatusResponse{
			Messages: []*machine.EtcdStatus{{
				MemberStatus: &machine.EtcdMemberStatus{
					MemberId: 1, Leader: 1, DbSize: 1000, DbSizeInUse: 500,
				},
			}},
		}
		mock.etcdMemberResp = &machine.EtcdMemberListResponse{
			Messages: []*machine.EtcdMembers{{
				Members: []*machine.EtcdMember{{Id: 1, Hostname: "cp-1"}, {Id: 2, Hostname: "cp-2"}, {Id: 3, Hostname: "cp-3"}},
			}},
		}
		mock.etcdAlarmResp = &machine.EtcdAlarmListResponse{
			Messages: []*machine.EtcdAlarm{{MemberAlarms: nil}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "etcd")
		res := run(t, args...)
		assertResult(t, res, 0, "'etcd_dbsize'=", "'etcd_dbsize_in_use'=", "'etcd_members'=")
	})

	t.Run("load has 3 perfdata metrics", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.loadAvgResp = &machine.LoadAvgResponse{
			Messages: []*machine.LoadAvg{{Load1: 1, Load5: 2, Load15: 3}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "load", "-w", "4", "-c", "8")
		res := run(t, args...)
		assertResult(t, res, 0, "'load1'=", "'load5'=", "'load15'=")
	})
}

// ---------------------------------------------------------------------------
// Test: Validation errors produce no perfdata (except go-nagios 'time')
// ---------------------------------------------------------------------------

func TestE2E_ValidationNoPerfData(t *testing.T) {
	res := run(t, "-e", "127.0.0.1:50000",
		"--talos-ca", caPath,
		"--talos-cert", certPath,
		"--talos-key", keyPath,
		"cpu", "-w", "abc")
	assertResult(t, res, 3, "TALOS CPU UNKNOWN", "Invalid warning threshold")
	// Should not contain check-specific perfdata.
	assertNotContains(t, res, "'cpu_usage'=")
}

// ---------------------------------------------------------------------------
// Test: Default thresholds are applied
// ---------------------------------------------------------------------------

func TestE2E_DefaultThresholds(t *testing.T) {
	t.Run("cpu defaults 80/90", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.systemStatResp = &machine.SystemStatResponse{
			Messages: []*machine.SystemStat{{
				CpuTotal: &machine.CPUStat{User: 500, Idle: 500},
			}},
		}
		mock.mu.Unlock()

		// No -w/-c flags -> defaults to 80/90.
		args := append(authArgs(), "cpu")
		res := run(t, args...)
		assertResult(t, res, 0, "'cpu_usage'=50;80;90;0;100")
	})

	t.Run("etcd defaults ~:100000000/~:200000000", func(t *testing.T) {
		mock.reset()
		mock.mu.Lock()
		mock.etcdStatusResp = &machine.EtcdStatusResponse{
			Messages: []*machine.EtcdStatus{{
				MemberStatus: &machine.EtcdMemberStatus{
					MemberId: 1, Leader: 1, DbSize: 1000, DbSizeInUse: 500,
				},
			}},
		}
		mock.etcdMemberResp = &machine.EtcdMemberListResponse{
			Messages: []*machine.EtcdMembers{{
				Members: []*machine.EtcdMember{{Id: 1}, {Id: 2}, {Id: 3}},
			}},
		}
		mock.etcdAlarmResp = &machine.EtcdAlarmListResponse{
			Messages: []*machine.EtcdAlarm{{MemberAlarms: nil}},
		}
		mock.mu.Unlock()

		args := append(authArgs(), "etcd")
		res := run(t, args...)
		assertResult(t, res, 0, "'etcd_dbsize'=1000B;~:100000000;~:200000000;0;")
	})
}

// ---------------------------------------------------------------------------
// Test: Output format matches Nagios convention
// ---------------------------------------------------------------------------

func TestE2E_OutputFormat(t *testing.T) {
	// Every output line must start with "TALOS <CHECK> <STATUS> - ".
	checks := []struct {
		name      string
		checkName string
		setup     func()
		args      []string
	}{
		{
			name:      "cpu",
			checkName: "CPU",
			setup: func() {
				mock.reset()
				mock.mu.Lock()
				mock.systemStatResp = &machine.SystemStatResponse{
					Messages: []*machine.SystemStat{{
						CpuTotal: &machine.CPUStat{User: 342, Idle: 608, Iowait: 50},
					}},
				}
				mock.mu.Unlock()
			},
			args: []string{"cpu"},
		},
		{
			name:      "memory",
			checkName: "MEMORY",
			setup: func() {
				mock.reset()
				mock.mu.Lock()
				mock.memoryResp = &machine.MemoryResponse{
					Messages: []*machine.Memory{{
						Meminfo: &machine.MemInfo{Memtotal: 8388608, Memavailable: 5000000}, // kB
					}},
				}
				mock.mu.Unlock()
			},
			args: []string{"memory"},
		},
		{
			name:      "disk",
			checkName: "DISK",
			setup: func() {
				mock.reset()
				mock.mu.Lock()
				mock.mountsResp = &machine.MountsResponse{
					Messages: []*machine.Mounts{{
						Stats: []*machine.MountStat{
							{MountedOn: "/var", Size: 21474836480, Available: 11000000000},
						},
					}},
				}
				mock.mu.Unlock()
			},
			args: []string{"disk"},
		},
		{
			name:      "services",
			checkName: "SERVICES",
			setup: func() {
				mock.reset()
				mock.mu.Lock()
				mock.serviceListResp = &machine.ServiceListResponse{
					Messages: []*machine.ServiceList{{
						Services: []*machine.ServiceInfo{
							{Id: "apid", State: "Running", Health: &machine.ServiceHealth{Healthy: true}},
						},
					}},
				}
				mock.mu.Unlock()
			},
			args: []string{"services"},
		},
		{
			name:      "etcd",
			checkName: "ETCD",
			setup: func() {
				mock.reset()
				mock.mu.Lock()
				mock.etcdStatusResp = &machine.EtcdStatusResponse{
					Messages: []*machine.EtcdStatus{{
						MemberStatus: &machine.EtcdMemberStatus{
							MemberId: 1, Leader: 1, DbSize: 1000, DbSizeInUse: 500,
						},
					}},
				}
				mock.etcdMemberResp = &machine.EtcdMemberListResponse{
					Messages: []*machine.EtcdMembers{{
						Members: []*machine.EtcdMember{{Id: 1}, {Id: 2}, {Id: 3}},
					}},
				}
				mock.etcdAlarmResp = &machine.EtcdAlarmListResponse{
					Messages: []*machine.EtcdAlarm{{MemberAlarms: nil}},
				}
				mock.mu.Unlock()
			},
			args: []string{"etcd"},
		},
		{
			name:      "load",
			checkName: "LOAD",
			setup: func() {
				mock.reset()
				mock.mu.Lock()
				mock.loadAvgResp = &machine.LoadAvgResponse{
					Messages: []*machine.LoadAvg{{Load1: 1, Load5: 2, Load15: 3}},
				}
				mock.mu.Unlock()
			},
			args: []string{"load", "-w", "4", "-c", "8"},
		},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			args := append(authArgs(), tc.args...)
			res := run(t, args...)

			// First line must start with "TALOS <CHECK> ".
			firstLine := strings.SplitN(res.stdout, "\n", 2)[0]
			prefix := fmt.Sprintf("TALOS %s ", tc.checkName)
			if !strings.HasPrefix(firstLine, prefix) {
				t.Errorf("output does not start with %q\ngot: %q", prefix, firstLine)
			}

			// Must contain pipe separator for perfdata.
			if !strings.Contains(firstLine, " | ") {
				t.Errorf("output missing perfdata pipe separator\ngot: %q", firstLine)
			}
		})
	}
}
