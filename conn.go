// Package sshor provides SSH-based onion routing with standard net interfaces
package sshor

import (
	"net"
	"time"
)

// Code relocated from: sshor.go
//
// This file implements the onionConn type, which wraps a net.Conn to route
// traffic through the SSH tunnel chain. It implements the net.Conn interface,
// making it a drop-in replacement for standard connections.

// onionConn wraps a connection through the onion route
type onionConn struct {
	conn net.Conn
}

func (oc *onionConn) Read(b []byte) (n int, err error) {
	return oc.conn.Read(b)
}

func (oc *onionConn) Write(b []byte) (n int, err error) {
	return oc.conn.Write(b)
}

func (oc *onionConn) Close() error {
	return oc.conn.Close()
}

func (oc *onionConn) LocalAddr() net.Addr {
	return oc.conn.LocalAddr()
}

func (oc *onionConn) RemoteAddr() net.Addr {
	return oc.conn.RemoteAddr()
}

func (oc *onionConn) SetDeadline(t time.Time) error {
	return oc.conn.SetDeadline(t)
}

func (oc *onionConn) SetReadDeadline(t time.Time) error {
	return oc.conn.SetReadDeadline(t)
}

func (oc *onionConn) SetWriteDeadline(t time.Time) error {
	return oc.conn.SetWriteDeadline(t)
}
