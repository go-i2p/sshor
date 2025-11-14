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

// onionPacketConn implements net.PacketConn through TCP tunneling
type onionPacketConn struct {
	router    *Router
	localConn net.PacketConn
	mu        sync.RWMutex
	peers     map[string]*tcpTunnel
}

func (opc *onionPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	return opc.localConn.ReadFrom(p)
}

func (opc *onionPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	opc.mu.RLock()
	tunnel, exists := opc.peers[addr.String()]
	opc.mu.RUnlock()

	if !exists {
		// Create new TCP tunnel for this peer
		conn, err := opc.router.Dial("tcp", addr.String())
		if err != nil {
			return 0, fmt.Errorf("failed to create tunnel to %s: %w", addr, err)
		}

		tunnel = &tcpTunnel{conn: conn, addr: addr}

		opc.mu.Lock()
		opc.peers[addr.String()] = tunnel
		opc.mu.Unlock()
	}

	return tunnel.conn.Write(p)
}

func (opc *onionPacketConn) Close() error {
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
