package midi

import (
	"fmt"

	gomidi "gitlab.com/gomidi/midi/v2"
)

// OpenOutput opens a MIDI output port and returns a send function.
// The send function accepts raw MIDI bytes and writes them to the output device.
func OpenOutput(portNumber int) (func([]byte) error, error) {
	outPort, err := gomidi.OutPort(portNumber)
	if err != nil {
		return nil, fmt.Errorf("could not open output port %d: %w", portNumber, err)
	}

	sender, err := gomidi.SendTo(outPort)
	if err != nil {
		return nil, fmt.Errorf("could not open sender for port %d: %w", portNumber, err)
	}

	fmt.Printf("Output open: %s\n", outPort)
	return func(data []byte) error {
		return sender(gomidi.Message(data))
	}, nil
}
