package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/jj-attaq/synth-stream/internal/client"
	"github.com/jj-attaq/synth-stream/internal/midi"

	gomidi "gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

func connect(username, address string, inPortNumber int, send func([]byte) error, sessionCode string) (string, error) {
	c, err := client.New(username, address)
	if err != nil {
		return "", err
	}
	defer c.Close()

	c.SetMidiSend(send)

	if err := c.Handshake(); err != nil {
		return "", err
	}

	if sessionCode == "" {
		if err := c.SessionSetup(); err != nil {
			return "", err
		}
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
		return "", err
	}
	defer stop()

	go c.ChatLoop()

	ping, err := c.Ping()
	if err != nil {
		log.Printf("ping error: %v", err)
	}

	log.Printf("Ping round trip time: %v", ping)

	return c.SessionCode(), c.ReadMessages()
}

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
	var sessionCode string
	for attempts := range 3 {
		if attempts > 0 {
			time.Sleep(2 * time.Second)
			fmt.Printf("reconnecting (attempt %d/3)...\n", attempts+1)
		}
		code, err := connect(username, "localhost:8080", inPortNumber, send, sessionCode)
		sessionCode = code
		if err == nil {
			break
		}
		log.Printf("disconnected: %v", err)
	}
}
