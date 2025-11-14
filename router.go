// Package sshor provides SSH-based onion routing with standard net interfaces
package sshor

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// Code relocated from: sshor.go
//
// This file contains the Router type and all its methods. The Router is the
// core component that establishes and maintains the SSH tunnel chain, and
// provides methods for creating connections through the onion route.

// Router provides onion routing through SSH servers
type Router struct {
	mu       sync.RWMutex
	config   Config
	client   *gossh.Client
	server   *ssh.Server
	sessions map[string]*routeSession
}

// NewRouter creates a new onion router with the given configuration
func NewRouter(config Config) *Router {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	return &Router{
		config:   config,
		sessions: make(map[string]*routeSession),
	}
}

// validateConfig checks that all required configuration fields are present
func (r *Router) validateConfig() error {
	// Validate ServerKey
	if r.config.ServerKey == nil {
		return fmt.Errorf("ServerKey is required but was nil")
	}

	// Validate Hops
	if len(r.config.Hops) == 0 {
		return fmt.Errorf("at least one SSH hop is required")
	}

	// Validate each hop's keys
	for i, hop := range r.config.Hops {
		if hop.PrivateKey == nil {
			return fmt.Errorf("hop %d: PrivateKey is required but was nil", i+1)
		}
		if hop.HostKey == nil {
			return fmt.Errorf("hop %d: HostKey is required but was nil", i+1)
		}
		if hop.Address == "" {
			return fmt.Errorf("hop %d: Address is required but was empty", i+1)
		}
		if hop.User == "" {
			return fmt.Errorf("hop %d: User is required but was empty", i+1)
		}
	}

	// Validate ServerAddr
	if r.config.ServerAddr == "" {
		return fmt.Errorf("ServerAddr is required but was empty")
	}

	return nil
}

// Start initializes the router and starts the embedded SSH server
func (r *Router) Start(ctx context.Context) error {
	// Validate configuration first
	if err := r.validateConfig(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Establish onion route
	if err := r.connectRoute(ctx); err != nil {
		return fmt.Errorf("failed to establish route: %w", err)
	}

	// Start embedded SSH server
	if err := r.startServer(); err != nil {
		return fmt.Errorf("failed to start SSH server: %w", err)
	}

	return nil
}

// connectRoute establishes the SSH tunnel chain
func (r *Router) connectRoute(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Configuration is already validated in Start()
	// Connect through all hops
	var client *gossh.Client
	for i, hop := range r.config.Hops {
		var err error
		client, err = r.connectHop(ctx, hop, client)
		if err != nil {
			return fmt.Errorf("failed to connect to hop %d (%s): %w",
				i+1, hop.Address, err)
		}
	}

	r.client = client
	return nil
}

// connectHop connects to a single SSH hop
func (r *Router) connectHop(ctx context.Context, hop SSHHop,
	prevClient *gossh.Client) (*gossh.Client, error) {

	config := &gossh.ClientConfig{
		User: hop.User,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(hop.PrivateKey),
		},
		HostKeyCallback: gossh.FixedHostKey(hop.HostKey),
		Timeout:         r.config.Timeout,
	}

	var conn net.Conn
	var err error

	if prevClient == nil {
		// Direct connection to first hop
		dialer := &net.Dialer{Timeout: config.Timeout}
		conn, err = dialer.DialContext(ctx, "tcp", hop.Address)
	} else {
		// Connect through previous SSH client
		conn, err = prevClient.Dial("tcp", hop.Address)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", hop.Address, err)
	}

	sshConn, chans, reqs, err := gossh.NewClientConn(conn, hop.Address, config)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH handshake failed: %w", err)
	}

	return gossh.NewClient(sshConn, chans, reqs), nil
}

// startServer starts the embedded SSH server
func (r *Router) startServer() error {
	r.server = &ssh.Server{
		Addr:        r.config.ServerAddr,
		HostSigners: []ssh.Signer{r.config.ServerKey},
		Handler:     r.handleSSHSession,
	}

	// Channel to signal when server is ready or if an error occurs
	errChan := make(chan error, 1)

	go func() {
		if err := r.server.ListenAndServe(); err != nil && err != ssh.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for server to start listening or fail
	// The server sets its listener before calling Serve(), so we can check
	// if it's ready by attempting a brief connection
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(5 * time.Second)
	for {
		select {
		case err := <-errChan:
			return fmt.Errorf("SSH server failed to start: %w", err)
		case <-timeout:
			return fmt.Errorf("SSH server startup timeout")
		case <-ticker.C:
			// Check if server is listening by attempting to connect
			conn, err := net.DialTimeout("tcp", r.config.ServerAddr, 50*time.Millisecond)
			if err == nil {
				conn.Close()
				return nil // Server is ready
			}
		}
	}
}

// handleSSHSession handles incoming SSH connections
func (r *Router) handleSSHSession(s ssh.Session) {
	// Forward the SSH session through the onion route
	r.mu.RLock()
	client := r.client
	r.mu.RUnlock()

	if client == nil {
		s.Close()
		return
	}

	// Create session on remote end
	remoteSession, err := client.NewSession()
	if err != nil {
		s.Close()
		return
	}
	defer remoteSession.Close()

	// Bidirectional forwarding
	remoteSession.Stdin = s
	remoteSession.Stdout = s
	remoteSession.Stderr = s.Stderr()

	// Wait for session to complete
	remoteSession.Wait()
}

// Dial creates a net.Conn through the onion route
func (r *Router) Dial(network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("unsupported network type: %s", network)
	}

	r.mu.RLock()
	client := r.client
	r.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("onion route not established")
	}

	conn, err := client.Dial(network, address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial through route: %w", err)
	}

	return &onionConn{conn: conn}, nil
}

// Listen creates a net.Listener that forwards through the onion route
func (r *Router) Listen(network, address string) (net.Listener, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("unsupported network type: %s", network)
	}

	r.mu.RLock()
	client := r.client
	r.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("onion route not established")
	}

	// Create a reverse tunnel through SSH
	listener, err := client.Listen(network, address)
	if err != nil {
		return nil, fmt.Errorf("failed to create remote listener: %w", err)
	}

	return &onionListener{listener: listener}, nil
}

// ListenPacket creates a net.PacketConn through the onion route
func (r *Router) ListenPacket(network, address string) (net.PacketConn, error) {
	// SSH doesn't natively support UDP forwarding, so we create a TCP tunnel
	// and handle UDP packet encapsulation
	if network != "udp" {
		return nil, fmt.Errorf("unsupported network type: %s", network)
	}

	localConn, err := net.ListenPacket(network, address)
	if err != nil {
		return nil, fmt.Errorf("failed to create local UDP listener: %w", err)
	}

	opc := &onionPacketConn{
		router:      r,
		localConn:   localConn,
		peers:       make(map[string]*tcpTunnel),
		idleTimeout: defaultTunnelIdleTimeout,
	}

	// Start background cleanup routine
	opc.startCleanupRoutine()

	return opc, nil
}

// Close shuts down the router
func (r *Router) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error

	if r.server != nil {
		if err := r.server.Close(); err != nil {
			errs = append(errs, fmt.Errorf("server close: %w", err))
		}
	}

	if r.client != nil {
		if err := r.client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("client close: %w", err))
		}
		r.client = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}
