package services

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

// UDPRelayService implements a TCP-to-UDP relay for testing onion routing
// It accepts TCP connections and relays data to/from a UDP destination
type UDPRelayService struct {
	tcpListener net.Listener
	udpTarget   string
	done        chan struct{}
	conns       sync.WaitGroup
}

// NewUDPRelayService creates a new UDP relay service
// tcpListener: TCP listener for incoming relay connections
// udpTarget: UDP address to relay packets to (e.g., "127.0.0.1:20000")
func NewUDPRelayService(tcpListener net.Listener, udpTarget string) *UDPRelayService {
	return &UDPRelayService{
		tcpListener: tcpListener,
		udpTarget:   udpTarget,
		done:        make(chan struct{}),
	}
}

// Serve starts the relay service
func (s *UDPRelayService) Serve() error {
	for {
		conn, err := s.tcpListener.Accept()
		if err != nil {
			select {
			case <-s.done:
				s.conns.Wait()
				return nil
			default:
				log.Printf("UDP relay accept error: %v", err)
				return err
			}
		}

		s.conns.Add(1)
		go s.handleConnection(conn)
	}
}

func (s *UDPRelayService) handleConnection(tcpConn net.Conn) {
	defer s.conns.Done()
	defer tcpConn.Close()

	// Create UDP connection to target
	udpConn, err := net.Dial("udp", s.udpTarget)
	if err != nil {
		log.Printf("UDP relay failed to connect to %s: %v", s.udpTarget, err)
		return
	}
	defer udpConn.Close()

	// Bidirectional relay
	done := make(chan struct{}, 2)

	// TCP -> UDP
	go func() {
		defer func() { done <- struct{}{} }()

		for {
			// Read length prefix (4 bytes)
			var length uint32
			if err := binary.Read(tcpConn, binary.BigEndian, &length); err != nil {
				if err != io.EOF {
					log.Printf("UDP relay TCP read length error: %v", err)
				}
				return
			}

			if length > 65535 {
				log.Printf("UDP relay packet too large: %d", length)
				return
			}

			// Read packet data
			data := make([]byte, length)
			if _, err := io.ReadFull(tcpConn, data); err != nil {
				log.Printf("UDP relay TCP read data error: %v", err)
				return
			}

			// Send to UDP destination
			if _, err := udpConn.Write(data); err != nil {
				log.Printf("UDP relay UDP write error: %v", err)
				return
			}
		}
	}()

	// UDP -> TCP
	go func() {
		defer func() { done <- struct{}{} }()

		buffer := make([]byte, 65535)
		for {
			n, err := udpConn.Read(buffer)
			if err != nil {
				if err != io.EOF {
					log.Printf("UDP relay UDP read error: %v", err)
				}
				return
			}

			// Write length prefix
			if err := binary.Write(tcpConn, binary.BigEndian, uint32(n)); err != nil {
				log.Printf("UDP relay TCP write length error: %v", err)
				return
			}

			// Write packet data
			if _, err := tcpConn.Write(buffer[:n]); err != nil {
				log.Printf("UDP relay TCP write data error: %v", err)
				return
			}
		}
	}()

	<-done
}

// Close shuts down the service
func (s *UDPRelayService) Close() error {
	close(s.done)
	return s.tcpListener.Close()
}

// Addr returns the service address
func (s *UDPRelayService) Addr() net.Addr {
	return s.tcpListener.Addr()
}

// UDPRelayClient wraps a TCP connection to communicate with UDPRelayService
type UDPRelayClient struct {
	conn net.Conn
	mu   sync.Mutex
}

// NewUDPRelayClient creates a client for the UDP relay
func NewUDPRelayClient(conn net.Conn) *UDPRelayClient {
	return &UDPRelayClient{conn: conn}
}

// SendPacket sends a UDP packet through the TCP relay
func (c *UDPRelayClient) SendPacket(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Write length prefix
	if err := binary.Write(c.conn, binary.BigEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("write length: %w", err)
	}

	// Write data
	if _, err := c.conn.Write(data); err != nil {
		return fmt.Errorf("write data: %w", err)
	}

	return nil
}

// ReceivePacket receives a UDP packet through the TCP relay
func (c *UDPRelayClient) ReceivePacket() ([]byte, error) {
	// Read length prefix
	var length uint32
	if err := binary.Read(c.conn, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("read length: %w", err)
	}

	if length > 65535 {
		return nil, fmt.Errorf("packet too large: %d", length)
	}

	// Read data
	data := make([]byte, length)
	if _, err := io.ReadFull(c.conn, data); err != nil {
		return nil, fmt.Errorf("read data: %w", err)
	}

	return data, nil
}

// Close closes the relay client connection
func (c *UDPRelayClient) Close() error {
	return c.conn.Close()
}
