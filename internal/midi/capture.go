package midi

import (
	"fmt"

	gomidi "gitlab.com/gomidi/midi/v2"
)

// CaptureInput opens a MIDI input port and listens for messages.
// Returns a stop function to halt listening.
func CaptureInput(portNumber int) (func(), error) {
	inPort, err := gomidi.InPort(portNumber)
	if err != nil {
		return nil, fmt.Errorf("could not open input port %d: %w", portNumber, err)
	}

	stop, err := gomidi.ListenTo(inPort, func(msg gomidi.Message, timestampms int32) {
		if msg.Is(gomidi.RealTimeMsg) {
			return
		}
		var channel, key, velocity uint8
		if msg.GetNoteStart(&channel, &key, &velocity) {
			fmt.Printf("  Channel:  %3d\n  Key:      %3d\n  Velocity: %3d\n\n", channel, key, velocity)
		}
		if msg.GetNoteEnd(&channel, &key) {
			fmt.Printf("  Channel:  %3d\n  Key:      %3d\n\n", channel, key)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("could not listen to port %d: %w", portNumber, err)
	}

	fmt.Printf("Listening on: %s\n", inPort)
	return stop, nil
}
