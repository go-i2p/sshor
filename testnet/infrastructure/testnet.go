package infrastructure

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/go-i2p/sshor"
	gossh "golang.org/x/crypto/ssh"
)

// ServiceType represents the type of service
type ServiceType int

const (
	TCPEcho ServiceType = iota
	HTTPHello
	UDPEcho
	UDPRelay // TCP-to-UDP relay for testing UDP through onion routes
	JSONRPCPing
)

func (st ServiceType) String() string {
	switch st {
	case TCPEcho:
		return "TCP Echo"
	case HTTPHello:
		return "HTTP Hello"
	case UDPEcho:
		return "UDP Echo"
	case UDPRelay:
		return "UDP Relay"
	case JSONRPCPing:
		return "JSON-RPC PING"
	default:
		return "Unknown"
	}
}

// ServerConfig holds configuration for a testnet server
type ServerConfig struct {
	Index       int
	ServiceType ServiceType
	SSHAddr     string
	ServiceAddr string
	User        string
	HostKey     *KeyPair
	ClientKey   *KeyPair // Key used to connect to other servers
}

// Testnet manages multiple SSH servers with services
type Testnet struct {
	Servers         []*SSHServer
	Configs         []*ServerConfig
	BaseSSHPort     int
	BaseServicePort int
}

// NewTestnet creates a new testnet with the specified number of servers
func NewTestnet(numServers int, distribution map[ServiceType]int) (*Testnet, error) {
	tn := &Testnet{
		Servers:         make([]*SSHServer, 0, numServers),
		Configs:         make([]*ServerConfig, 0, numServers),
		BaseSSHPort:     10000,
		BaseServicePort: 20000,
	}

	// Generate server configs
	serverIndex := 0
	for serviceType, count := range distribution {
		for i := 0; i < count; i++ {
			hostKey, err := GenerateKeyPair()
			if err != nil {
				return nil, fmt.Errorf("failed to generate host key: %w", err)
			}

			clientKey, err := GenerateKeyPair()
			if err != nil {
				return nil, fmt.Errorf("failed to generate client key: %w", err)
			}

			config := &ServerConfig{
				Index:       serverIndex,
				ServiceType: serviceType,
				SSHAddr:     fmt.Sprintf("127.0.0.1:%d", tn.BaseSSHPort+serverIndex),
				ServiceAddr: fmt.Sprintf("127.0.0.1:%d", tn.BaseServicePort+serverIndex),
				User:        fmt.Sprintf("user%d", serverIndex),
				HostKey:     hostKey,
				ClientKey:   clientKey,
			}

			tn.Configs = append(tn.Configs, config)
			serverIndex++
		}
	}

	return tn, nil
}

// StartServers starts all SSH servers
func (tn *Testnet) StartServers(ctx context.Context) error {
	for _, config := range tn.Configs {
		// For testnet, we allow connections from any client key
		server := NewSSHServer(config.SSHAddr, config.User, config.HostKey, nil)

		if err := server.Start(ctx); err != nil {
			return fmt.Errorf("failed to start server %d: %w", config.Index, err)
		}

		tn.Servers = append(tn.Servers, server)
		log.Printf("Started SSH server %d (%s) at %s", config.Index, config.ServiceType, config.SSHAddr)
	}

	return nil
}

// GetRouter creates a router through specified hops
func (tn *Testnet) GetRouter(hopIndices []int) (*sshor.Router, error) {
	if len(hopIndices) == 0 {
		return nil, fmt.Errorf("at least one hop is required")
	}

	// Build hops
	hops := make([]sshor.SSHHop, 0, len(hopIndices))
	for _, idx := range hopIndices {
		if idx < 0 || idx >= len(tn.Configs) {
			return nil, fmt.Errorf("hop index %d out of range", idx)
		}

		config := tn.Configs[idx]
		hop := sshor.SSHHop{
			Address:    config.SSHAddr,
			User:       config.User,
			PrivateKey: config.ClientKey.PrivateKey,
			HostKey:    config.HostKey.PublicKey,
		}
		hops = append(hops, hop)
	}

	// Generate server key for the router's embedded SSH server
	serverKey, err := GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate server key: %w", err)
	}

	// Find available port for router's SSH server
	serverAddr, err := getFreePort()
	if err != nil {
		return nil, fmt.Errorf("failed to get free port: %w", err)
	}

	routerConfig := sshor.Config{
		Hops:       hops,
		ServerAddr: serverAddr,
		ServerKey:  ToGliderLabsSigner(serverKey.PrivateKey),
		Timeout:    10 * time.Second,
	}

	router := sshor.NewRouter(routerConfig)
	return router, nil
}

// CreateDirectSSHClient creates a direct SSH client to a server
func (tn *Testnet) CreateDirectSSHClient(serverIndex int) (*gossh.Client, error) {
	if serverIndex < 0 || serverIndex >= len(tn.Configs) {
		return nil, fmt.Errorf("server index %d out of range", serverIndex)
	}

	config := tn.Configs[serverIndex]

	sshConfig := &gossh.ClientConfig{
		User: config.User,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(config.ClientKey.PrivateKey),
		},
		HostKeyCallback: gossh.FixedHostKey(config.HostKey.PublicKey),
		Timeout:         10 * time.Second,
	}

	client, err := gossh.Dial("tcp", config.SSHAddr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server %d: %w", serverIndex, err)
	}

	return client, nil
}

// Shutdown stops all servers
func (tn *Testnet) Shutdown() error {
	var errs []error
	for i, server := range tn.Servers {
		if err := server.Close(); err != nil {
			errs = append(errs, fmt.Errorf("server %d: %w", i, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

// getFreePort finds an available port
func getFreePort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := listener.Addr().String()
	listener.Close()

	// Small delay to ensure port is released
	time.Sleep(10 * time.Millisecond)

	return addr, nil
}

// GetFreeUDPPort finds an available UDP port
func GetFreeUDPPort() (string, error) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := conn.LocalAddr().String()
	conn.Close()

	// Small delay to ensure port is released
	time.Sleep(10 * time.Millisecond)

	return addr, nil
}
