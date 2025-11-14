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

```bash
sshor [options]
```

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
