package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/jj-attaq/synth-stream/internal/protocol"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: client <username>")
		os.Exit(1)
	}
	username := os.Args[1]

	conn, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()

	// Send username as handshake
	if err := protocol.WriteMessage(conn, protocol.TypeText, []byte(username)); err != nil {
		log.Fatalf("could not send username: %v", err)
	}

	// Read incoming messages in background
	go func() {
		for {
			packet, err := protocol.ReadMessage(conn)
			if err != nil {
				fmt.Println("\ndisconnected from server")
				os.Exit(0)
			}
			fmt.Printf("\r%s\n> ", string(packet.Payload))
		}
	}()

	// Read from stdin, send each line as TypeText
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		if err := protocol.WriteMessage(conn, protocol.TypeText, scanner.Bytes()); err != nil {
			log.Printf("could not send message")
			return
		}
		fmt.Print("> ")
	}
}
