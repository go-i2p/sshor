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

func (opc *onionPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	return opc.localConn.ReadFrom(p)
}

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
		}

		opc.mu.Lock()
		opc.peers[addr.String()] = tunnel
		opc.mu.Unlock()
	} else {
		// Update last access time
		opc.mu.Lock()
		tunnel.lastAccess = time.Now()
		opc.mu.Unlock()
	}

	return tunnel.conn.Write(p)
}

// cleanupIdleTunnels removes tunnels that haven't been accessed recently
func (opc *onionPacketConn) cleanupIdleTunnels(force bool) {
	opc.mu.Lock()
	defer opc.mu.Unlock()

	now := time.Now()
	for addr, tunnel := range opc.peers {
		idleTime := now.Sub(tunnel.lastAccess)
		if force || idleTime > opc.idleTimeout {
			tunnel.conn.Close()
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
