# synth-stream

Real-time MIDI collaboration over the network. Two musicians connect, play, and hear each other with peer-to-peer audio routing via WebRTC.

---

## How it works

Each musician runs the client on their machine. The server handles authentication and session pairing. Once both musicians are connected, MIDI data flows directly peer-to-peer via a WebRTC DataChannel — the server is no longer in the data path.

If a direct connection cannot be established, the session falls back to TCP relay through the server automatically.

---

## Requirements

- Go 1.21+
- PostgreSQL (for user accounts)
- A virtual MIDI driver to route MIDI between synth-stream and your DAW:
  - **macOS**: IAC Driver (built-in, see setup below)
  - **Windows**: [loopMIDI](https://www.tobias-erichsen.de/software/loopmidi.html)
  - **Linux**: ALSA virtual MIDI or JACK

---

## MIDI Setup (macOS)

The IAC Driver creates virtual MIDI buses that allow synth-stream to send MIDI data into your DAW.

1. Open **Audio MIDI Setup** (search in Spotlight)
2. Open **Window → MIDI Studio**
3. Double-click **IAC Driver**
4. Check **Device is online**
5. Add at least 2 buses (e.g. Bus 1, Bus 2) — one for input, one for output

In your DAW (e.g. Ableton Live):
- Set your MIDI input track to receive from the IAC bus synth-stream will send to
- Set synth-stream's output to a different IAC bus than your controller uses, to avoid feedback loops

**Recommended 4-bus setup for two musicians:**
```
Bus 1 — Alice's controller → synth-stream input
Bus 2 — synth-stream output → Alice's DAW
Bus 3 — Bobby's controller → synth-stream input
Bus 4 — synth-stream output → Bobby's DAW
```

---

## Server Setup

1. Copy `.env.example` to `.env` and fill in your values:
   ```
   PORT=8080
   API_PORT=8081
   DATABASE_URL=postgres://user:password@localhost:5432/synthstream
   JWT_SECRET=your-secret-here
   ```

2. Create the database schema:
   ```bash
   psql $DATABASE_URL < db/schema.sql
   ```

3. Start the server:
   ```bash
   go run cmd/server/main.go
   ```

---

## Client Usage

### 1. Create an account

```bash
curl -X POST http://localhost:8081/register \
  -H "Content-Type: application/json" \
  -d '{"username": "alice", "password": "yourpassword"}'
```

### 2. Start the client

```bash
go run cmd/client/main.go
```

### 3. Log in

Enter your username and password when prompted.

### 4. Select MIDI devices

The client lists all available MIDI input and output devices. Enter the number corresponding to:
- **Input**: the device your controller/keyboard is connected to
- **Output**: the IAC bus your DAW is listening on

### 5. Create or join a session

- **Create**: press `c` — you'll receive a 6-character session code (e.g. `AB12CD`)
- **Join**: press `j` — enter the session code your partner shared with you

### 6. Play

Once both musicians are connected, MIDI flows in real time. You'll see a ping measurement confirming the connection.

---

## Chat Commands

While in a session, type messages to chat with your partner. Special commands:

| Command | Description |
|---|---|
| `/ping` | Measure round-trip latency to the server |
| `/disconnect` | Disconnect from the session (for testing reconnection) |

---

## Reconnection

If your connection drops, the client automatically retries up to 3 times with a 2-second backoff. The session is preserved on the server for 2 minutes — if you reconnect within that window, you rejoin your existing session without your partner needing to do anything.
