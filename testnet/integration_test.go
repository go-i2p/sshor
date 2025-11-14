package testnet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-i2p/sshor/testnet/infrastructure"
	"github.com/go-i2p/sshor/testnet/services"
)

const (
	testTimeout = 60 * time.Second
)

// TestTestnetIntegration is the main integration test
func TestTestnetIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create testnet with 20 servers
	// Distribution: 5 TCP Echo, 5 HTTP Hello, 5 UDP Echo, 5 JSON-RPC
	distribution := map[infrastructure.ServiceType]int{
		infrastructure.TCPEcho:     5,
		infrastructure.HTTPHello:   5,
		infrastructure.UDPEcho:     5,
		infrastructure.JSONRPCPing: 5,
	}

	testnet, err := infrastructure.NewTestnet(20, distribution)
	if err != nil {
		t.Fatalf("Failed to create testnet: %v", err)
	}

	// Start all SSH servers
	if err := testnet.StartServers(ctx); err != nil {
		t.Fatalf("Failed to start servers: %v", err)
	}
	defer testnet.Shutdown()

	t.Logf("Started 20 SSH servers successfully")

	// Start services on each server
	serviceRunners := startAllServices(t, testnet)
	defer stopAllServices(serviceRunners)

	t.Logf("Started all services successfully")

	// Run tests
	t.Run("DirectConnections", func(t *testing.T) {
		testDirectConnections(t, testnet)
	})

	t.Run("OnionRouting", func(t *testing.T) {
		testOnionRouting(t, ctx, testnet)
	})
}

// serviceRunner manages a running service
type serviceRunner struct {
	service interface{ Close() error }
	errChan chan error
}

func startAllServices(t *testing.T, tn *infrastructure.Testnet) []*serviceRunner {
	runners := make([]*serviceRunner, 0, len(tn.Configs))

	for _, config := range tn.Configs {
		var runner *serviceRunner

		switch config.ServiceType {
		case infrastructure.TCPEcho:
			runner = startTCPEcho(t, config)
		case infrastructure.HTTPHello:
			runner = startHTTPHello(t, config)
		case infrastructure.UDPEcho:
			runner = startUDPEcho(t, config)
		case infrastructure.JSONRPCPing:
			runner = startJSONRPCPing(t, config)
		}

		if runner != nil {
			runners = append(runners, runner)
		}
	}

	return runners
}

func startTCPEcho(t *testing.T, config *infrastructure.ServerConfig) *serviceRunner {
	listener, err := net.Listen("tcp", config.ServiceAddr)
	if err != nil {
		t.Fatalf("Failed to create TCP listener for server %d: %v", config.Index, err)
	}

	service := services.NewTCPEchoService(listener)
	runner := &serviceRunner{
		service: service,
		errChan: make(chan error, 1),
	}

	go func() {
		runner.errChan <- service.Serve()
	}()

	t.Logf("Started TCP Echo service %d at %s", config.Index, config.ServiceAddr)
	return runner
}

func startHTTPHello(t *testing.T, config *infrastructure.ServerConfig) *serviceRunner {
	listener, err := net.Listen("tcp", config.ServiceAddr)
	if err != nil {
		t.Fatalf("Failed to create HTTP listener for server %d: %v", config.Index, err)
	}

	service := services.NewHTTPHelloService(listener)
	runner := &serviceRunner{
		service: service,
		errChan: make(chan error, 1),
	}

	go func() {
		runner.errChan <- service.Serve()
	}()

	t.Logf("Started HTTP Hello service %d at %s", config.Index, config.ServiceAddr)
	return runner
}

func startUDPEcho(t *testing.T, config *infrastructure.ServerConfig) *serviceRunner {
	conn, err := net.ListenPacket("udp", config.ServiceAddr)
	if err != nil {
		t.Fatalf("Failed to create UDP listener for server %d: %v", config.Index, err)
	}

	service := services.NewUDPEchoService(conn)
	runner := &serviceRunner{
		service: service,
		errChan: make(chan error, 1),
	}

	go func() {
		runner.errChan <- service.Serve()
	}()

	t.Logf("Started UDP Echo service %d at %s", config.Index, config.ServiceAddr)
	return runner
}

func startJSONRPCPing(t *testing.T, config *infrastructure.ServerConfig) *serviceRunner {
	listener, err := net.Listen("tcp", config.ServiceAddr)
	if err != nil {
		t.Fatalf("Failed to create JSON-RPC listener for server %d: %v", config.Index, err)
	}

	service := services.NewJSONRPCPingService(listener)
	runner := &serviceRunner{
		service: service,
		errChan: make(chan error, 1),
	}

	go func() {
		runner.errChan <- service.Serve()
	}()

	t.Logf("Started JSON-RPC PING service %d at %s", config.Index, config.ServiceAddr)
	return runner
}

func stopAllServices(runners []*serviceRunner) {
	for _, runner := range runners {
		runner.service.Close()
	}
}

func testDirectConnections(t *testing.T, tn *infrastructure.Testnet) {
	// Test one of each service type directly
	tests := []struct {
		name        string
		serviceType infrastructure.ServiceType
	}{
		{"TCP Echo", infrastructure.TCPEcho},
		{"HTTP Hello", infrastructure.HTTPHello},
		{"UDP Echo", infrastructure.UDPEcho},
		{"JSON-RPC PING", infrastructure.JSONRPCPing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find first server with this service type
			var config *infrastructure.ServerConfig
			for _, c := range tn.Configs {
				if c.ServiceType == tt.serviceType {
					config = c
					break
				}
			}

			if config == nil {
				t.Fatalf("No server found with service type %s", tt.name)
			}

			switch tt.serviceType {
			case infrastructure.TCPEcho:
				testDirectTCPEcho(t, config.ServiceAddr)
			case infrastructure.HTTPHello:
				testDirectHTTPHello(t, config.ServiceAddr)
			case infrastructure.UDPEcho:
				testDirectUDPEcho(t, config.ServiceAddr)
			case infrastructure.JSONRPCPing:
				testDirectJSONRPC(t, config.ServiceAddr)
			}
		})
	}
}

func testDirectTCPEcho(t *testing.T, addr string) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	testData := "Hello, TCP Echo!"

	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	buf := make([]byte, len(testData))
	n, err := io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if n != len(testData) {
		t.Fatalf("Expected %d bytes, got %d", len(testData), n)
	}

	if string(buf) != testData {
		t.Fatalf("Expected %q, got %q", testData, string(buf))
	}

	t.Logf("TCP Echo test passed: %q", string(buf))
}

func testDirectHTTPHello(t *testing.T, addr string) {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(fmt.Sprintf("http://%s/", addr))
	if err != nil {
		t.Fatalf("Failed to make HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.Contains(string(body), "Hello World") {
		t.Fatalf("Expected 'Hello World' in response, got: %q", string(body))
	}

	t.Logf("HTTP Hello test passed: %q", string(body))
}

func testDirectUDPEcho(t *testing.T, addr string) {
	conn, err := net.DialTimeout("udp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	testData := []byte("Hello, UDP!")

	if _, err := conn.Write(testData); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if !bytes.Equal(buf[:n], testData) {
		t.Fatalf("Expected %q, got %q", testData, buf[:n])
	}

	t.Logf("UDP Echo test passed: %q", buf[:n])
}

func testDirectJSONRPC(t *testing.T, addr string) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "ping",
		"id":      1,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(request); err != nil {
		t.Fatalf("Failed to encode request: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	decoder := json.NewDecoder(conn)
	var response map[string]interface{}
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected result object, got: %v", response)
	}

	status, ok := result["status"].(string)
	if !ok || status != "pong" {
		t.Fatalf("Expected status 'pong', got: %v", result)
	}

	t.Logf("JSON-RPC PING test passed: %v", result)
}

func testOnionRouting(t *testing.T, ctx context.Context, tn *infrastructure.Testnet) {
	// Test multi-hop routing through different combinations
	hopConfigs := []struct {
		name string
		hops []int
	}{
		{"3-hop route", []int{0, 5, 10}},
		{"4-hop route", []int{1, 6, 11, 15}},
		{"5-hop route", []int{2, 7, 12, 16, 18}},
	}

	for _, hc := range hopConfigs {
		t.Run(hc.name, func(t *testing.T) {
			// Test each service type through this route
			testOnionRouteTCPEcho(t, ctx, tn, hc.hops)
			testOnionRouteHTTPHello(t, ctx, tn, hc.hops)
			testOnionRouteUDPEcho(t, ctx, tn, hc.hops)
			testOnionRouteJSONRPC(t, ctx, tn, hc.hops)
		})
	}
}

func testOnionRouteTCPEcho(t *testing.T, ctx context.Context, tn *infrastructure.Testnet, hops []int) {
	// Find a TCP Echo service
	var targetConfig *infrastructure.ServerConfig
	for _, c := range tn.Configs {
		if c.ServiceType == infrastructure.TCPEcho {
			// Make sure it's not in the hop path
			inPath := false
			for _, hop := range hops {
				if hop == c.Index {
					inPath = true
					break
				}
			}
			if !inPath {
				targetConfig = c
				break
			}
		}
	}

	if targetConfig == nil {
		t.Skip("No suitable TCP Echo target found")
		return
	}

	router, err := tn.GetRouter(hops)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	if err := router.Start(ctx); err != nil {
		t.Fatalf("Failed to start router: %v", err)
	}
	defer router.Close()

	// Connect through the onion route
	conn, err := router.Dial("tcp", targetConfig.ServiceAddr)
	if err != nil {
		t.Fatalf("Failed to dial through onion route: %v", err)
	}
	defer conn.Close()

	testData := fmt.Sprintf("Onion routed TCP test through %d hops!", len(hops))

	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	buf := make([]byte, len(testData))
	n, err := io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if string(buf[:n]) != testData {
		t.Fatalf("Expected %q, got %q", testData, string(buf[:n]))
	}

	t.Logf("Onion TCP Echo (%d hops) test passed: %q", len(hops), string(buf))
}

func testOnionRouteHTTPHello(t *testing.T, ctx context.Context, tn *infrastructure.Testnet, hops []int) {
	var targetConfig *infrastructure.ServerConfig
	for _, c := range tn.Configs {
		if c.ServiceType == infrastructure.HTTPHello {
			inPath := false
			for _, hop := range hops {
				if hop == c.Index {
					inPath = true
					break
				}
			}
			if !inPath {
				targetConfig = c
				break
			}
		}
	}

	if targetConfig == nil {
		t.Skip("No suitable HTTP Hello target found")
		return
	}

	router, err := tn.GetRouter(hops)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	if err := router.Start(ctx); err != nil {
		t.Fatalf("Failed to start router: %v", err)
	}
	defer router.Close()

	// Create HTTP client that uses the onion route
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return router.Dial(network, addr)
			},
		},
	}

	resp, err := client.Get(fmt.Sprintf("http://%s/", targetConfig.ServiceAddr))
	if err != nil {
		t.Fatalf("Failed to make HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.Contains(string(body), "Hello World") {
		t.Fatalf("Expected 'Hello World' in response, got: %q", string(body))
	}

	t.Logf("Onion HTTP Hello (%d hops) test passed: %q", len(hops), string(body))
}

func testOnionRouteUDPEcho(t *testing.T, ctx context.Context, tn *infrastructure.Testnet, hops []int) {
	var targetConfig *infrastructure.ServerConfig
	for _, c := range tn.Configs {
		if c.ServiceType == infrastructure.UDPEcho {
			inPath := false
			for _, hop := range hops {
				if hop == c.Index {
					inPath = true
					break
				}
			}
			if !inPath {
				targetConfig = c
				break
			}
		}
	}

	if targetConfig == nil {
		t.Skip("No suitable UDP Echo target found")
		return
	}

	router, err := tn.GetRouter(hops)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	if err := router.Start(ctx); err != nil {
		t.Fatalf("Failed to start router: %v", err)
	}
	defer router.Close()

	// Get PacketConn through onion route
	// First bind to local address
	localAddr, err := infrastructure.GetFreeUDPPort()
	if err != nil {
		t.Fatalf("Failed to get free UDP port: %v", err)
	}

	pc, err := router.ListenPacket("udp", localAddr)
	if err != nil {
		t.Fatalf("Failed to create packet conn: %v", err)
	}
	defer pc.Close()

	testData := []byte(fmt.Sprintf("UDP onion test %d hops", len(hops)))

	targetAddr, err := net.ResolveUDPAddr("udp", targetConfig.ServiceAddr)
	if err != nil {
		t.Fatalf("Failed to resolve target address: %v", err)
	}

	// Send packet
	if _, err := pc.WriteTo(testData, targetAddr); err != nil {
		t.Fatalf("Failed to write packet: %v", err)
	}

	pc.SetReadDeadline(time.Now().Add(10 * time.Second))

	buf := make([]byte, 1024)
	n, addr, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("Failed to read packet: %v", err)
	}

	if !bytes.Equal(buf[:n], testData) {
		t.Fatalf("Expected %q, got %q", testData, buf[:n])
	}

	t.Logf("Onion UDP Echo (%d hops) test passed from %s: %q", len(hops), addr, buf[:n])
}

func testOnionRouteJSONRPC(t *testing.T, ctx context.Context, tn *infrastructure.Testnet, hops []int) {
	var targetConfig *infrastructure.ServerConfig
	for _, c := range tn.Configs {
		if c.ServiceType == infrastructure.JSONRPCPing {
			inPath := false
			for _, hop := range hops {
				if hop == c.Index {
					inPath = true
					break
				}
			}
			if !inPath {
				targetConfig = c
				break
			}
		}
	}

	if targetConfig == nil {
		t.Skip("No suitable JSON-RPC target found")
		return
	}

	router, err := tn.GetRouter(hops)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	if err := router.Start(ctx); err != nil {
		t.Fatalf("Failed to start router: %v", err)
	}
	defer router.Close()

	conn, err := router.Dial("tcp", targetConfig.ServiceAddr)
	if err != nil {
		t.Fatalf("Failed to dial through onion route: %v", err)
	}
	defer conn.Close()

	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "status",
		"id":      1,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(request); err != nil {
		t.Fatalf("Failed to encode request: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	decoder := json.NewDecoder(conn)
	var response map[string]interface{}
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected result object, got: %v", response)
	}

	status, ok := result["status"].(string)
	if !ok || status != "running" {
		t.Fatalf("Expected status 'running', got: %v", result)
	}

	t.Logf("Onion JSON-RPC (%d hops) test passed: %v", len(hops), result)
}

// TestConcurrentOnionRoutes tests multiple simultaneous onion routes
func TestConcurrentOnionRoutes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	distribution := map[infrastructure.ServiceType]int{
		infrastructure.TCPEcho:     5,
		infrastructure.HTTPHello:   5,
		infrastructure.UDPEcho:     5,
		infrastructure.JSONRPCPing: 5,
	}

	testnet, err := infrastructure.NewTestnet(20, distribution)
	if err != nil {
		t.Fatalf("Failed to create testnet: %v", err)
	}

	if err := testnet.StartServers(ctx); err != nil {
		t.Fatalf("Failed to start servers: %v", err)
	}
	defer testnet.Shutdown()

	serviceRunners := startAllServices(t, testnet)
	defer stopAllServices(serviceRunners)

	// Run 10 concurrent onion routes
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(routeNum int) {
			defer wg.Done()

			// Create unique hop path
			hops := []int{routeNum % 5, (routeNum + 5) % 20, (routeNum + 10) % 20}

			router, err := testnet.GetRouter(hops)
			if err != nil {
				errors <- fmt.Errorf("route %d: failed to create router: %w", routeNum, err)
				return
			}

			if err := router.Start(ctx); err != nil {
				errors <- fmt.Errorf("route %d: failed to start router: %w", routeNum, err)
				return
			}
			defer router.Close()

			// Find TCP Echo service not in the hop path
			var targetAddr string
			for _, c := range testnet.Configs {
				if c.ServiceType == infrastructure.TCPEcho {
					inPath := false
					for _, h := range hops {
						if h == c.Index {
							inPath = true
							break
						}
					}
					if !inPath {
						targetAddr = c.ServiceAddr
						break
					}
				}
			}

			if targetAddr == "" {
				errors <- fmt.Errorf("route %d: no suitable target found", routeNum)
				return
			}

			conn, err := router.Dial("tcp", targetAddr)
			if err != nil {
				errors <- fmt.Errorf("route %d: failed to dial: %w", routeNum, err)
				return
			}
			defer conn.Close()

			testData := fmt.Sprintf("Concurrent route %d", routeNum)
			if _, err := conn.Write([]byte(testData)); err != nil {
				errors <- fmt.Errorf("route %d: write failed: %w", routeNum, err)
				return
			}

			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			buf := make([]byte, len(testData))
			if _, err := io.ReadFull(conn, buf); err != nil {
				errors <- fmt.Errorf("route %d: read failed: %w", routeNum, err)
				return
			}

			if string(buf) != testData {
				errors <- fmt.Errorf("route %d: data mismatch", routeNum)
				return
			}

			t.Logf("Concurrent route %d completed successfully", routeNum)
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// TestDirectSSHForwarding tests direct SSH port forwarding without sshor
func TestDirectSSHForwarding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	distribution := map[infrastructure.ServiceType]int{
		infrastructure.TCPEcho: 2,
	}

	testnet, err := infrastructure.NewTestnet(2, distribution)
	if err != nil {
		t.Fatalf("Failed to create testnet: %v", err)
	}

	if err := testnet.StartServers(ctx); err != nil {
		t.Fatalf("Failed to start servers: %v", err)
	}
	defer testnet.Shutdown()

	serviceRunners := startAllServices(t, testnet)
	defer stopAllServices(serviceRunners)

	// Create direct SSH client to server 0
	client, err := testnet.CreateDirectSSHClient(0)
	if err != nil {
		t.Fatalf("Failed to create SSH client: %v", err)
	}
	defer client.Close()

	// Forward to TCP Echo service on server 1
	targetAddr := testnet.Configs[1].ServiceAddr

	conn, err := client.Dial("tcp", targetAddr)
	if err != nil {
		t.Fatalf("Failed to dial through SSH: %v", err)
	}
	defer conn.Close()

	testData := "Direct SSH forwarding test"
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	buf := make([]byte, len(testData))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if string(buf) != testData {
		t.Fatalf("Expected %q, got %q", testData, string(buf))
	}

	t.Logf("Direct SSH forwarding test passed")
}
