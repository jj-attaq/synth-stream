package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/jj-attaq/synth-stream/internal/midi"
	"github.com/jj-attaq/synth-stream/internal/protocol"

	gomidi "gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: client <username>")
		os.Exit(1)
	}
	username := os.Args[1]

	defer gomidi.CloseDriver()
	midi.PrintDevices()

	// Device selection
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Select input device number: ")
	scanner.Scan()
	portNumber, err := strconv.Atoi(scanner.Text())
	if err != nil {
		log.Fatalf("invalid port number: %v", err)
	}

	stop, err := midi.CaptureInput(portNumber)
	if err != nil {
		log.Fatalf("could not start capture: %v", err)
	}
	defer stop()

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
	fmt.Print("> ")
	for scanner.Scan() {
		if err := protocol.WriteMessage(conn, protocol.TypeText, scanner.Bytes()); err != nil {
			log.Printf("could not send message")
			return
		}
		fmt.Print("> ")
	}
}
