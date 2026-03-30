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

	"github.com/joho/godotenv"
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
	localSend midi.MidiSender,
	sessionCode string,
	stdinCh <-chan string) (string, error) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c, err := dialAndPair(token, address, sessionCode, stdinCh)
	if err != nil {
		return "", err
	}
	defer c.Close()

	// SetMidiOutput must be called before negotiateP2P — ReadMessages starts inside
	// negotiateP2P and calls midiOutput when partner MIDI arrives. The localSend function
	// wraps midi.OpenOutput(), which routes incoming MIDI to the configured virtual
	// MIDI output port for the DAW to receive. Device selection happens in main()
	// before any network connection is established.
	c.SetMidiOutput(localSend)

	dcSend, connDone, err := negotiateP2P(c)
	if err != nil {
		return "", err
	}

	stop, err := startMIDI(inPortNumber, localSend, dcSend)
	if err != nil {
		return "", err
	}
	defer stop()

	beginChat(ctx, c, stdinCh)

	code := c.SessionCode()
	connErr := <-connDone
	if c.IsQuit() {
		return "", nil
	}
	return code, connErr
}

func dialAndPair(token, address, sessionCode string, stdinCh <-chan string) (*client.Client, error) {
	c, err := client.New(token, address)
	if err != nil {
		return nil, err
	}

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

func negotiateP2P(c *client.Client) (dcSend func([]byte) error, connDone <-chan error, err error) {
	dcReady := make(chan func([]byte) error, 1)
	webrtcErr := make(chan error, 1)

	onReady := func(dcSend func([]byte) error) {
		dcReady <- dcSend
	}
	// onFailed := func() {
	// 	dcSend = c.SendMidi
	// 	log.Println("WebRTC failed — falling back to TCP relay")
	// }

	go func() {
		if err := c.StartWebRTC(onReady); err != nil {
			webrtcErr <- err
		}
	}()

	ch := make(chan error, 1)
	connDone = ch
	go func() {
		ch <- c.ReadMessages()
	}()

	select {
	case dcSend = <-dcReady:
		log.Println("P2P connected — you can play")
	case err = <-webrtcErr:
		return nil, nil, fmt.Errorf("WebRTC failed: %w", err)
	case <-time.After(15 * time.Second):
		dcSend = c.SendMidi
		log.Println("WebRTC timeout — falling back to TCP relay")
	}

	return dcSend, connDone, nil
}

func startMIDI(inPortNumber int, localSend midi.MidiSender, dcSend func([]byte) error) (func(), error) {
	stop, err := midi.CaptureInput(inPortNumber, func(data []byte) {
		if err := localSend(data); err != nil {
			log.Printf("midi local playback error: %v", err)
		}
		if err := dcSend(data); err != nil {
			log.Printf("midi network send error: %v", err)
		}
	})
	if err != nil {
		return nil, err
	}

	return stop, nil
}

func beginChat(ctx context.Context, c *client.Client, stdinCh <-chan string) {
	ping, err := c.Ping()
	if err != nil {
		log.Printf("ping error: %v", err)
	} else {
		log.Printf("Ping round trip time: %v", ping)
	}

	go c.ChatLoop(ctx, stdinCh)
}

func main() {
	godotenv.Load()
	scanner := bufio.NewScanner(os.Stdin)

	// Auth — read synchronously before handing stdin to the channel.
	fmt.Print("Username: ")
	scanner.Scan()
	username := scanner.Text()

	fmt.Print("Password: ")
	scanner.Scan()
	password := scanner.Text()

	token, err := login(username, password, "http://localhost:"+os.Getenv("API_PORT"))
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

	localSend, err := midi.OpenOutput(outPortNumber)
	if err != nil {
		log.Fatalf("could not open output: %v", err)
	}

	// Hand stdin to a single goroutine. All session/chat reads go through this channel.
	// Only one goroutine ever reads from os.Stdin, eliminating scanner races on reconnect.
	stdinCh := readStdin(scanner)

	// Server connection
	var sessionCode string
	for attempts := range 3 {
		if attempts > 0 {
			time.Sleep(2 * time.Second)
			fmt.Printf("reconnecting (attempt %d/3)...\n", attempts+1)
		}
		code, err := connect(token, "localhost:"+os.Getenv("PORT"), inPortNumber, localSend, sessionCode, stdinCh)
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
