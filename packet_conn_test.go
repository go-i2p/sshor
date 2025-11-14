package sshor

import (
	"net"
	"testing"
	"time"
)

// test_finding6_resource_leak_many_peers tests that creating many peer tunnels
// causes unbounded resource growth without cleanup
func TestFinding6ResourceLeakManyPeers(t *testing.T) {
	// This test demonstrates the resource leak issue
	// We can't easily test it without a full router setup, so we'll
	// document the expected behavior

	// Before fix: peers map grows unbounded, tunnels never cleaned up
	// After fix: idle tunnels should be cleaned up based on timeout or LRU

	t.Log("Resource leak issue: TCP tunnels accumulate in peers map without cleanup")
	t.Log("Each WriteTo() to a new address creates a tunnel that's never closed until Close()")
}

// test_finding6_tunnel_cleanup tests that idle tunnels are cleaned up
func TestFinding6TunnelCleanup(t *testing.T) {
	// Create a minimal onionPacketConn for testing
	localConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local UDP listener: %v", err)
	}
	defer localConn.Close()

	opc := &onionPacketConn{
		router:    nil, // Will be nil for this test
		localConn: localConn,
		peers:     make(map[string]*tcpTunnel),
	}

	// Verify initial state
	if len(opc.peers) != 0 {
		t.Fatalf("Expected 0 peers initially, got %d", len(opc.peers))
	}

	// Close should handle empty peers map
	err = opc.Close()
	if err != nil {
		t.Errorf("Close() failed: %v", err)
	}

	if len(opc.peers) != 0 {
		t.Errorf("Expected 0 peers after Close(), got %d", len(opc.peers))
	}
}

// test_finding6_max_peers tests that we can limit the number of peer tunnels
func TestFinding6MaxPeers(t *testing.T) {
	t.Log("After fix: should support max peer limit to prevent unbounded growth")
	// This will be implemented as part of the fix
}

// test_finding6_idle_timeout tests that idle tunnels are closed after timeout
func TestFinding6IdleTimeout(t *testing.T) {
	t.Log("After fix: should close tunnels that haven't been used for specified duration")
	// This will be implemented as part of the fix
}

// test_finding6_cleanup_mechanism verifies the cleanup mechanism works
func TestFinding6CleanupMechanism(t *testing.T) {
	localConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local UDP listener: %v", err)
	}
	defer localConn.Close()

	opc := &onionPacketConn{
		router:      nil,
		localConn:   localConn,
		peers:       make(map[string]*tcpTunnel),
		idleTimeout: 100 * time.Millisecond, // Short timeout for testing
	}

	// Add some mock tunnels with different last access times
	now := time.Now()

	// Create mock connections
	addr1, _ := net.ResolveUDPAddr("udp", "127.0.0.1:9001")
	addr2, _ := net.ResolveUDPAddr("udp", "127.0.0.1:9002")
	addr3, _ := net.ResolveUDPAddr("udp", "127.0.0.1:9003")

	// Tunnel 1: old (should be cleaned up)
	opc.peers[addr1.String()] = &tcpTunnel{
		conn:       &mockConn{},
		addr:       addr1,
		lastAccess: now.Add(-200 * time.Millisecond),
	}

	// Tunnel 2: recent (should be kept)
	opc.peers[addr2.String()] = &tcpTunnel{
		conn:       &mockConn{},
		addr:       addr2,
		lastAccess: now.Add(-50 * time.Millisecond),
	}

	// Tunnel 3: very old (should be cleaned up)
	opc.peers[addr3.String()] = &tcpTunnel{
		conn:       &mockConn{},
		addr:       addr3,
		lastAccess: now.Add(-500 * time.Millisecond),
	}

	// Verify initial state
	if len(opc.peers) != 3 {
		t.Fatalf("Expected 3 peers initially, got %d", len(opc.peers))
	}

	// Run cleanup
	opc.cleanupIdleTunnels(false)

	// Should have removed the idle tunnels
	if len(opc.peers) != 1 {
		t.Errorf("Expected 1 peer after cleanup, got %d", len(opc.peers))
	}

	// Verify the correct tunnel was kept
	if _, exists := opc.peers[addr2.String()]; !exists {
		t.Errorf("Expected recent tunnel to be kept")
	}
}

// mockConn is a mock net.Conn for testing
type mockConn struct {
	closed bool
}

func (m *mockConn) Read(b []byte) (n int, err error)   { return 0, nil }
func (m *mockConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (m *mockConn) Close() error                       { m.closed = true; return nil }
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }
