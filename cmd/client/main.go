package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/jj-attaq/synth-stream/internal/client"
	"github.com/jj-attaq/synth-stream/internal/midi"

	gomidi "gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: client <username>")
		os.Exit(1)
	}
	username := os.Args[1]

	// MIDI setup
	defer gomidi.CloseDriver()
	midi.PrintDevices()

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Print("Select input device number: ")
	scanner.Scan()
	inPortNumber, err := strconv.Atoi(scanner.Text())
	if err != nil {
		log.Fatalf("invalid port number: %v", err)
	}

	fmt.Print("Select output device number: ")
	scanner.Scan()
	outPortNumber, err := strconv.Atoi(scanner.Text())
	if err != nil {
		log.Fatalf("invalid port number: %v", err)
	}

	send, err := midi.OpenOutput(outPortNumber)
	if err != nil {
		log.Fatalf("could not open output: %v", err)
	}

	// Server connection
	c, err := client.New(username, "localhost:8080")
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer c.Close()

	c.SetMidiSend(send)

	if err := c.Handshake(); err != nil {
		log.Fatalf("handshake failed: %v", err)
	}

	stop, err := midi.CaptureInput(inPortNumber, func(data []byte) {
		if err := send(data); err != nil {
			log.Printf("midi local playback error: %v", err)
		}
		if err := c.SendMidi(data); err != nil {
			log.Printf("midi network send error: %v", err)
		}
	})
	if err != nil {
		log.Fatalf("could not start capture: %v", err)
	}
	defer stop()

	go c.ReadMessages()
	c.ChatLoop()
}
