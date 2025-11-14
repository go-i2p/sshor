package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-i2p/sshor"
)

func main() {
	// Configure the router
	config := sshor.Config{
		Hops: []sshor.SSHHop{
			{
				Address: "hop1.example.com:22",
				User:    "user1",
				// Load PrivateKey and HostKey from files
			},
			{
				Address: "hop2.example.com:22",
				User:    "user2",
				// Load PrivateKey and HostKey from files
			},
		},
		ServerAddr: "127.0.0.1:2222",
		// Load ServerKey from file
		Timeout: 30 * time.Second,
	}

	router := sshor.NewRouter(config)

	ctx := context.Background()
	if err := router.Start(ctx); err != nil {
		log.Fatal("Failed to start router:", err)
	}
	defer router.Close()

	// Use as standard net.Conn
	conn, err := router.Dial("tcp", "example.com:80")
	if err != nil {
		log.Fatal("Dial failed:", err)
	}
	defer conn.Close()

	// Use as standard net.Listener
	listener, err := router.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		log.Fatal("Listen failed:", err)
	}
	defer listener.Close()

	// Use as standard net.PacketConn
	packetConn, err := router.ListenPacket("udp", "127.0.0.1:9090")
	if err != nil {
		log.Fatal("ListenPacket failed:", err)
	}
	defer packetConn.Close()

	fmt.Println("Onion router started with standard net interfaces")
	select {} // Keep running
}
