package midi

import (
	"fmt"

	gomidi "gitlab.com/gomidi/midi/v2"
)

type Device struct {
	Number int
	Name   string
}

func ListInputDevices() []Device {
	inPorts := gomidi.GetInPorts()
	devices := make([]Device, len(inPorts))
	for i, port := range inPorts {
		devices[i] = Device{
			Number: port.Number(),
			Name:   port.String(),
		}
	}
	return devices
}

func ListOutputDevices() []Device {
	outPorts := gomidi.GetOutPorts()
	devices := make([]Device, len(outPorts))
	for i, port := range outPorts {
		devices[i] = Device{
			Number: port.Number(),
			Name:   port.String(),
		}
	}
	return devices
}

// PrintDevices prints all available MIDI input and output devices to stdout.
func PrintDevices() {
	fmt.Println("MIDI Devices")
	inputDevices := ListInputDevices()
	outputDevices := ListOutputDevices()
	if len(inputDevices) == 0 {
		fmt.Println("No MIDI input devices detected")
	} else {
		fmt.Println("Inputs:")
		for _, d := range inputDevices {
			fmt.Printf("  [%d] %s\n", d.Number, d.Name)
		}
	}

	if len(outputDevices) == 0 {
		fmt.Println("No MIDI output devices detected")
	} else {
		fmt.Println("Outputs:")
		for _, d := range outputDevices {
			fmt.Printf("  [%d] %s\n", d.Number, d.Name)
		}
	}
}
