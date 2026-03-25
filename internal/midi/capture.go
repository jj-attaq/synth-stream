package midi

import (
	"fmt"
	"log"

	gomidi "gitlab.com/gomidi/midi/v2"
)

// CaptureInput opens a MIDI input port and listens for messages.
// Calls onMessage with raw MIDI bytes for each incoming message.
// Returns a stop function to halt listening.
func CaptureInput(portNumber int, onMessage func([]byte)) (func(), error) {
	inPort, err := gomidi.InPort(portNumber)
	if err != nil {
		return nil, fmt.Errorf("could not open input port %d: %w", portNumber, err)
	}

	stop, err := gomidi.ListenTo(inPort, func(msg gomidi.Message, timestampms int32) {
		if msg.Is(gomidi.RealTimeMsg) {
			return
		}
		onMessage(msg.Bytes())
	})
	if err != nil {
		return nil, fmt.Errorf("could not listen to port %d: %w", portNumber, err)
	}

	log.Printf("Listening on: %s\n", inPort)
	return stop, nil
}
