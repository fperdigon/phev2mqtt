# phev2mqtt Code Analysis — Findings & Fixes

**Audit date:** 2026-07-04  
**Auditor:** fperdigon  
**Branch:** `fixes/audit-2026-07-04`  
**Base commit:** c698b3a

---

## Summary

18 issues identified across four packages: `protocol/`, `client/`, `cmd/`, and `go.mod`.  
Issues range from latent data-corruption bugs to reliability and security improvements.

| # | Severity | Package | File | Issue |
|---|----------|---------|------|-------|
| 1 | High | protocol | raw.go | `Checksum()` uses byte arithmetic → wraps on length ≥ 254 |
| 2 | High | protocol | raw.go | `GenerateProposal()` uses unseeded `math/rand` → predictable key |
| 3 | High | protocol | raw.go | No `SecurityKey.Snapshot()` helper for safe recovery scanning |
| 4 | High | protocol | message.go | `NewFromBytes` double-XOR: `ValidateAndDecodeMessage` result re-encoded before `DecodeFromBytes`, which decodes it again |
| 5 | High | protocol | message.go | `NewFromBytes` sliding-window mutates live key on misaligned frames — a false-positive `CmdInMy18StartReq` regenerates the entire keymap |
| 6 | Medium | client | client.go | `pinger()` goroutine blocks forever on full `Send` channel after writer exits |
| 7 | Medium | client | client.go | `startTimeout` = 20 s — too short for slow car WiFi association |
| 8 | Medium | cmd | mqtt.go | `publish()` uses `retain=false` — HA loses all sensor state on broker restart |
| 9 | Medium | cmd | mqtt.go | `update_interval` ticker fires inside receive loop via `SetRegister` — blocks up to 10 s |
| 10 | Medium | cmd | mqtt.go | `/door/front_right` duplicates `/door/driver` (same `reg.Driver` source) |
| 11 | Low | cmd | mqtt.go | `charge/remaining` sentinel values ≥ 1000 published as-is; HA shows nonsense |
| 12 | Low | cmd | mqtt.go | `SetRegister(0x6, ...)` in update loop has no comment explaining purpose |
| 13 | Low | cmd | mqtt.go | MQTT client ID hardcoded to `"phev2mqtt"` — prevents two instances on same broker |
| 14 | Low | cmd | mqtt.go | HA discovery JSON uses both `"device"` and `"dev"` keys inconsistently |
| 15 | Low | cmd | mqtt.go | `defaultWifiRestartCmd` hardcodes `wlan0` — breaks on systems with different interface names |
| 16 | Low | go.mod | go.mod | `go 1.16` declared but RPi runs Go 1.19; module directive does not match runtime |
| 17 | Low | go.mod | go.mod | `wercker/journalhook` listed as `indirect` but imported in `cmd/root.go`; EOL project |
| 18 | Low | cmd | pcap.go / set.go | Two `go vet` warnings: wrong-arity `logrus.Errorf` and type-mismatch `fmt.Printf` |

---

## Issue Details

### #1 — `Checksum()` byte overflow

**File:** `protocol/raw.go:134`

The loop variable `i` and the length `length` are both `byte`. When `message[1] >= 254`, `length = message[1] + 2` wraps to 0 or 1, causing the loop to either not run or compute a completely wrong checksum.

```go
// Before (broken)
func Checksum(message []byte) byte {
    length := message[1] + 2  // byte arithmetic — wraps at 256
    b := byte(0)
    for i := byte(0); ; i++ {
        if i >= length-1 { break }
        b = (byte)(message[i] + b)
    }
    return b
}
```

**Fix:** Use `int` for length and loop variable. Add bounds guard.

---

### #2 — Predictable security key proposal (`math/rand`)

**File:** `protocol/raw.go:27`

`GenerateProposal()` fills the 8-byte proposed key with `math/rand.Intn(256)`. Without an explicit seed (none is set anywhere), the sequence is the same on every cold start (Go 1.19 and earlier). An attacker on the same WiFi network can predict the proposed key and spoof authentication.

**Fix:** Replace with `crypto/rand.Read`.

---

### #3 — Missing `SecurityKey.Snapshot()`

**File:** `protocol/raw.go` (new method)

Required by fix #5: a deep copy of the key struct including the `keyMap` slice, so that framing-recovery decodes operate on an isolated copy without touching live state.

---

### #4 — Double XOR in `NewFromBytes`

**File:** `protocol/message.go:217`

`ValidateAndDecodeMessage` returns the XOR-decoded frame (`dat`). The code then immediately re-encodes it (`dat = XorMessageWith(dat, xor)`) before calling `DecodeFromBytes`, which calls `ValidateAndDecodeMessage` again internally. The raw frame is XOR'd and un-XOR'd twice for no purpose.

**Fix:** Pass the original raw slice (`data[offset : offset+frameLen]`) directly to `DecodeFromBytes`.

---

### #5 — Security key corruption during sliding-window recovery

**File:** `protocol/message.go:202`

`NewFromBytes` uses a sliding-window scan when a frame is not found at offset 0. Each candidate byte position is tried until a valid checksum is found. `DecodeFromBytes` is called for each candidate, including ones at `offset > 0`.

`DecodeFromBytes` calls `key.Update(p.OriginalXored)` for `CmdInMy18StartReq` frames. If the scanner stumbles onto bytes that happen to form a valid-looking Start18 frame at `offset > 0`, `Update()` regenerates the entire keymap from garbage bytes. All subsequent outgoing frames are encoded with the wrong XOR, causing a `CmdInBadEncoding` cascade from the car.

**Root cause of the 2026-07-04 bad-sum storm:** this is what triggers the 7-hour "Bad sum" loop after midnight reboots when the car's TCP state was stale.

**Fix:** When `offset > 0`, use `key.Snapshot()` as the key argument to `DecodeFromBytes`, so any key mutation affects only the copy.

---

### #6 — Pinger goroutine blocks on full `Send` channel

**File:** `client/client.go:244`

After the TCP writer exits due to a write error, no goroutine drains `c.Send`. The pinger continues ticking every 200 ms and blocks on `c.Send <- ...` once the channel's buffer (capacity 5) fills up. The goroutine is leaked until `c.Close()` is eventually called.

**Fix:** Use `select` with a `default` case in the pinger so that a full channel is silently skipped.

---

### #7 — `startTimeout` too short

**File:** `client/client.go:158`

The car's WiFi AP typically takes 5–8 s to associate and the TCP + start handshake adds further latency. The current 20 s timeout is tight; a brief network hiccup causes a spurious timeout and a full reconnection cycle.

**Fix:** Increase to 45 s.

---

### #8 — Sensor MQTT publishes without `retain`

**File:** `cmd/mqtt.go:209`

`publish()` calls `m.client.Publish(..., false, ...)` (retain=false). When the MQTT broker restarts (or HA restarts), all PHEV state topics are lost. HA shows "unknown" for all sensors until the car sends a full register update.

**Fix:** Set retain=true.

---

### #9 — `update_interval` ticker blocks receive loop

**File:** `cmd/mqtt.go:362`

`handlePhev`'s main select loop handles the `updaterTicker.C` case by calling `m.phev.SetRegister(0x6, []byte{0x3})`, which internally waits up to 10 s for an ACK from the car. During this 10 s window, no other `Recv` messages are processed, including `CmdInBadEncoding` (which would reset the encoding error counter).

**Fix:** Launch the `SetRegister` call in a separate goroutine so the receive loop is never blocked.

---

### #10 — Duplicate `/door/front_right` topic

**File:** `cmd/mqtt.go:435`

Both `/door/front_right` and `/door/driver` publish `boolOpen[reg.Driver]`. The `RegisterDoorStatus` struct has a `Driver` field (the driver's door). There is no distinct "front right" door field. The duplicate topic pollutes HA and is not backed by any HA discovery entity.

**Fix:** Remove the `/door/front_right` publish line.

---

### #11 — `charge/remaining` sentinel values leak to HA

**File:** `cmd/mqtt.go:430`

`RegisterChargeStatus.Remaining` is computed as `int(Data[2])<<8 | int(Data[1])`. The car uses `Data[2] == 0xff` for "not available", which is already filtered to 0. However other sentinel values (e.g. `0x07ff = 2047`) are passed through unchanged. A real PHEV battery can charge at most ~8 h (480 min); any value ≥ 1000 is a sentinel and should not be published.

**Fix:** Cap values ≥ 1000 to 0 before publishing.

---

### #12 — `SetRegister(0x6)` without explanation

**File:** `cmd/mqtt.go:363`

The update-interval write to register 0x6 with data `[0x3]` has no comment. Register 0x6 is a "request full refresh" command — it tells the car to re-send all register states.

**Fix:** Add a comment.

---

### #13 — Hardcoded MQTT client ID

**File:** `cmd/mqtt.go:165`

`SetClientID("phev2mqtt")` prevents running two instances against the same broker (each new connection kicks the previous one). Important for testing and multi-car setups.

**Fix:** Add `--mqtt_client_id` flag defaulting to `"phev2mqtt"`.

---

### #14 — Mixed `"device"` / `"dev"` keys in HA discovery JSON

**File:** `cmd/mqtt.go:466+`

Some entities use `"device": {...}` (correct HA shortname) and others use `"dev": {...}`. HA accepts both, but inconsistency makes maintenance harder.

**Fix:** Standardize all entries to `"device"`.

---

### #15 — Hardcoded `wlan0` in default wifi restart command

**File:** `cmd/mqtt.go:34`

```go
const defaultWifiRestartCmd = "sudo ip link set wlan0 down && sleep 3 && sudo ip link set wlan0 up"
```

Only works on systems where the WiFi interface is named `wlan0`. Systems using NetworkManager predictable interface names (e.g. `wlp3s0`) or the RPi's `wlan0` via systemd naming need a custom value.

**Fix:** Change default to empty string (disabling auto-restart) and document the flag. Users opt in by setting `--wifi_restart_command`.

---

### #16 — go.mod `go 1.16` vs runtime Go 1.19

**File:** `go.mod:3`

The runtime on the RPi is Go 1.19.8. Declaring `go 1.16` in go.mod is not wrong but can suppress language features and module graph updates available in 1.19.

**Fix:** Update to `go 1.19`.

---

### #17 — Unused / EOL `journalhook` dependency

**File:** `go.mod:14`, `cmd/root.go`

`wercker/journalhook` integrates logrus with systemd journal. The project is effectively unmaintained. On systems without systemd journal support, the import adds startup cost.

**Fix:** Keep but annotate; document as optional systemd integration.

---

### #18 — `go vet` warnings in `cmd/`

**Files:** `cmd/pcap.go:106`, `cmd/set.go:97`

- `logrus.Errorf(err.Error())` — format string is the only argument but `Errorf` expects args
- `fmt.Printf("...", register[0])` — format verb mismatch

**Fix:** Correct both call sites.
