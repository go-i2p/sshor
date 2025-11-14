# Testnet Integration Tests

This directory contains comprehensive integration tests that demonstrate end-to-end functionality of the sshor library using real SSH servers and network services.

## Overview

The testnet spins up 20 local SSH servers, each hosting a single service type. Tests verify both direct connections and multi-hop onion routing through these servers.

## Architecture

### Services (20 servers total)

- **5 TCP Echo Servers**: Echo back all received TCP data
- **5 HTTP Hello World Servers**: Return "Hello World" HTTP responses
- **3 UDP Echo Servers**: Echo back received UDP packets (direct connection only)
- **2 UDP Relay Servers**: TCP-to-UDP relay for testing UDP through onion routes
- **5 JSON-RPC PING Servers**: Return server status/uptime information

### Server Distribution

- SSH servers run on ports 10000-10019
- Services run on ports 20000-20019
- Each server has unique Ed25519 host and client keys
- All servers support SSH port forwarding for onion routing

## Running the Tests

### Run All Tests

```bash
cd testnet
go test -v -timeout 120s
```

### Run Specific Test Suites

```bash
# Direct connections only
go test -v -run TestTestnetIntegration/DirectConnections

# Onion routing only
go test -v -run TestTestnetIntegration/OnionRouting

# Concurrent routing tests
go test -v -run TestConcurrentOnionRoutes

# Direct SSH forwarding
go test -v -run TestDirectSSHForwarding
```

### Race Detection

```bash
go test -race -v -timeout 120s
```

## Test Coverage

### Direct Connection Tests

Tests verify each service type works correctly with direct TCP/UDP connections:

- **TCP Echo**: Sends data, verifies echo response
- **HTTP Hello**: Makes HTTP GET request, validates response
- **UDP Echo**: Sends UDP packet, verifies echo (direct connection)
- **JSON-RPC PING**: Sends JSON-RPC request, validates pong response

### Onion Routing Tests

Tests verify multi-hop routing through SSH tunnel chains:

- **3-hop routes**: Client → Hop1 → Hop2 → Hop3 → Service
- **4-hop routes**: Client → Hop1 → Hop2 → Hop3 → Hop4 → Service
- **5-hop routes**: Client → Hop1 → Hop2 → Hop3 → Hop4 → Hop5 → Service

Each hop configuration tests all service types that support onion routing:

- **TCP Echo**: Direct TCP forwarding through hops
- **HTTP**: Application-layer HTTP protocol over onion routes
- **UDP Relay**: TCP-to-UDP relay enabling UDP communication through onion routes
- **JSON-RPC**: Structured RPC protocol over onion routes

> **Note**: Pure UDP echo services are tested via direct connections only. UDP through onion routes requires a TCP-to-UDP relay service at the destination, which is demonstrated by the UDP Relay servers.

### Concurrent Routing Tests

Spawns 10 simultaneous onion routes with different hop paths to verify:

- No race conditions
- Proper connection isolation
- Resource cleanup
- Router independence

## What the Tests Demonstrate

### Real Network Implementation

- Uses only real `net.Conn` and `net.Listener` - no mocks
- Real SSH handshakes and authentication
- Actual TCP/UDP packet routing
- Real SSH port forwarding mechanisms

### Onion Routing Features

- **Chain Building**: Establishes SSH tunnels through multiple hops
- **TCP Forwarding**: Routes TCP connections through the chain
- **UDP Forwarding**: Symmetric UDP packet forwarding via TCP tunnels
- **Encryption**: Each hop adds/removes a layer of SSH encryption
- **Anonymity**: Intermediate hops can't see final destination

### Service Protocols

- **TCP Echo**: Simple bidirectional streaming
- **HTTP**: Application-layer protocol over onion routes
- **UDP Echo**: Direct packet-based communication
- **UDP Relay**: TCP-to-UDP relay for UDP over onion routes
- **JSON-RPC**: Structured RPC over onion routes

## Implementation Details

### Directory Structure

```
testnet/
├── README.md                 # This file
├── integration_test.go       # Main test suite
├── infrastructure/
│   ├── keys.go              # Ed25519 key generation
│   ├── ssh_server.go        # SSH server with forwarding
│   └── testnet.go           # Testnet orchestration
└── services/
    ├── tcp_echo.go          # TCP echo service
    ├── http_hello.go        # HTTP hello service
    ├── udp_echo.go          # UDP echo service
    ├── udp_relay.go         # TCP-to-UDP relay service
    └── jsonrpc_ping.go      # JSON-RPC ping service
```

### Key Components

**Infrastructure Package**:
- `KeyPair`: Ed25519 key generation and management
- `SSHServer`: Full SSH server with port forwarding support
- `Testnet`: Orchestrates 20 servers with different service types

**Services Package**:
- Each service implements `Serve()` and `Close()` methods
- Services use real `net.Listener` or `net.PacketConn`
- Proper goroutine management and resource cleanup

### Test Flow

1. **Setup**: Create testnet with 20 servers
2. **Start SSH Servers**: Each binds to unique port
3. **Start Services**: Each service type starts on its port
4. **Direct Tests**: Verify services work without routing
5. **Onion Tests**: Create routers with different hop counts
6. **Concurrent Tests**: Verify parallel routing
7. **Cleanup**: Shutdown all servers and services

## Expected Output

When tests run successfully, you'll see:

```
=== RUN   TestTestnetIntegration
Started SSH server 0 (TCP Echo) at 127.0.0.1:10000
Started SSH server 1 (TCP Echo) at 127.0.0.1:10001
...
Started SSH server 19 (JSON-RPC PING) at 127.0.0.1:10019
Started 20 SSH servers successfully
Started TCP Echo service 0 at 127.0.0.1:20000
...
Started all services successfully
=== RUN   TestTestnetIntegration/DirectConnections
=== RUN   TestTestnetIntegration/DirectConnections/TCP_Echo
TCP Echo test passed: "Hello, TCP Echo!"
=== RUN   TestTestnetIntegration/DirectConnections/HTTP_Hello
HTTP Hello test passed: "Hello World from 127.0.0.1:20005"
...
=== RUN   TestTestnetIntegration/OnionRouting
=== RUN   TestTestnetIntegration/OnionRouting/3-hop_route
Onion TCP Echo (3 hops) test passed: "Onion routed TCP test through 3 hops!"
...
PASS
ok      github.com/go-i2p/sshor/testnet    45.234s
```

## Troubleshooting

### Port Conflicts

If you see "address already in use" errors:
- Ensure no other processes are using ports 10000-10019 or 20000-20019
- Wait a few seconds between test runs for ports to be released

### Timeouts

If tests timeout:
- Increase test timeout: `go test -timeout 180s`
- Check system resources (too many open files, etc.)
- Verify SSH servers started successfully in logs

### Race Conditions

If race detector finds issues:
- Report as bug - should be no races
- Check goroutine cleanup in service shutdown

## Performance Notes

- Starting 20 SSH servers takes ~2-5 seconds
- Each onion route setup takes ~100-500ms
- UDP forwarding adds ~10-50ms latency per hop
- Total test suite runs in ~45-60 seconds

## Future Enhancements

Potential additions:
- [ ] Stress tests with thousands of concurrent connections
- [ ] Latency measurements for different hop counts
- [ ] Bandwidth tests
- [ ] Service failure/recovery scenarios
- [ ] Dynamic route reconfiguration
- [ ] More service types (SOCKS5, DNS, etc.)
