package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/go-i2p/sshor"
	gossh "golang.org/x/crypto/ssh"
)

var (
	serverAddr     = flag.String("server", "127.0.0.1:2222", "Address for the embedded SSH server")
	serverKeyFile  = flag.String("server-key", "", "Path to server private key file (required)")
	hopsFlag       = flag.String("hops", "", "Comma-separated list of hops in format 'user@host:port' (required)")
	hopKeysDir     = flag.String("hop-keys", "", "Directory containing hop private keys (id_rsa_hop1, id_rsa_hop2, etc.)")
	hopHostKeysDir = flag.String("hop-hostkeys", "", "Directory containing hop host public keys (host_key_hop1.pub, etc.)")
	timeout        = flag.Duration("timeout", 30*time.Second, "Connection timeout")
	help           = flag.Bool("help", false, "Show help message")
)

func main() {
	flag.Parse()

	if *help {
		printUsage()
		os.Exit(0)
	}

	// Validate required flags
	if *serverKeyFile == "" {
		log.Fatal("Error: -server-key is required")
	}
	if *hopsFlag == "" {
		log.Fatal("Error: -hops is required")
	}

	// Load server key
	serverKey, err := loadPrivateKey(*serverKeyFile)
	if err != nil {
		log.Fatalf("Failed to load server key from %s: %v", *serverKeyFile, err)
	}

	// Parse hops
	hops, err := parseHops(*hopsFlag)
	if err != nil {
		log.Fatalf("Failed to parse hops: %v", err)
	}

	// Load keys for each hop
	for i := range hops {
		// Load private key
		keyFile := fmt.Sprintf("%s/id_rsa_hop%d", *hopKeysDir, i+1)
		if *hopKeysDir == "" {
			keyFile = fmt.Sprintf("id_rsa_hop%d", i+1)
		}

		privKey, err := loadPrivateKey(keyFile)
		if err != nil {
			log.Fatalf("Failed to load private key for hop %d from %s: %v", i+1, keyFile, err)
		}
		hops[i].PrivateKey = privKey

		// Load host public key
		hostKeyFile := fmt.Sprintf("%s/host_key_hop%d.pub", *hopHostKeysDir, i+1)
		if *hopHostKeysDir == "" {
			hostKeyFile = fmt.Sprintf("host_key_hop%d.pub", i+1)
		}

		hostKey, err := loadPublicKey(hostKeyFile)
		if err != nil {
			log.Fatalf("Failed to load host key for hop %d from %s: %v", i+1, hostKeyFile, err)
		}
		hops[i].HostKey = hostKey
	}

	// Configure the router
	config := sshor.Config{
		Hops:       hops,
		ServerAddr: *serverAddr,
		ServerKey:  toSSHSigner(serverKey),
		Timeout:    *timeout,
	}

	router := sshor.NewRouter(config)

	ctx := context.Background()
	log.Printf("Starting onion router with %d hops...", len(hops))
	if err := router.Start(ctx); err != nil {
		log.Fatalf("Failed to start router: %v", err)
	}
	defer router.Close()

	log.Printf("Onion router started successfully")
	log.Printf("  Server listening on: %s", *serverAddr)
	log.Printf("  Hops configured: %d", len(hops))
	for i, hop := range hops {
		log.Printf("    Hop %d: %s@%s", i+1, hop.User, hop.Address)
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
}

func printUsage() {
	fmt.Println("sshor - SSH-based Onion Router")
	fmt.Println("\nUsage:")
	fmt.Println("  sshor -server-key <path> -hops <hops> [options]")
	fmt.Println("\nRequired flags:")
	fmt.Println("  -server-key string")
	fmt.Println("        Path to server private key file")
	fmt.Println("  -hops string")
	fmt.Println("        Comma-separated list of hops in format 'user@host:port'")
	fmt.Println("        Example: user1@hop1.com:22,user2@hop2.com:22")
	fmt.Println("\nOptional flags:")
	fmt.Println("  -server string")
	fmt.Println("        Address for the embedded SSH server (default: 127.0.0.1:2222)")
	fmt.Println("  -hop-keys string")
	fmt.Println("        Directory containing hop private keys (default: current directory)")
	fmt.Println("        Expected files: id_rsa_hop1, id_rsa_hop2, etc.")
	fmt.Println("  -hop-hostkeys string")
	fmt.Println("        Directory containing hop host public keys (default: current directory)")
	fmt.Println("        Expected files: host_key_hop1.pub, host_key_hop2.pub, etc.")
	fmt.Println("  -timeout duration")
	fmt.Println("        Connection timeout (default: 30s)")
	fmt.Println("  -help")
	fmt.Println("        Show this help message")
	fmt.Println("\nExample:")
	fmt.Println("  sshor -server-key server.key -hops user@10.0.0.1:22,user@10.0.0.2:22")
}

func parseHops(hopsStr string) ([]sshor.SSHHop, error) {
	parts := strings.Split(hopsStr, ",")
	hops := make([]sshor.SSHHop, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Parse user@host:port
		atIdx := strings.Index(part, "@")
		if atIdx == -1 {
			return nil, fmt.Errorf("invalid hop format '%s': missing @", part)
		}

		user := part[:atIdx]
		address := part[atIdx+1:]

		if user == "" {
			return nil, fmt.Errorf("invalid hop format '%s': empty username", part)
		}
		if address == "" {
			return nil, fmt.Errorf("invalid hop format '%s': empty address", part)
		}

		hops = append(hops, sshor.SSHHop{
			Address: address,
			User:    user,
			// Keys will be loaded separately
		})
	}

	if len(hops) == 0 {
		return nil, fmt.Errorf("no valid hops found")
	}

	return hops, nil
}

func loadPrivateKey(path string) (gossh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	key, err := gossh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return key, nil
}

func loadPublicKey(path string) (gossh.PublicKey, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	key, _, _, _, err := gossh.ParseAuthorizedKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	return key, nil
}

// Helper function to convert gossh.Signer to ssh.Signer
func toSSHSigner(gs gossh.Signer) ssh.Signer {
	return &sshSignerWrapper{gs}
}

type sshSignerWrapper struct {
	gossh.Signer
}
