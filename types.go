// Package sshor provides SSH-based onion routing with standard net interfaces
package sshor

import (
	"net"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// Code relocated from: sshor.go
//
// This file contains internal types used by the Router for managing sessions
// and TCP tunnels. These types are not exported and are implementation details.

// routeSession tracks an active routing session
type routeSession struct {
	id     string
	client *gossh.Client
	conn   net.Conn
}

// tcpTunnel represents a TCP tunnel for UDP packet forwarding
type tcpTunnel struct {
	conn       net.Conn
	addr       net.Addr
	lastAccess time.Time
}
