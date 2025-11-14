package services

import (
	"io"
	"log"
	"net"
)

// TCPEchoService implements a TCP echo server that echoes back all received data
type TCPEchoService struct {
	listener net.Listener
	done     chan struct{}
}

// NewTCPEchoService creates a new TCP echo service
func NewTCPEchoService(listener net.Listener) *TCPEchoService {
	return &TCPEchoService{
		listener: listener,
		done:     make(chan struct{}),
	}
}

// Serve starts the echo service
func (s *TCPEchoService) Serve() error {
	defer close(s.done)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
				log.Printf("TCP echo accept error: %v", err)
				return err
			}
		}

		go s.handleConnection(conn)
	}
}

func (s *TCPEchoService) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Echo all data back to client
	_, err := io.Copy(conn, conn)
	if err != nil && err != io.EOF {
		log.Printf("TCP echo copy error: %v", err)
	}
}

// Close shuts down the service
func (s *TCPEchoService) Close() error {
	close(s.done)
	return s.listener.Close()
}

// Addr returns the service address
func (s *TCPEchoService) Addr() net.Addr {
	return s.listener.Addr()
}
