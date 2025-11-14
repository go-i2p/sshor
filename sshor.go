// Package sshor provides SSH-based onion routing with standard net interfaces.
//
// This package implements onion routing (layered encryption routing) using SSH
// as the transport protocol. It provides standard Go net interfaces (net.Conn,
// net.Listener, net.PacketConn) for transparent integration with existing code.
//
// File Organization:
//   - config.go        - Configuration types (Config, SSHHop)
//   - router.go        - Main Router type and routing logic
//   - types.go         - Internal types (routeSession, tcpTunnel)
//   - conn.go          - net.Conn wrapper (onionConn)
//   - listener.go      - net.Listener wrapper (onionListener)
//   - packet_conn.go   - net.PacketConn wrapper (onionPacketConn)
//
// Example usage:
//
//	router := sshor.NewRouter(config)
//	if err := router.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	defer router.Close()
//
//	// Use as standard net.Conn
//	conn, err := router.Dial("tcp", "example.com:80")
package sshor
