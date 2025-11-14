# sshor

A simple onion-routing implementation over SSH for educational purposes.

## Overview

`sshor` demonstrates onion routing concepts using SSH as the transport layer. This is an educational project and **not intended for production use**.

## Modules

- **Library (`go-i2p/sshor`)**: Core onion routing functionality
- **CLI (`go-i2p/sshor/sshor`)**: Command-line interface for running the onion router

## Installation

```bash
go get github.com/go-i2p/sshor
go install github.com/go-i2p/sshor/sshor@latest
```

## Usage

### Command Line Interface

```bash
sshor -server-key <path> -hops <hops> [options]
```

**Required Flags:**

- `-server-key <path>` - Path to server private key file
- `-hops <hops>` - Comma-separated list of hops in format `user@host:port`
  - Example: `user1@hop1.com:22,user2@hop2.com:22`

**Optional Flags:**

- `-server <address>` - Address for the embedded SSH server (default: `127.0.0.1:2222`)
- `-hop-keys <dir>` - Directory containing hop private keys (default: current directory)
  - Expected files: `id_rsa_hop1`, `id_rsa_hop2`, etc.
- `-hop-hostkeys <dir>` - Directory containing hop host public keys (default: current directory)
  - Expected files: `host_key_hop1.pub`, `host_key_hop2.pub`, etc.
- `-timeout <duration>` - Connection timeout (default: `30s`)
- `-help` - Show help message

**Example:**

```bash
sshor -server-key server.key -hops user@10.0.0.1:22,user@10.0.0.2:22
```

### Library Usage

See the [GoDoc documentation](https://pkg.go.dev/github.com/go-i2p/sshor) for library usage examples.

## Important Notes

### UDP Forwarding Behavior

The `ListenPacket()` implementation provides **symmetric (bidirectional) UDP forwarding**:

- **WriteTo()**: Sends packets **through the SSH tunnel chain** (encrypted and onion-routed)
- **ReadFrom()**: Receives packets **back through the SSH tunnels** (symmetric behavior)

This is achieved by spawning a read goroutine for each TCP tunnel that forwards responses back to the local UDP socket, enabling full bidirectional UDP communication without requiring SSH server modifications.

## Warning

This implementation is for educational purposes only. It is not secure for real-world applications.

## License

See LICENSE file for details.
