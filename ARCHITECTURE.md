# phev2mqtt Architecture

## Overview

`phev2mqtt` is a Go program that bridges a **Mitsubishi Outlander PHEV** to an
**MQTT broker** (and optionally Home Assistant via auto-discovery). It runs on a
Raspberry Pi 3B that connects to the car's Wi-Fi access point
(`REMOTE47fcta / 192.168.8.46:8080`).

```
Car WiFi AP (192.168.8.46:8080)
        │ TCP
        ▼
  ┌─────────────┐   Recv channel   ┌──────────────┐
  │ client pkg  │ ────────────────► │              │   MQTT publish
  │  (TCP I/O,  │ ◄──────────────  │  cmd/mqtt.go │ ──────────────► Broker
  │   XOR key)  │   Send channel   │  mqttClient  │ ◄──────────────
  └─────────────┘                  └──────────────┘   MQTT subscribe
```

---

## Package Structure

```
phev2mqtt/
├── main.go              — Entry point, calls cmd.Execute()
├── cmd/                 — Cobra commands (CLI surface)
│   ├── root.go          — Root command, log level, viper config
│   ├── client.go        — "client" parent command, --address flag
│   ├── mqtt.go          — "client mqtt" — the main bridge command
│   ├── watch.go         — "client watch" — print register changes
│   ├── set.go           — "client set" — write a register from CLI
│   ├── register.go      — "client register/unregister" — WiFi pairing
│   ├── decode.go        — "decode" parent command
│   ├── hex.go           — "decode hex" — decode a hex string
│   ├── file.go          — "decode file" — decode a binary capture
│   └── pcap.go          — "decode pcap" — decode a pcap capture
├── client/
│   └── client.go        — TCP connection management, goroutine model
└── protocol/
    ├── raw.go           — Frame framing, XOR key management, checksum
    ├── message.go       — PhevMessage type, register decoders
    └── settings.go      — Register 0x16 (settings) decoder
```

---

## Protocol Layer (`protocol/`)

### Frame Format

```
Byte:  [0]      [1]         [2]     [3]        [4 .. length-2]  [length-1]
       Type     Length-2    XOR     Register   Data             Checksum
```

- `Length` field = actual total bytes − 2; stored as `data[1]`.
- `Checksum` = sum of bytes `[0 .. length-2]` mod 256.
- Every byte is XOR-encoded: `wire_byte = real_byte XOR xor_key`.
- `data[2]` (the XOR byte) is itself XOR-encoded, so `wire[2] XOR wire[2] = 0x00`
  after decoding — the XOR cancels itself out for byte[2].

### XOR Key Lifecycle

1. **Car sends `CmdInMy18StartReq` (0x5e) or `CmdInMy14StartReq` (0x4e):**
   - `SecurityKey.Update(packet)` extracts an 8-bit security key from bits [3]
     of bytes 4–11 in the raw (XOR-encoded) packet.
   - A 256-entry `keyMap` is generated via a non-standard Fisher-Yates shuffle
     seeded by `securityKey`.
   - `sNum` and `rNum` counters reset to 0.

2. **Outgoing frames (`EncodeToBytes`):**
   - `CmdOutSend` (0xf6): XOR = `keyMap[sNum]`, then `sNum++`.
   - All other types: XOR = `keyMap[sNum]` without incrementing.

3. **Incoming frames (`ValidateAndDecodeMessage`):**
   - XOR is read directly from `wire[2]` — no key lookup needed for decoding.
   - `DecodeFromBytes` calls `key.RKey(true)` for 0x6f frames (tracking `rNum`)
     and `key.SKey(true)` for 0xf6 frames.

4. **Framing recovery (`NewFromBytes`):**
   - If a valid frame isn't found at offset 0, the scanner increments offset.
   - Frames found at `offset > 0` are decoded with a `key.Snapshot()` (deep copy)
     to prevent a false-positive `CmdInMy18StartReq` from regenerating the keymap
     with garbage bytes.

### Register Decoders

| Register | Constant              | Decoded struct          |
|----------|-----------------------|-------------------------|
| 0x02     | BatteryWarningRegister | RegisterBatteryWarning  |
| 0x15     | VINRegister           | RegisterVIN             |
| 0x1a     | ACOperStatusRegister  | RegisterACOperStatus    |
| 0x1b     | SetACModeRegisterMY18 | (write-only)            |
| 0x1c     | ACModeRegister        | RegisterACMode          |
| 0x1d     | BatteryLevelRegister  | RegisterBatteryLevel    |
| 0x1e     | ChargePlugRegister    | RegisterChargePlug      |
| 0x1f     | ChargeStatusRegister  | RegisterChargeStatus    |
| 0x24     | DoorStatusRegister    | RegisterDoorStatus      |
| 0xc0     | ECUVersionRegister    | RegisterECUVersion      |

All other registers are decoded as `RegisterGeneric` (raw hex bytes).

---

## Client Layer (`client/`)

### Goroutine Model

`client.New()` creates a struct. `client.Connect()` dials TCP. `client.Start()`
launches three goroutines:

```
Start()
  ├── reader()  — TCP → NewFromBytes → send to listeners' channels
  ├── writer()  — reads from c.Send channel → EncodeToBytes → TCP write
  └── pinger()  — every 4 s: non-blocking send of CmdOutPingReq to c.Send
```

**`client.Recv` channel** (capacity 16): the default listener. Callers (`cmd/mqtt.go`,
`cmd/watch.go`) range over this to receive decoded frames.

**`AddListener()`** creates additional listeners (used by `SetRegister` for
command/response matching).

**`SetRegister(reg, data)`** sends a `CmdOutSend` frame and waits up to 10 s for
an acknowledgement. On `CmdInBadEncoding` response, it retries once with the
XOR value the car expected.

### Key invariants
- `c.Send` channel has capacity 5. The pinger uses a non-blocking send (`select { default: }`) to avoid goroutine leaks when the writer stalls.
- `startTimeout` = 45 s: how long `Start()` waits to receive the first
  `CmdInMy18StartReq` or `CmdInMy14StartReq` before returning an error.

---

## Command Layer (`cmd/`)

### `mqtt.go` — Main Bridge

**`mqttClient` struct** holds all state for one MQTT session:

| Field               | Purpose                                             |
|---------------------|-----------------------------------------------------|
| `client`            | Paho MQTT client                                    |
| `phev`              | Active PHEV TCP client (replaced on reconnect)      |
| `mqttData`          | Cache of last-published values (dedup filter)       |
| `climate`           | Tracks AC state + mode across separate registers    |
| `publishedDiscovery`| Set `true` after first HA discovery publish; reset to `false` by `SetOnConnectHandler` on each broker reconnect |
| `lastWifiRestart`   | Throttle for optional WiFi interface restart        |

**Connection loop** (`Run` → `handlePhev`):
```
Run()
  SetOnConnectHandler → reset publishedDiscovery
  loop:
    handlePhev(cmd)     — connect, register MQTT, receive loop
    sleep 1s
    restart wifi if idle > wifi_restart_time
```

**`publishRegister(msg)`** dispatches each decoded register to MQTT topics.
The cache (`mqttData`) suppresses duplicate publishes — the broker only sees
a message when the value actually changes.

**Home Assistant discovery** is triggered by the first VIN register and uses
`retain=true` so entities persist across broker restarts.

**Climate state machine:** registers 0x1a (ACOperStatus) and 0x1c (ACMode)
arrive independently. The `climate` struct buffers both; `mqttStates()` combines
them only when both have arrived (`climate.ready()`).

### `watch.go`

Connects to the car, prints register changes, and ACKs each register. Accepts
`--wait N` to exit after `N` duration (0 = run forever, the default).

### `register.go`

Registers or unregisters the Pi's MAC address with the car. Waits up to 30 s
for the VIN register (needed to identify the car before writing reg 0x10/0x15).

### `decode.go` / `hex.go` / `file.go` / `pcap.go`

Off-line frame decoders. These use a shared package-level `securityKey` which
is valid since only one decode sub-command runs at a time.

---

## MQTT Topic Map

| Topic (relative to prefix) | Direction | Description                        |
|----------------------------|-----------|------------------------------------|
| `/available`               | publish   | `online` / `offline` LWT           |
| `/vin`                     | publish   | Vehicle Identification Number       |
| `/ecuversion`              | publish   | ECU firmware version string        |
| `/battery/level`           | publish   | Battery % (0–100)                  |
| `/charge/charging`         | publish   | `on` / `off`                       |
| `/charge/remaining`        | publish   | Minutes remaining (0 = sentinel)   |
| `/charge/plug`             | publish   | `connected` / `unplugged`          |
| `/climate/mode`            | publish   | `off` / `heat` / `cool` / `windscreen` |
| `/climate/heat`            | publish   | `on` / `off`                       |
| `/climate/cool`            | publish   | `on` / `off`                       |
| `/climate/windscreen`      | publish   | `on` / `off`                       |
| `/door/locked`             | publish   | `open`=unlocked / `closed`=locked  |
| `/door/driver`             | publish   | `open` / `closed`                  |
| `/door/front_passenger`    | publish   | `open` / `closed`                  |
| `/door/rear_left`          | publish   | `open` / `closed`                  |
| `/door/rear_right`         | publish   | `open` / `closed`                  |
| `/door/boot`               | publish   | `open` / `closed`                  |
| `/door/bonnet`             | publish   | `open` / `closed`                  |
| `/lights/head`             | publish   | `on` / `off`                       |
| `/lights/parking`          | publish   | `on` / `off`                       |
| `/register/XX`             | publish   | Raw hex data for register 0xXX     |
| `/set/climate/mode`        | subscribe | Send `heat`/`cool`/`windscreen`/`off` |
| `/set/climate/heat`        | subscribe | Send `on` (10 min) / `off`         |
| `/set/climate/cool`        | subscribe | Send `on` / `off`                  |
| `/set/climate/windscreen`  | subscribe | Send `on` / `off`                  |
| `/set/parkinglights`       | subscribe | `on` / `off`                       |
| `/set/headlights`          | subscribe | `on` / `off`                       |
| `/set/cancelchargetimer`   | subscribe | any payload cancels timer          |
| `/set/register/XX`         | subscribe | Raw register write (hex payload)   |
| `/connection`              | subscribe | `on` / `off` / `restart`           |

---

## systemd Service

```ini
[Service]
ExecStartPre=/bin/sleep 20   # prevents stale-session loop after midnight reboots
ExecStart=/home/pi/phev2mqtt/phev2mqtt client mqtt ...
Restart=always
```

The 20-second pre-sleep prevents the car's TCP stack from rejecting the
connection when the Pi reconnects too quickly after a reboot (the car stores
the Raspberry Pi's MAC address `b8:27:eb:c1:89:60` for security and will not
accept connections from any other MAC).
