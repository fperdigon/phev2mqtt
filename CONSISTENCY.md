# phev2mqtt — Consistency & Logic Review (Second Pass)

> **Date:** 2026-07-04  
> **Branch:** `fixes/audit-2026-07-04`  
> **Basis:** Second pass after the 18-issue audit (see `FINDINGS.md`)

This document records issues found during a full logic and consistency review of
every source file. Issues are ordered by severity. All `STATUS: FIXED` items have
tests covering the corrected behaviour.

---

## ISSUE L1 — `RegisterChargePlug.Decode` misclassifies `0x0002` as disconnected

**File:** `protocol/message.go` · `RegisterChargePlug.Decode`  
**Severity:** High  
**Status:** FIXED (commit in this pass)

**Description:**  
The condition `r.Connected = (m.Data[1] == 1 || m.Data[0] > 0)` maps wire value
`0x0002` (`Data[0]=0x00, Data[1]=0x02`) to `Connected=false`. The protocol
README documents `0x0002` as "plug connected, charging pending" — the cable is
physically present.

**Fix:**  
Changed condition to `r.Connected = (m.Data[1] > 0 || m.Data[0] > 0)`.

**Test:** `TestRegisterChargePlugDecode/charging_pending_0x0002`

---

## ISSUE L2 — `RegisterACMode.Decode` maps mode 0 to `"unknown"` instead of `"off"`

**File:** `protocol/message.go` · `RegisterACMode.Decode`  
**Severity:** High  
**Status:** FIXED (commit in this pass)

**Description:**  
When the car reports mode nibble `0x0` (AC inactive), the code set `Mode =
"unknown"`. The HA discovery `select` entity lists options `["off", "heat",
"cool", "windscreen"]` — `"unknown"` is not in this list, causing HA to show a
stale or errored select widget when AC turns off.

The `mqttStates()` switch also had no `"unknown"` case, so the mode topic would
emit `"unknown"` while all sub-topics remained `"off"` — an inconsistent state.

**Fix:**  
`case 0: r.Mode = "off"`.  Added `default: r.Mode = "unknown"` for unrecognised
nibble values (4–15).

**Test:** `TestRegisterACModeDecode/off_(0x00)`, `TestRegisterACModeDecode/unknown_nibble_0x0f`

---

## ISSUE L3 — `publishedDiscovery` was a package-level variable

**File:** `cmd/mqtt.go`  
**Severity:** Medium  
**Status:** FIXED (commit in this pass)

**Description:**  
`var publishedDiscovery = false` was a package-level variable. Once set to
`true` it was never cleared, so Home Assistant discovery messages were never
re-sent after a broker restart. With `retain=false` (also a bug — see L4),
entities disappeared permanently from HA after a broker restart.

**Fix:**  
Moved `publishedDiscovery` to a field on `mqttClient`. Added a
`SetOnConnectHandler` callback that resets `m.publishedDiscovery = false` on
every MQTT broker connection (including reconnects), triggering re-discovery.

**Test:** `TestPublishRegisteredDiscovery`

---

## ISSUE L4 — HA discovery messages published with `retain=false`

**File:** `cmd/mqtt.go` · `publishHomeAssistantDiscovery`  
**Severity:** Medium  
**Status:** FIXED (commit in this pass)

**Description:**  
Discovery JSON blobs were published with QoS 0, `retain=false`. If the broker
restarted, HA would lose all PHEV entities until the binary reconnected to the
car AND received the VIN register again — which could take minutes or never
happen if the car was away. The broker retains no memory of the entities.

**Fix:** Changed to `m.client.Publish(topic, 0, true, d)` (`retain=true`).

---

## ISSUE L5 — `lastWifiRestart` was a package-level variable

**File:** `cmd/mqtt.go`  
**Severity:** Low  
**Status:** FIXED (commit in this pass)

**Description:**  
`var lastWifiRestart time.Time` was package-level mutable state. While not a
race condition in normal use (only accessed from `Run`'s main goroutine), it's
not safe for testing and violates the principle that all `mqttClient` state
should be instance-local.

**Fix:** Moved to a field on `mqttClient`. Changed `restartWifi()` to a method
`(m *mqttClient) restartWifi()`.

---

## ISSUE L6 — `watch --wait` flag defined but never read

**File:** `cmd/watch.go`  
**Severity:** Low  
**Status:** FIXED (commit in this pass)

**Description:**  
`watchCmd.Flags().DurationP("wait", ...)` registered a user-visible flag but
the `Run()` function never called `cmd.Flags().GetDuration("wait")`. The command
ran forever regardless of the value supplied, making the flag purely cosmetic and
misleading.

**Fix:** `Run()` now reads the flag and uses `time.After(wait)` to implement the
timeout. Default changed from `60s` to `0` (forever), since a 60 s default would
silently terminate a monitoring session.

---

## ISSUE L7 — `register.go` blocks forever if VIN never arrives

**File:** `cmd/register.go` · `runRegister`  
**Severity:** Medium  
**Status:** FIXED (commit in this pass)

**Description:**  
`vin, ok := <-vinCh` had no timeout. If the car never sent a VIN register
(connection refused, wrong protocol version, etc.) the command hung indefinitely
with no error message.

**Fix:** Wrapped in `select { case <-vinCh: ... case <-time.After(30s): error + cl.Close() }`.
Also fixed a typo: `"recieving"` → `"receiving"`.

---

## ISSUE L8 — Dead `"OFF": 0x0` entry in climate `modeMap`

**File:** `cmd/mqtt.go` · `handleIncomingMqtt`  
**Severity:** Low  
**Status:** FIXED (commit in this pass)

**Description:**  
`payload := strings.ToLower(string(msg.Payload()))` normalised the payload to
lowercase before the map lookup, making the `"OFF": 0x0` key unreachable. It
was dead code that added noise to the map.

**Fix:** Removed the `"OFF"` key.

---

## ISSUE L9 — Headlights log error message cites wrong register number

**File:** `cmd/mqtt.go` · `handleIncomingMqtt`  
**Severity:** Low  
**Status:** FIXED (commit in this pass)

**Description:**  
When `m.phev.SetRegister(0xa, ...)` failed in the `/set/headlights` branch, the
error message said `"Error setting register 0xb"` (the parking lights register)
instead of `"Error setting register 0xa"`.

**Fix:** Corrected to `"Error setting register 0xa"`.

---

## ISSUE L10 — `NewFromBytes` used `fmt.Printf` for decode errors

**File:** `protocol/message.go` · `NewFromBytes`  
**Severity:** Low  
**Status:** FIXED (commit in this pass)

**Description:**  
Decode errors in `NewFromBytes` were written to `os.Stdout` via `fmt.Printf`,
bypassing the application's `logrus` logger. This broke log-level filtering and
log aggregation (journald would miss these lines).

**Fix:** Changed to `log.Errorf("decode error: %v", err)`.

---

## Previously Deferred Issues (Now Fixed)

### R1 — `c.closed` bool was a data race in `client/client.go`

**Status: FIXED** — replaced `closed bool` with `atomic.Bool`; all 5 read/write
sites use `.Load()`/`.Store()`. Test: `TestClosedAtomicNoRace` (run with `-race`
on any x86 CI; RPi 3B kernel has 39-bit VMA which ThreadSanitizer does not support).

### R2 — `manage()` goroutine leaked after TCP disconnect

**Status: FIXED** — `reader()` cleanup now calls `l.ProcessStop()` after
`l.Stop()`, which closes `l.C` and unblocks `manage()`s `for m := range ml.C`.
Test: `TestManageGoroutineExitsOnDisconnect`.

### R3 — `SetRegister` timer not reset on retry

**Status: FIXED** — replaced `goto SETREG` with an outer `for` loop. `timer :=
time.After(10s)` is now inside the loop so each attempt starts with a fresh
10-second window. Test: `TestSetRegisterFreshTimerOnRetry`.

### R4 — `"mode": 0x4` in climate `modeMap` was an undocumented sentinel

**Status: FIXED** — sentinel removed; dispatch extracted to
`resolveClimateMode(lastPart, payload)` which handles both cases explicitly.
`modeMap` now contains only the 4 actual protocol byte values (off/cool/heat/windscreen).
Test: `TestResolveClimateMode`.
