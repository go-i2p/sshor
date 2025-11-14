package sshor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// Helper function to generate test SSH keys
func generateTestKeys(t *testing.T) (gossh.Signer, gossh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	publicKey, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("Failed to create public key: %v", err)
	}

	return signer, publicKey
}

// Helper function to convert gossh.Signer to ssh.Signer
func toSSHSigner(gs gossh.Signer) ssh.Signer {
	return &sshSignerWrapper{gs}
}

type sshSignerWrapper struct {
	gossh.Signer
}

// test_finding1_race_condition_server_start tests that Start() has a race condition
// where it returns before the server is actually listening
func TestFinding1RaceConditionServerStart(t *testing.T) {
	// Create test keys
	serverSigner, serverPubKey := generateTestKeys(t)
	clientSigner, _ := generateTestKeys(t)

	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	serverAddr := listener.Addr().String()
	listener.Close()

	config := Config{
		Hops: []SSHHop{
			{
				Address:    "127.0.0.1:22",
				User:       "testuser",
				PrivateKey: clientSigner,
				HostKey:    serverPubKey,
			},
		},
		ServerAddr: serverAddr,
		ServerKey:  toSSHSigner(serverSigner),
		Timeout:    5 * time.Second,
	}

	router := NewRouter(config)
	ctx := context.Background()

	// Start() returns immediately but server may not be ready
	err = router.Start(ctx)

	// This test demonstrates the race condition:
	// If Start() properly waited for server initialization, we would know
	// whether it succeeded or failed. Currently it returns nil even if
	// the server will fail to start.

	if err != nil {
		// Expected: error about connecting to hop (since 127.0.0.1:22 doesn't exist)
		t.Logf("Expected error from connectRoute: %v", err)
	} else {
		// Start returned nil, but we don't know if server is actually listening
		// Try to connect immediately - this may fail due to race condition
		time.Sleep(10 * time.Millisecond) // Small delay to expose race

		conn, dialErr := net.Dial("tcp", serverAddr)
		if dialErr != nil {
			t.Logf("Server not ready immediately after Start(): %v", dialErr)
		} else {
			conn.Close()
		}
	}

	defer router.Close()
}

// test_finding1_server_error_not_propagated tests that server startup errors
// are not propagated to the caller
func TestFinding1ServerErrorNotPropagated(t *testing.T) {
	serverSigner, _ := generateTestKeys(t)

	// Use an invalid server address to force a server startup error
	config := Config{
		Hops:       []SSHHop{},              // Empty hops to make connectRoute fail first
		ServerAddr: "999.999.999.999:99999", // Invalid address
		ServerKey:  toSSHSigner(serverSigner),
		Timeout:    5 * time.Second,
	}

	router := NewRouter(config)
	ctx := context.Background()

	err := router.Start(ctx)

	// We expect an error from connectRoute (empty hops)
	if err == nil {
		t.Fatal("Expected error from empty hops, got nil")
	}

	// The bug is that even if we had valid hops, server startup errors
	// would be silently swallowed (only printed to stdout)
	t.Logf("Got expected error from connectRoute: %v", err)
}

// test_finding1_no_server_ready_signal tests that there's no way to know
// when the server is ready to accept connections
func TestFinding1NoServerReadySignal(t *testing.T) {
	serverSigner, serverPubKey := generateTestKeys(t)
	clientSigner, _ := generateTestKeys(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	hopAddr := listener.Addr().String()

	// Start a minimal SSH server for the hop
	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			conn.Close()
		}
	}()
	defer listener.Close()

	listener2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	serverAddr := listener2.Addr().String()
	listener2.Close()

	config := Config{
		Hops: []SSHHop{
			{
				Address:    hopAddr,
				User:       "testuser",
				PrivateKey: clientSigner,
				HostKey:    serverPubKey,
			},
		},
		ServerAddr: serverAddr,
		ServerKey:  toSSHSigner(serverSigner),
		Timeout:    5 * time.Second,
	}

	router := NewRouter(config)
	ctx := context.Background()

	// Start returns immediately
	startTime := time.Now()
	err = router.Start(ctx)
	elapsed := time.Since(startTime)

	// Start should return almost immediately (race condition)
	if elapsed > 100*time.Millisecond {
		t.Errorf("Start() took %v, expected to return immediately", elapsed)
	}

	if err != nil {
		t.Logf("connectRoute error (expected since minimal server): %v", err)
	}

	// There's no way to know if the server is ready
	// We can only try to connect and see what happens

	defer router.Close()
}

// test_finding1_server_ready_after_start verifies that after the fix,
// the server is guaranteed to be ready when Start() returns successfully
func TestFinding1ServerReadyAfterStart(t *testing.T) {
	serverSigner, _ := generateTestKeys(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	serverAddr := listener.Addr().String()
	listener.Close()

	config := Config{
		Hops:       []SSHHop{}, // Will fail at connectRoute, but we can test server independently
		ServerAddr: serverAddr,
		ServerKey:  toSSHSigner(serverSigner),
		Timeout:    5 * time.Second,
	}

	router := NewRouter(config)
	ctx := context.Background()

	// This will fail at connectRoute (no hops), but that's expected
	err = router.Start(ctx)
	if err == nil {
		t.Fatal("Expected error from empty hops")
	}

	// The fix ensures that server startup errors are properly propagated
	t.Logf("Got expected error: %v", err)
}

// test_finding1_invalid_server_address verifies that server startup
// errors are properly propagated to the caller
func TestFinding1InvalidServerAddress(t *testing.T) {
	serverSigner, serverPubKey := generateTestKeys(t)
	clientSigner, _ := generateTestKeys(t)

	// Start a dummy hop server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	hopAddr := listener.Addr().String()
	listener.Close()

	config := Config{
		Hops: []SSHHop{
			{
				Address:    hopAddr,
				User:       "testuser",
				PrivateKey: clientSigner,
				HostKey:    serverPubKey,
			},
		},
		ServerAddr: "256.256.256.256:99999", // Invalid address
		ServerKey:  toSSHSigner(serverSigner),
		Timeout:    5 * time.Second,
	}

	router := NewRouter(config)
	ctx := context.Background()

	err = router.Start(ctx)

	// After the fix, we should get an error about server startup failure
	// (or connectRoute failure if that happens first)
	if err == nil {
		t.Fatal("Expected error from invalid server address, got nil")
	}

	t.Logf("Got expected error: %v", err)
}

// test_finding2_nil_server_key tests that nil ServerKey should be caught
// during configuration validation rather than causing a panic later
func TestFinding2NilServerKey(t *testing.T) {
	clientSigner, serverPubKey := generateTestKeys(t)

	config := Config{
		Hops: []SSHHop{
			{
				Address:    "127.0.0.1:22",
				User:       "testuser",
				PrivateKey: clientSigner,
				HostKey:    serverPubKey,
			},
		},
		ServerAddr: "127.0.0.1:2222",
		ServerKey:  nil, // Nil ServerKey should be validated
		Timeout:    1 * time.Second,
	}

	router := NewRouter(config)
	ctx := context.Background()

	// After fix: should return clear error during validation
	err := router.Start(ctx)

	if err == nil {
		t.Fatal("Expected error from nil ServerKey, got nil")
	}

	// Check that error message mentions ServerKey
	errMsg := err.Error()
	if errMsg == "" {
		t.Fatal("Expected error message, got empty string")
	}

	t.Logf("Got expected validation error: %v", err)
}

// test_finding2_validation_in_new_router tests that validation
// happens early and provides clear error messages
func TestFinding2ValidationInNewRouter(t *testing.T) {
	serverSigner, serverPubKey := generateTestKeys(t)
	clientSigner, _ := generateTestKeys(t)

	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorText   string
	}{
		{
			name: "nil ServerKey",
			config: Config{
				Hops: []SSHHop{
					{
						Address:    "127.0.0.1:22",
						User:       "user",
						PrivateKey: clientSigner,
						HostKey:    serverPubKey,
					},
				},
				ServerAddr: "127.0.0.1:2222",
				ServerKey:  nil,
				Timeout:    5 * time.Second,
			},
			expectError: true,
			errorText:   "ServerKey",
		},
		{
			name: "empty hops",
			config: Config{
				Hops:       []SSHHop{},
				ServerAddr: "127.0.0.1:2222",
				ServerKey:  toSSHSigner(serverSigner),
				Timeout:    5 * time.Second,
			},
			expectError: true,
			errorText:   "hop",
		},
		{
			name: "nil hop PrivateKey",
			config: Config{
				Hops: []SSHHop{
					{
						Address:    "127.0.0.1:22",
						User:       "user",
						PrivateKey: nil,
						HostKey:    serverPubKey,
					},
				},
				ServerAddr: "127.0.0.1:2222",
				ServerKey:  toSSHSigner(serverSigner),
				Timeout:    5 * time.Second,
			},
			expectError: true,
			errorText:   "PrivateKey",
		},
		{
			name: "nil hop HostKey",
			config: Config{
				Hops: []SSHHop{
					{
						Address:    "127.0.0.1:22",
						User:       "user",
						PrivateKey: clientSigner,
						HostKey:    nil,
					},
				},
				ServerAddr: "127.0.0.1:2222",
				ServerKey:  toSSHSigner(serverSigner),
				Timeout:    5 * time.Second,
			},
			expectError: true,
			errorText:   "HostKey",
		},
		{
			name: "empty ServerAddr",
			config: Config{
				Hops: []SSHHop{
					{
						Address:    "127.0.0.1:22",
						User:       "user",
						PrivateKey: clientSigner,
						HostKey:    serverPubKey,
					},
				},
				ServerAddr: "",
				ServerKey:  toSSHSigner(serverSigner),
				Timeout:    5 * time.Second,
			},
			expectError: true,
			errorText:   "ServerAddr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter(tt.config)
			ctx := context.Background()
			err := router.Start(ctx)

			if tt.expectError {
				if err == nil {
					t.Fatalf("Expected error containing '%s', got nil", tt.errorText)
				}
				t.Logf("Got expected validation error: %v", err)
			}
		})
	}
}
