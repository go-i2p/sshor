package infrastructure

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// SSHServer represents a single SSH server instance
type SSHServer struct {
	Address       string
	User          string
	HostKey       *KeyPair
	AuthorizedKey gossh.PublicKey // Public key authorized to connect

	server    *ssh.Server
	mu        sync.Mutex
	listeners map[string]net.Listener
	clients   map[string]net.Conn
}

// NewSSHServer creates a new SSH server instance
func NewSSHServer(address, user string, hostKey *KeyPair, authorizedKey gossh.PublicKey) *SSHServer {
	return &SSHServer{
		Address:       address,
		User:          user,
		HostKey:       hostKey,
		AuthorizedKey: authorizedKey,
		listeners:     make(map[string]net.Listener),
		clients:       make(map[string]net.Conn),
	}
}

// Start starts the SSH server
func (s *SSHServer) Start(ctx context.Context) error {
	s.server = &ssh.Server{
		Addr:    s.Address,
		Handler: s.handleSession,
		LocalPortForwardingCallback: func(ctx ssh.Context, dhost string, dport uint32) bool {
			// Allow all port forwarding
			return true
		},
		ReversePortForwardingCallback: func(ctx ssh.Context, host string, port uint32) bool {
			// Allow all reverse port forwarding
			return true
		},
		PublicKeyHandler: func(ctx ssh.Context, key ssh.PublicKey) bool {
			// Check if the public key matches our authorized key
			if s.AuthorizedKey == nil {
				return true // Accept all keys if no specific key is set
			}
			return ssh.KeysEqual(key, s.AuthorizedKey)
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"direct-tcpip": ssh.DirectTCPIPHandler,
		},
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        s.handleTCPIPForward,
			"cancel-tcpip-forward": s.handleCancelTCPIPForward,
		},
	}

	s.server.AddHostKey(ToGliderLabsSigner(s.HostKey.PrivateKey))

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != ssh.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for server to be ready
	select {
	case err := <-errChan:
		return fmt.Errorf("SSH server failed to start: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Give server time to start
		return s.waitForReady()
	}
}

func (s *SSHServer) waitForReady() error {
	for i := 0; i < 50; i++ {
		conn, err := net.Dial("tcp", s.Address)
		if err == nil {
			conn.Close()
			return nil
		}
		// Small delay between attempts
		select {
		case <-context.Background().Done():
			return context.Background().Err()
		default:
		}
	}
	return fmt.Errorf("SSH server did not become ready")
}

func (s *SSHServer) handleSession(sess ssh.Session) {
	// Simple shell session - just echo commands back
	io.WriteString(sess, fmt.Sprintf("Welcome to %s\n", s.Address))
	io.Copy(sess, sess)
}

// handleTCPIPForward handles reverse port forwarding requests
func (s *SSHServer) handleTCPIPForward(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	var reqPayload struct {
		Addr string
		Port uint32
	}

	if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
		log.Printf("Failed to unmarshal tcpip-forward request: %v", err)
		return false, nil
	}

	addr := fmt.Sprintf("%s:%d", reqPayload.Addr, reqPayload.Port)

	// Create listener
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Failed to listen on %s: %v", addr, err)
		return false, nil
	}

	// Store listener
	s.mu.Lock()
	s.listeners[addr] = ln
	s.mu.Unlock()

	// Handle incoming connections
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go s.handleForwardedConnection(ctx, conn, reqPayload.Addr, reqPayload.Port)
		}
	}()

	// Return the actual port we're listening on
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port uint32
	fmt.Sscanf(portStr, "%d", &port)

	return true, gossh.Marshal(struct{ Port uint32 }{Port: port})
}

func (s *SSHServer) handleForwardedConnection(ctx ssh.Context, conn net.Conn, host string, port uint32) {
	defer conn.Close()

	// Open a channel back to the client
	payload := gossh.Marshal(struct {
		Addr       string
		Port       uint32
		OriginAddr string
		OriginPort uint32
	}{
		Addr:       host,
		Port:       port,
		OriginAddr: "127.0.0.1",
		OriginPort: 0,
	})

	channel, reqs, err := ctx.Value(ssh.ContextKeyConn).(*gossh.ServerConn).OpenChannel("forwarded-tcpip", payload)
	if err != nil {
		log.Printf("Failed to open forwarded-tcpip channel: %v", err)
		return
	}
	defer channel.Close()

	go gossh.DiscardRequests(reqs)

	// Bidirectional copy
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(channel, conn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(conn, channel)
		done <- struct{}{}
	}()

	<-done
}

// handleCancelTCPIPForward handles cancel reverse port forwarding requests
func (s *SSHServer) handleCancelTCPIPForward(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	var reqPayload struct {
		Addr string
		Port uint32
	}

	if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
		return false, nil
	}

	addr := fmt.Sprintf("%s:%d", reqPayload.Addr, reqPayload.Port)

	s.mu.Lock()
	ln, ok := s.listeners[addr]
	if ok {
		delete(s.listeners, addr)
	}
	s.mu.Unlock()

	if ok {
		ln.Close()
	}

	return true, nil
}

// Close shuts down the SSH server
func (s *SSHServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close all listeners
	for _, ln := range s.listeners {
		ln.Close()
	}
	s.listeners = make(map[string]net.Listener)

	// Close all client connections
	for _, conn := range s.clients {
		conn.Close()
	}
	s.clients = make(map[string]net.Conn)

	if s.server != nil {
		return s.server.Close()
	}
	return nil
}
