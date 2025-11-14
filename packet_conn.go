// Package sshor provides SSH-based onion routing with standard net interfaces
package sshor

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// Code relocated from: sshor.go
//
// This file implements the onionPacketConn type, which provides UDP-like packet
// communication through the SSH tunnel chain. Since SSH doesn't natively support
// UDP forwarding, this implementation creates TCP tunnels for each peer and
// encapsulates UDP packets over TCP.
//
// SYMMETRIC UDP FORWARDING IMPLEMENTATION
// ----------------------------------------
// This implementation provides SYMMETRIC (bidirectional) UDP forwarding:
//
// - WriteTo(): Sends packets THROUGH the SSH tunnel chain
//   * Packets are routed through all configured hops
//   * Traffic is encrypted and onion-routed
//   * Creates persistent TCP tunnels for each destination
//
// - ReadFrom(): Receives packets from multiple sources:
//   * Packets that came back through SSH tunnels (tunnel responses)
//   * Direct packets from the local network (if any)
//
// How it works:
// 1. When WriteTo() creates a TCP tunnel to a destination, it also spawns
//    a goroutine that reads from that tunnel
// 2. Any data received on the tunnel is forwarded to the local UDP socket
//    with the appropriate source address
// 3. ReadFrom() receives these forwarded packets, creating symmetric behavior
// 4. This works without modifying the SSH server - we use standard TCP tunnels
//
// Resource Management:
// - Idle tunnels are automatically closed after 5 minutes
// - Maximum 1000 peer tunnels before forced cleanup
// - Each tunnel's read goroutine is properly stopped on cleanup

const (
	// Default idle timeout for TCP tunnels (5 minutes)
	defaultTunnelIdleTimeout = 5 * time.Minute
	// Maximum number of peer tunnels before cleanup is forced
	maxPeerTunnels = 1000
)

// onionPacketConn implements net.PacketConn through TCP tunneling
type onionPacketConn struct {
	router        *Router
	localConn     net.PacketConn
	mu            sync.RWMutex
	peers         map[string]*tcpTunnel
	idleTimeout   time.Duration
	cleanupTicker *time.Ticker
	cleanupDone   chan struct{}
}

// ReadFrom receives packets from the local UDP socket.
// With symmetric UDP enabled, this receives both:
// - Direct packets from the local network
// - Packets forwarded back through SSH tunnels (from tunnel read goroutines)
func (opc *onionPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	return opc.localConn.ReadFrom(p)
}

// WriteTo sends packets through the SSH tunnel chain to the specified address.
// For each unique destination address, a persistent TCP tunnel is created and reused.
// Idle tunnels are automatically cleaned up after the configured timeout.
func (opc *onionPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	opc.mu.RLock()
	tunnel, exists := opc.peers[addr.String()]
	opc.mu.RUnlock()

	if !exists {
		// Check if we've hit the max peer limit
		opc.mu.RLock()
		peerCount := len(opc.peers)
		opc.mu.RUnlock()

		if peerCount >= maxPeerTunnels {
			// Force cleanup of idle tunnels
			opc.cleanupIdleTunnels(true)
		}

		// Create new TCP tunnel for this peer
		conn, err := opc.router.Dial("tcp", addr.String())
		if err != nil {
			return 0, fmt.Errorf("failed to create tunnel to %s: %w", addr, err)
		}

		tunnel = &tcpTunnel{
			conn:       conn,
			addr:       addr,
			lastAccess: time.Now(),
			stopRead:   make(chan struct{}),
		}

		opc.mu.Lock()
		opc.peers[addr.String()] = tunnel
		opc.mu.Unlock()

		// Start goroutine to read responses from tunnel and forward to local UDP socket
		go opc.readFromTunnel(tunnel)
	} else {
		// Update last access time
		opc.mu.Lock()
		tunnel.lastAccess = time.Now()
		opc.mu.Unlock()
	}

	return tunnel.conn.Write(p)
}

// readFromTunnel reads packets from the TCP tunnel and forwards them to the local UDP socket.
// This enables symmetric UDP forwarding - responses come back through the tunnel.
func (opc *onionPacketConn) readFromTunnel(tunnel *tcpTunnel) {
	buffer := make([]byte, 65535) // Max UDP packet size

	for {
		select {
		case <-tunnel.stopRead:
			return
		default:
			// Set read deadline to allow periodic checking of stopRead
			tunnel.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, err := tunnel.conn.Read(buffer)
			if err != nil {
				// Check if it's a timeout (expected) or real error
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Normal timeout, check stopRead and continue
				}
				// Real error or connection closed
				return
			}

			if n > 0 {
				// Forward the packet to local UDP socket with source address
				// This makes it appear as if it came from the remote peer
				_, err := opc.localConn.WriteTo(buffer[:n], tunnel.addr)
				if err != nil {
					// Log error but continue - don't stop forwarding for one failure
					continue
				}

				// Update last access time
				opc.mu.Lock()
				tunnel.lastAccess = time.Now()
				opc.mu.Unlock()
			}
		}
	}
}

// cleanupIdleTunnels removes tunnels that haven't been accessed recently
func (opc *onionPacketConn) cleanupIdleTunnels(force bool) {
	opc.mu.Lock()
	defer opc.mu.Unlock()

	now := time.Now()
	for addr, tunnel := range opc.peers {
		idleTime := now.Sub(tunnel.lastAccess)
		if force || idleTime > opc.idleTimeout {
			// Stop the read goroutine
			close(tunnel.stopRead)
			// Close the connection
			tunnel.conn.Close()
			// Remove from map
			delete(opc.peers, addr)
		}
	}
}

// startCleanupRoutine starts a background goroutine to periodically clean up idle tunnels
func (opc *onionPacketConn) startCleanupRoutine() {
	opc.cleanupTicker = time.NewTicker(opc.idleTimeout / 2)
	opc.cleanupDone = make(chan struct{})

	go func() {
		for {
			select {
			case <-opc.cleanupTicker.C:
				opc.cleanupIdleTunnels(false)
			case <-opc.cleanupDone:
				return
			}
		}
	}()
}

func (opc *onionPacketConn) Close() error {
	// Stop cleanup routine
	if opc.cleanupTicker != nil {
		opc.cleanupTicker.Stop()
	}
	if opc.cleanupDone != nil {
		close(opc.cleanupDone)
	}

	opc.mu.Lock()
	defer opc.mu.Unlock()

	for _, tunnel := range opc.peers {
		// Stop the read goroutine
		close(tunnel.stopRead)
		// Close the connection
		tunnel.conn.Close()
	}
	opc.peers = make(map[string]*tcpTunnel)

	return opc.localConn.Close()
}

func (opc *onionPacketConn) LocalAddr() net.Addr {
	return opc.localConn.LocalAddr()
}

func (opc *onionPacketConn) SetDeadline(t time.Time) error {
	return opc.localConn.SetDeadline(t)
}

func (opc *onionPacketConn) SetReadDeadline(t time.Time) error {
	return opc.localConn.SetReadDeadline(t)
}

func (opc *onionPacketConn) SetWriteDeadline(t time.Time) error {
	return opc.localConn.SetWriteDeadline(t)
}
