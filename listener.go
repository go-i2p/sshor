// Package sshor provides SSH-based onion routing with standard net interfaces
package sshor

import (
	"net"
)

// Code relocated from: sshor.go
//
// This file implements the onionListener type, which wraps a net.Listener to
// accept connections through the SSH tunnel chain. It implements the
// net.Listener interface for transparent integration.

// onionListener wraps a listener through the onion route
type onionListener struct {
	listener net.Listener
}

func (ol *onionListener) Accept() (net.Conn, error) {
	conn, err := ol.listener.Accept()
	if err != nil {
		return nil, err
	}
	return &onionConn{conn: conn}, nil
}

func (ol *onionListener) Close() error {
	return ol.listener.Close()
}

func (ol *onionListener) Addr() net.Addr {
	return ol.listener.Addr()
}
