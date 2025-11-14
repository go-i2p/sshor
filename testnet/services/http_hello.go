package services

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
)

// HTTPHelloService implements a simple HTTP "Hello World" server
type HTTPHelloService struct {
	listener net.Listener
	server   *http.Server
	done     chan struct{}
	closed   bool
	mu       sync.Mutex
}

// NewHTTPHelloService creates a new HTTP hello world service
func NewHTTPHelloService(listener net.Listener) *HTTPHelloService {
	mux := http.NewServeMux()

	service := &HTTPHelloService{
		listener: listener,
		server: &http.Server{
			Handler: mux,
		},
		done: make(chan struct{}),
	}

	mux.HandleFunc("/", service.handleRoot)
	mux.HandleFunc("/health", service.handleHealth)

	return service
}

func (s *HTTPHelloService) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Hello World from %s", s.listener.Addr())
}

func (s *HTTPHelloService) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"healthy","service":"http-hello"}`)
}

// Serve starts the HTTP service
func (s *HTTPHelloService) Serve() error {
	err := s.server.Serve(s.listener)
	if err != nil && err != http.ErrServerClosed {
		log.Printf("HTTP hello serve error: %v", err)
		return err
	}
	return nil
}

// Close shuts down the service
func (s *HTTPHelloService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)
	return s.server.Close()
}

// Addr returns the service address
func (s *HTTPHelloService) Addr() net.Addr {
	return s.listener.Addr()
}
