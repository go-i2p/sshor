package infrastructure

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// KeyPair holds both private and public keys for SSH authentication
type KeyPair struct {
	PrivateKey gossh.Signer
	PublicKey  gossh.PublicKey
}

// GenerateKeyPair generates a new Ed25519 key pair
func GenerateKeyPair() (*KeyPair, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	return &KeyPair{
		PrivateKey: signer,
		PublicKey:  signer.PublicKey(),
	}, nil
}

// ToGliderLabsSigner converts a gossh.Signer to gliderlabs/ssh.Signer
func ToGliderLabsSigner(s gossh.Signer) ssh.Signer {
	return s
}
