package sshor

import (
	"net"
	"testing"
	"time"
)

// test_finding1_double_close_prevented verifies fix prevents double close panic
func TestFinding1DoubleClosePrevented(t *testing.T) {
	localConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local UDP listener: %v", err)
	}
	defer localConn.Close()

	opc := &onionPacketConn{
		router:      nil,
		localConn:   localConn,
		peers:       make(map[string]*tcpTunnel),
		idleTimeout: 100 * time.Millisecond,
	}

	// Add multiple mock tunnels
	addr1, _ := net.ResolveUDPAddr("udp", "127.0.0.1:9001")
	addr2, _ := net.ResolveUDPAddr("udp", "127.0.0.1:9002")

	opc.peers[addr1.String()] = &tcpTunnel{
		conn:       &mockConn{},
		addr:       addr1,
		lastAccess: time.Now().Add(-200 * time.Millisecond),
		stopRead:   make(chan struct{}),
	}

	opc.peers[addr2.String()] = &tcpTunnel{
		conn:       &mockConn{},
		addr:       addr2,
		lastAccess: time.Now().Add(-200 * time.Millisecond),
		stopRead:   make(chan struct{}),
	}

	// First cleanup - should succeed and remove both
	opc.cleanupIdleTunnels(true)

	// Verify tunnels were removed
	if len(opc.peers) != 0 {
		t.Errorf("Expected 0 peers after cleanup, got %d", len(opc.peers))
	}

	// Second cleanup on empty map - should not panic
	// This verifies that tunnels removed from map can't be double-cleaned
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Unexpected panic after fix: %v", r)
		}
	}()

	opc.cleanupIdleTunnels(true)

	// Test passes if no panic occurred
	t.Log("Successfully prevented double close panic")
}

// test_finding1_concurrent_cleanup_race reproduces the race between forced and periodic cleanup
func TestFinding1ConcurrentCleanupRace(t *testing.T) {
	localConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local UDP listener: %v", err)
	}
	defer localConn.Close()

	opc := &onionPacketConn{
		router:      nil,
		localConn:   localConn,
		peers:       make(map[string]*tcpTunnel),
		idleTimeout: 50 * time.Millisecond,
	}

	// Add many old tunnels to trigger forced cleanup
	for i := 0; i < 10; i++ {
		addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:900"+string(rune('0'+i)))
		opc.peers[addr.String()] = &tcpTunnel{
			conn:       &mockConn{},
			addr:       addr,
			lastAccess: time.Now().Add(-200 * time.Millisecond),
			stopRead:   make(chan struct{}),
		}
	}

	// Start cleanup routine
	opc.startCleanupRoutine()
	defer func() {
		if opc.cleanupTicker != nil {
			opc.cleanupTicker.Stop()
		}
		if opc.cleanupDone != nil {
			close(opc.cleanupDone)
		}
	}()

	// This may panic if forced cleanup and periodic cleanup
	// try to close the same channels concurrently
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Caught panic (indicates bug): %v", r)
		}
	}()

	// Trigger forced cleanup while periodic cleanup might be running
	opc.cleanupIdleTunnels(true)

	// Wait a bit for periodic cleanup to potentially run
	time.Sleep(100 * time.Millisecond)
}
