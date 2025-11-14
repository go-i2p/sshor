package sshor

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// mockConnWithError simulates a connection that fails after N reads
type mockConnWithError struct {
	mockConn
	readsBeforeError int
	readCount        int
}

func (m *mockConnWithError) Read(b []byte) (n int, err error) {
	if m.closed {
		return 0, io.EOF
	}
	m.readCount++
	if m.readCount > m.readsBeforeError {
		return 0, errors.New("simulated read error")
	}
	// Return timeout to keep goroutine running until error
	return 0, &net.OpError{Op: "read", Err: errors.New("timeout")}
}

// test_finding3_leak_prevented verifies fix removes dead tunnels
func TestFinding3LeakPrevented(t *testing.T) {
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

	// Add a tunnel with a connection that will fail
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:9001")
	mockConn := &mockConnWithError{
		readsBeforeError: 2, // Fail after 2 reads
	}

	tunnel := &tcpTunnel{
		conn:       mockConn,
		addr:       addr,
		lastAccess: time.Now(),
		stopRead:   make(chan struct{}),
	}

	opc.mu.Lock()
	opc.peers[addr.String()] = tunnel
	opc.mu.Unlock()

	// Start the read goroutine
	go opc.readFromTunnel(tunnel)

	// Wait for the connection to fail and goroutine to exit
	time.Sleep(100 * time.Millisecond)

	// After fix: Tunnel should be removed from map when goroutine exits
	opc.mu.RLock()
	_, stillExists := opc.peers[addr.String()]
	opc.mu.RUnlock()

	if stillExists {
		t.Error("Tunnel still in map after goroutine died - fix not working")
	} else {
		t.Log("Fix successful: dead tunnel removed from map")
	}
}

// test_finding3_no_accumulation verifies dead tunnels don't accumulate
func TestFinding3NoAccumulation(t *testing.T) {
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

	// Create multiple tunnels that will fail
	for i := 0; i < 5; i++ {
		addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:900"+string(rune('0'+i)))
		mockConn := &mockConnWithError{
			readsBeforeError: 1,
		}

		tunnel := &tcpTunnel{
			conn:       mockConn,
			addr:       addr,
			lastAccess: time.Now(),
			stopRead:   make(chan struct{}),
		}

		opc.mu.Lock()
		opc.peers[addr.String()] = tunnel
		opc.mu.Unlock()

		// Start read goroutine that will die
		go opc.readFromTunnel(tunnel)
	}

	// Wait for goroutines to die and clean up
	time.Sleep(100 * time.Millisecond)

	// After fix: All dead tunnels should be removed
	opc.mu.RLock()
	peerCount := len(opc.peers)
	opc.mu.RUnlock()

	if peerCount == 0 {
		t.Log("Fix successful: all dead tunnels cleaned up")
	} else {
		t.Errorf("Expected 0 tunnels after cleanup, got %d", peerCount)
	}
}
