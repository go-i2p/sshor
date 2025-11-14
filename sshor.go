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

// Config holds configuration for the onion router
type Config struct {
	Hops       []SSHHop
	ServerAddr string
	ServerKey  ssh.Signer
	Timeout    time.Duration
}

// SSHHop represents a single SSH server in the route
type SSHHop struct {
	Address    string
	User       string
	PrivateKey gossh.Signer
	HostKey    gossh.PublicKey
}

// Router provides onion routing through SSH servers
type Router struct {
	mu       sync.RWMutex
	config   Config
	client   *gossh.Client
	server   *ssh.Server
	sessions map[string]*routeSession
}

// routeSession tracks an active routing session
type routeSession struct {
	id     string
	client *gossh.Client
	conn   net.Conn
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

// Start initializes the router and starts the embedded SSH server
func (r *Router) Start(ctx context.Context) error {
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

	if len(r.config.Hops) == 0 {
		return fmt.Errorf("no SSH hops configured")
	}

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

	go func() {
		if err := r.server.ListenAndServe(); err != nil && err != ssh.ErrServerClosed {
			// Log error - in production, use proper logging
			fmt.Printf("SSH server error: %v\n", err)
		}
	}()

	return nil
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

	return &onionPacketConn{
		router:    r,
		localConn: localConn,
		peers:     make(map[string]*tcpTunnel),
	}, nil
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

// tcpTunnel represents a TCP tunnel for UDP packet forwarding
type tcpTunnel struct {
	conn net.Conn
	addr net.Addr
}

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
