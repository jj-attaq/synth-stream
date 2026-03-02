package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jj-attaq/synth-stream/internal/client"
	"github.com/jj-attaq/synth-stream/internal/midi"

	gomidi "gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

// login calls POST /login on the HTTP API and returns a JWT on success.
func login(username, password, apiAddress string) (string, error) {
	var result struct {
		Token string `json:"token"`
	}

	data, err := json.Marshal(struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{username, password})
	if err != nil {
		return "", fmt.Errorf("marshal failed: %w", err)
	}

	body := bytes.NewReader(data)
	resp, err := http.Post(apiAddress+"/login", "application/json", body)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed: status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return result.Token, nil
}

func connect(token, address string, inPortNumber int, send func([]byte) error, sessionCode string) (string, error) {
	c, err := client.New(token, address)
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
	scanner := bufio.NewScanner(os.Stdin)

	// Auth
	fmt.Print("Username: ")
	scanner.Scan()
	username := scanner.Text()

	fmt.Print("Password: ")
	scanner.Scan()
	password := scanner.Text()

	token, err := login(username, password, "http://localhost:8081")
	if err != nil {
		log.Fatalf("login failed: %v", err)
	}

	// MIDI setup
	defer gomidi.CloseDriver()
	midi.PrintDevices()

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
		code, err := connect(token, "localhost:8080", inPortNumber, send, sessionCode)
		sessionCode = code
		if err == nil {
			break
		}
		log.Printf("disconnected: %v", err)
	}
}
