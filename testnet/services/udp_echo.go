package services

import (
	"log"
	"net"
)

// UDPEchoService implements a UDP echo server
type UDPEchoService struct {
	conn   net.PacketConn
	done   chan struct{}
	closed bool
}

// NewUDPEchoService creates a new UDP echo service
func NewUDPEchoService(conn net.PacketConn) *UDPEchoService {
	return &UDPEchoService{
		conn:   conn,
		done:   make(chan struct{}),
		closed: false,
	}
}

// Serve starts the UDP echo service
func (s *UDPEchoService) Serve() error {
	buffer := make([]byte, 65535)
	for {
		select {
		case <-s.done:
			return nil
		default:
			n, addr, err := s.conn.ReadFrom(buffer)
			if err != nil {
				select {
				case <-s.done:
					return nil
				default:
					log.Printf("UDP echo read error: %v", err)
					return err
				}
			}

			// Echo the packet back
			go func(data []byte, dest net.Addr) {
				_, err := s.conn.WriteTo(data, dest)
				if err != nil {
					log.Printf("UDP echo write error: %v", err)
				}
			}(buffer[:n], addr)
		}
	}
}

// Close shuts down the service
func (s *UDPEchoService) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)
	return s.conn.Close()
}

// Addr returns the service address
func (s *UDPEchoService) Addr() net.Addr {
	return s.conn.LocalAddr()
}
