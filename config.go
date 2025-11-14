// Package sshor provides SSH-based onion routing with standard net interfaces
package sshor

import (
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// Code relocated from: sshor.go
//
// This file contains configuration types for the onion router.
// These types define how the SSH tunnel chain is constructed and configured.

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
