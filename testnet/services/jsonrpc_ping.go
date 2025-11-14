package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// JSONRPCPingService implements a simple JSON-RPC PING API
type JSONRPCPingService struct {
	listener  net.Listener
	done      chan struct{}
	startTime time.Time
	reqCount  atomic.Uint64
	closed    bool
	mu        sync.Mutex
}

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id"`
}

type jsonrpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type pingResult struct {
	Status      string `json:"status"`
	Timestamp   int64  `json:"timestamp"`
	UptimeMs    int64  `json:"uptime_ms"`
	RequestNum  uint64 `json:"request_num"`
	ServiceAddr string `json:"service_addr"`
}

// NewJSONRPCPingService creates a new JSON-RPC PING service
func NewJSONRPCPingService(listener net.Listener) *JSONRPCPingService {
	return &JSONRPCPingService{
		listener:  listener,
		done:      make(chan struct{}),
		startTime: time.Now(),
	}
}

// Serve starts the JSON-RPC service
func (s *JSONRPCPingService) Serve() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
				log.Printf("JSON-RPC accept error: %v", err)
				return err
			}
		}

		go s.handleConnection(conn)
	}
}

func (s *JSONRPCPingService) handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req jsonrpcRequest
		if err := decoder.Decode(&req); err != nil {
			if err != io.EOF {
				log.Printf("JSON-RPC decode error: %v", err)
			}
			return
		}

		resp := s.handleRequest(&req)
		if err := encoder.Encode(resp); err != nil {
			log.Printf("JSON-RPC encode error: %v", err)
			return
		}
	}
}

func (s *JSONRPCPingService) handleRequest(req *jsonrpcRequest) *jsonrpcResponse {
	resp := &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	if req.JSONRPC != "2.0" {
		resp.Error = map[string]interface{}{
			"code":    -32600,
			"message": "Invalid Request",
		}
		return resp
	}

	switch req.Method {
	case "ping":
		reqNum := s.reqCount.Add(1)
		uptime := time.Since(s.startTime).Milliseconds()

		resp.Result = pingResult{
			Status:      "pong",
			Timestamp:   time.Now().Unix(),
			UptimeMs:    uptime,
			RequestNum:  reqNum,
			ServiceAddr: s.listener.Addr().String(),
		}

	case "status":
		reqNum := s.reqCount.Add(1)
		uptime := time.Since(s.startTime).Milliseconds()

		resp.Result = map[string]interface{}{
			"service":      "jsonrpc-ping",
			"status":       "running",
			"uptime_ms":    uptime,
			"total_reqs":   reqNum,
			"service_addr": s.listener.Addr().String(),
			"start_time":   s.startTime.Unix(),
		}

	default:
		resp.Error = map[string]interface{}{
			"code":    -32601,
			"message": fmt.Sprintf("Method not found: %s", req.Method),
		}
	}

	return resp
}

// Close shuts down the service
func (s *JSONRPCPingService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)
	return s.listener.Close()
}

// Addr returns the service address
func (s *JSONRPCPingService) Addr() net.Addr {
	return s.listener.Addr()
}
