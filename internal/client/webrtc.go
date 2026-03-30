package client

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/jj-attaq/synth-stream/internal/protocol"
	"github.com/pion/webrtc/v4"
)

// StartWebRTC negotiates a WebRTC DataChannel with the paired partner using the
// existing TCP connection as the signaling channel. Once the DataChannel is open,
// onReady is called with a send function that routes MIDI bytes directly peer-to-peer,
// bypassing the TCP relay server.
func (c *Client) StartWebRTC(onReady func(send func([]byte) error)) (retErr error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
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

	// setupDataChannel wires OnOpen and OnMessage onto a DataChannel.
	// Called by both paths: offerer passes dc from CreateDataChannel,
	// answerer passes dc received in OnDataChannel.
	setupDataChannel := func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			onReady(func(data []byte) error {
				return dc.Send(data)
			})
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if c.midiOutput != nil {
				if err := c.midiOutput(msg.Data); err != nil {
					log.Printf("p2p midi playback error: %v", err)
				}
			}
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
		dc, err := pc.CreateDataChannel("midi", &webrtc.DataChannelInit{Ordered: &ordered})
		if err != nil {
			return fmt.Errorf("CreateDataChannel: %w", err)
		}
		//   2. setupDataChannel(dc)
		setupDataChannel(dc)

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
		//   1. pc.OnDataChannel(func(dc) { setupDataChannel(dc) })
		pc.OnDataChannel(func(dc *webrtc.DataChannel) { setupDataChannel(dc) })
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
