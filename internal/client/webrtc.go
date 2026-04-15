package client

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/jj-attaq/synth-stream/internal/protocol"
	"github.com/pion/webrtc/v4"
)

// StartWebRTC negotiates a WebRTC DataChannel with the paired partner using the
// existing TCP connection as the signaling channel. Once the DataChannel is open,
// onReady is called with a send function that routes MIDI bytes directly peer-to-peer,
// bypassing the TCP relay server.
func (c *Client) StartWebRTC(onReady func(send func([]byte) error)) (retErr error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		// ICETransportPolicy: webrtc.ICETransportPolicyRelay,
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{
				URLs:       []string{"turn:global.relay.metered.ca:80"},
				Username:   os.Getenv("TURN_USER"),
				Credential: os.Getenv("TURN_PASS"),
			},
			{
				URLs:       []string{"turn:global.relay.metered.ca:80?transport=tcp"},
				Username:   os.Getenv("TURN_USER"),
				Credential: os.Getenv("TURN_PASS"),
			},
			{
				URLs:       []string{"turn:global.relay.metered.ca:443"},
				Username:   os.Getenv("TURN_USER"),
				Credential: os.Getenv("TURN_PASS"),
			},
			{
				URLs:       []string{"turns:global.relay.metered.ca:443?transport=tcp"},
				Username:   os.Getenv("TURN_USER"),
				Credential: os.Getenv("TURN_PASS"),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("create peer connection: %w", err)
	}
	defer func() {
		if retErr != nil {
			pc.Close()
		}
	}()

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		// if state == webrtc.PeerConnectionStateFailed {
		// 	onFailed()
		// }
		log.Printf("WebRTC: %s", state)
	})

	// setupMidiChannel wires OnOpen and OnMessage onto the MIDI DataChannel.
	// Called by both paths: offerer passes dc from CreateDataChannel,
	// answerer passes dc received in OnDataChannel.
	setupMidiChannel := func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			onReady(func(data []byte) error {
				return dc.Send(data)
			})
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			log.Printf("MIDI received via DataChannel: %d bytes %v", len(msg.Data), msg.Data)
			if c.midiOutput != nil {
				if err := c.midiOutput(msg.Data); err != nil {
					log.Printf("p2p midi playback error: %v", err)
				}
			}
		})
	}

	// OnOpen: store dc.Send in c.chatSend so ChatLoop uses it instead of TCP
	// OnMessage: print the received message (same format as ReadMessages TypeText: "\r%s\n> ")
	setupChatChannel := func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			c.mu.Lock()
			c.chatSend = dc.Send
			c.mu.Unlock()
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			fmt.Printf("\r%s\n> ", msg.Data)
		})
	}

	// Helper — marshal LocalDescription and send it over TCP:
	sendSDP := func(msgType byte) error {
		sdp, err := json.Marshal(pc.LocalDescription())
		if err != nil {
			return err
		}
		return protocol.WriteMessage(c.conn, msgType, sdp)
	}
	//
	// Helper — wait on sigCh for the next signal packet and unmarshal it:
	recvSDP := func() (webrtc.SessionDescription, error) {
		packet := <-c.sigCh
		var desc webrtc.SessionDescription
		return desc, json.Unmarshal(packet.Payload, &desc)
	}
	//
	// Offerer path  (c.IsOfferer() == true):
	if c.IsOfferer() {
		// COULD use true, but if a packet is lost, the channel waits for the lost packet, this is better for live use.

		//   1. CreateDataChannel("midi", &webrtc.DataChannelInit{Ordered: &false})
		ordered := false
		midiDC, err := pc.CreateDataChannel("midi", &webrtc.DataChannelInit{Ordered: &ordered})
		if err != nil {
			return fmt.Errorf("CreateDataChannel midi: %w", err)
		}
		setupMidiChannel(midiDC)

		chatDC, err := pc.CreateDataChannel("chat", nil)
		if err != nil {
			return fmt.Errorf("CreateDataChannel chat: %w", err)
		}
		setupChatChannel(chatDC)

		//   3. CreateOffer -> SetLocalDescription -> GatheringCompletePromise
		offer, err := pc.CreateOffer(nil)
		if err != nil {
			return fmt.Errorf("CreateOffer: %w", err)
		}
		if err := pc.SetLocalDescription(offer); err != nil {
			return fmt.Errorf("SetLocalDescription: %w", err)
		}
		// blocks until gathering is complete
		<-webrtc.GatheringCompletePromise(pc)

		//   4. sendSDP(TypeSignalOffer)
		if err := sendSDP(protocol.TypeSignalOffer); err != nil {
			return fmt.Errorf("sendSDP: %w", err)
		}
		//   5. recvSDP() -> SetRemoteDescription
		answer, err := recvSDP()
		if err != nil {
			return fmt.Errorf("recvSDP: %w", err)
		}
		if err := pc.SetRemoteDescription(answer); err != nil {
			return fmt.Errorf("SetRemoteDescription: %w", err)
		}
	} else {
		// Answerer path (c.IsOfferer() == false):
		pc.OnDataChannel(func(dc *webrtc.DataChannel) {
			switch dc.Label() {
			case "midi":
				setupMidiChannel(dc)
			case "chat":
				setupChatChannel(dc)
			}
		})
		//   2. recvSDP() -> SetRemoteDescription
		offer, err := recvSDP()
		if err != nil {
			return fmt.Errorf("recvSDP: %w", err)
		}
		if err := pc.SetRemoteDescription(offer); err != nil {
			return fmt.Errorf("SetRemoteDescription: %w", err)
		}
		//   3. CreateAnswer -> SetLocalDescription -> GatheringCompletePromise
		answer, err := pc.CreateAnswer(nil)
		if err != nil {
			return fmt.Errorf("CreateAnswer: %w", err)
		}
		if err := pc.SetLocalDescription(answer); err != nil {
			return fmt.Errorf("SetLocalDescription: %w", err)
		}
		<-webrtc.GatheringCompletePromise(pc)
		//   4. sendSDP(TypeSignalAnswer)
		if err := sendSDP(protocol.TypeSignalAnswer); err != nil {
			return fmt.Errorf("sendSDP: %w", err)
		}
	}

	return nil
}
