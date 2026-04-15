# synth-stream

Real-time MIDI collaboration over the internet. Two musicians connect, play, and hear each other live — MIDI data flows directly peer-to-peer via WebRTC, falling back to a TCP relay server automatically if a direct connection can't be established.

---

## What you need

- A Mac (Windows and Linux support is in progress)
- A DAW — Ableton Live, GarageBand, Logic, etc.
- A MIDI controller (keyboard, pad, etc.)
- Go 1.21+ to build the client

---

## MIDI routing setup (macOS)

synth-stream sends and receives raw MIDI. To get that MIDI into and out of your DAW, you need the IAC Driver — a virtual MIDI cable built into macOS.

1. Open **Audio MIDI Setup** (search in Spotlight)
2. Go to **Window → MIDI Studio**
3. Double-click **IAC Driver**
4. Check **Device is online**
5. Add at least 2 buses using the **+** button

In your DAW, set one instrument track to receive MIDI from the IAC bus synth-stream outputs to, with Monitor set to **In**. That track will play everything your partner sends.

**Recommended 4-bus setup for a single-machine test:**

```
IAC Bus 1 — Alice's controller → synth-stream captures here (input)
IAC Bus 2 — synth-stream writes here → Alice's DAW receives (output)
IAC Bus 3 — Bobby's controller → synth-stream captures here (input)
IAC Bus 4 — synth-stream writes here → Bobby's DAW receives (output)
```

For a two-machine test, each machine only needs 2 buses.

---

## Getting started

### 1. Create an account

The server is live at `https://synth-stream.fly.dev`. Register with:

```bash
curl -X POST https://synth-stream.fly.dev/register \
  -H "Content-Type: application/json" \
  -d '{"username": "alice", "password": "yourpassword"}'
```

### 2. Build the client

```bash
git clone https://github.com/jj-attaq/synth-stream
cd synth-stream
go build -o synth-stream-client ./cmd/client
```

### 3. Run the client

```bash
export API_URL=https://synth-stream.fly.dev
export SERVER_HOST=<tcp-server-host>  # available on request — see note below
export PORT=8080
./synth-stream-client
```

> **Note:** The TCP server host is not published here to avoid overloading the free-tier infrastructure this project runs on. If you'd like to try it, open an issue or get in touch directly.

The client will prompt for your username and password, show your available MIDI devices, and ask you to pick input and output port numbers.

### 4. Start or join a session

- Press **c** to create a session — you'll get a 6-character code (e.g. `AB12CD`)
- Share that code with your partner
- Your partner runs the client and presses **j**, then enters the code

Once both musicians are connected, you'll see a ping measurement and can start playing. MIDI you play goes to your partner in real time, and theirs comes back to you.

---

## Chat commands

While in a session you can type messages to chat with your partner. Special commands:

| Command | What it does |
|---|---|
| `/ping` | Measure round-trip latency to the relay server |
| `/quit` | Leave the session cleanly and exit |

Press **Ctrl-C** at any time to exit — the session is closed gracefully.

---

## Reconnection

If your connection drops unexpectedly, the client retries automatically up to 3 times. The server holds your session open for 2 minutes — if you reconnect within that window, you rejoin without your partner needing to do anything.

---

## Architecture

Two servers run in the same process:

```
HTTP API  (:8081)  — accounts, login, JWT tokens
TCP server (:8080)  — real-time MIDI relay and WebRTC signaling
```

The client logs in via HTTP to get a JWT, then presents that token to the TCP server during the handshake. The two servers never talk directly — the token carries the user's identity between them.

Once two musicians are paired, the TCP connection is used only for signaling (exchanging the WebRTC offer/answer). After that, MIDI and chat both flow peer-to-peer via dedicated WebRTC DataChannels. If WebRTC negotiation fails or times out (15 seconds), both fall back to routing through the TCP server automatically.

**Stack:** Go, PostgreSQL, sqlc, pion/webrtc, Fly.io, Supabase

**Key packages:**

| Package | Role |
|---|---|
| `internal/protocol` | Custom binary protocol (TLV framing over TCP) |
| `internal/server` | TCP server, session pairing, MIDI routing |
| `internal/client` | TCP client, WebRTC negotiation, MIDI I/O |
| `internal/auth` | bcrypt password hashing, JWT signing and validation |
| `internal/api` | HTTP handlers for `/register` and `/login` |
| `internal/midi` | MIDI device discovery, capture, and playback |
