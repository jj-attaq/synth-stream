package main

import (
	"bufio"
	"bytes"
	"context"
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

func connect(token, address string,
	inPortNumber int,
	send func([]byte) error,
	sessionCode string,
	stdinCh <-chan string) (string, error) {
	//

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
		if err := c.SessionSetup(stdinCh); err != nil {
			return "", err
		}
	} else {
		if err := c.Rejoin(sessionCode); err != nil {
			return "", err
		}
	}
	/////////////////////
	dcReady := make(chan func([]byte) error, 1)
	webrtcErr := make(chan error, 1)
	var dcSend func([]byte) error

	// go func() {
	// 	if err := c.StartWebRTC(func(dcSend func([]byte) error) {
	// 		dcReady <- dcSend
	// 	}, func() {
	// 		dcSend = c.SendMidi
	// 		log.Println("WebRTC failed — falling back to TCP relay")
	// 	}); err != nil {
	// 		webrtcErr <- err
	// 	}
	// }()
	onReady := func(send func([]byte) error) {
		dcReady <- send
	}
	onFailed := func() {
		dcSend = c.SendMidi
		log.Println("WebRTC failed — falling back to TCP relay")
	}

	go func() {
		if err := c.StartWebRTC(onReady, onFailed); err != nil {
			webrtcErr <- err
		}
	}()

	// Start ReadMessages first — Ping() depends on it routing the echo to pingCh.

	// readDone is connect(...)'s exit condition
	readDone := make(chan error, 1)
	go func() {
		readDone <- c.ReadMessages()
	}()

	select {
	case dcSend = <-dcReady:
		log.Println("P2P connected — you can play")
		// start CaptureInput here, using dcSend directly
	case err := <-webrtcErr:
		return "", fmt.Errorf("WebRTC failed: %w", err)
	case <-time.After(15 * time.Second):
		dcSend = c.SendMidi
		log.Println("WebRTC timeout — falling back to TCP relay")
	}

	stop, err := midi.CaptureInput(inPortNumber, func(data []byte) {
		if err := send(data); err != nil {
			log.Printf("midi local playback error: %v", err)
		}
		if err := dcSend(data); err != nil {
			log.Printf("midi network send error: %v", err)
		}
	})
	if err != nil {
		return "", err
	}
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go c.ChatLoop(ctx, stdinCh)

	ping, err := c.Ping()
	if err != nil {
		log.Printf("ping error: %v", err)
	} else {
		log.Printf("Ping round trip time: %v", ping)
	}
	fmt.Print("> ")

	return c.SessionCode(), <-readDone
}

// make sure to close the client in connect()
func setup(token, address, sessionCode string, stdinCh <-chan string) (*client.Client, error) {
	c, err := client.New(token, address)
	if err != nil {
		return nil, err
	}
	// defer c.Close()

	if err := c.Handshake(); err != nil {
		return nil, err
	}

	if sessionCode == "" {
		if err := c.SessionSetup(stdinCh); err != nil {
			return nil, err
		}
	} else {
		if err := c.Rejoin(sessionCode); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func negotiateP2P(c *client.Client, send func([]byte) error) (func([]byte) error, error)

func startMIDI(inPortNumber int, send, dcSend func([]byte) error) (func(), error)

func run(c *client.Client, stdinCh <-chan string) error

func main() {
	scanner := bufio.NewScanner(os.Stdin)

	// Auth — read synchronously before handing stdin to the channel.
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

	// Hand stdin to a single goroutine. All session/chat reads go through this channel.
	// Only one goroutine ever reads from os.Stdin, eliminating scanner races on reconnect.

	stdinCh := readStdin(scanner)
	// stdinCh := make(chan string)
	// go func() {
	// 	for scanner.Scan() {
	// 		stdinCh <- scanner.Text()
	// 	}
	// 	close(stdinCh)
	// }()

	// Server connection
	var sessionCode string
	for attempts := range 3 {
		if attempts > 0 {
			time.Sleep(2 * time.Second)
			fmt.Printf("reconnecting (attempt %d/3)...\n", attempts+1)
		}
		code, err := connect(token, "localhost:8080", inPortNumber, send, sessionCode, stdinCh)
		sessionCode = code
		if err == nil {
			break
		}
		log.Printf("disconnected: %v", err)
	}
}

func readStdin(scanner *bufio.Scanner) <-chan string {
	ch := make(chan string)
	go func() {
		for scanner.Scan() {
			ch <- scanner.Text()
		}
		close(ch)
	}()
	return ch
}
