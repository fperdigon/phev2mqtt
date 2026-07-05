# Changelog

All notable changes to this fork (`fperdigon/phev2mqtt`) relative to the
upstream (`buxtronix/phev2mqtt`) are documented here.

Issue numbers in parentheses refer to open issues in the
[upstream repository](https://github.com/buxtronix/phev2mqtt/issues).

---

## [Unreleased] — 2026-07-04

This fork incorporates all community contributions merged into upstream through
commit `c698b3a`, then adds a full code audit with 14 bug fixes, expanded test
coverage, and operational tooling for long-running Raspberry Pi deployments.

### Fixed — Protocol / Framing

- **Root cause of "Bad sum" loop** (`protocol/message.go`, `client/client.go`)
  *(addresses [#25](https://github.com/buxtronix/phev2mqtt/issues/25))*:
  `NewFromBytes` was calling `key.Update()` on false-positive frames found at
  non-zero offsets during framing recovery, corrupting the rolling XOR key and
  causing every subsequent frame to fail checksum — the symptom reported as an
  endless ping loop with incrementing IDs and repeated "Bad sum" log lines.
  Fix uses `key.Snapshot()` for any frame found at offset > 0 so the live key
  is never mutated by a speculative decode.
- **`Checksum()` byte overflow** (`protocol/raw.go`): length byte was accumulated
  as `byte`, wrapping silently on frames where `message[1]` is near `0xff`. Widened
  to `int`.
- **Crypto/rand for key proposal** (`protocol/raw.go`): replaced `math/rand` with
  `crypto/rand` so the 8-byte security key proposal is cryptographically random.
- **`RegisterChargePlug.Decode` misclassified `0x0002`** (`protocol/message.go`):
  wire value `0x0002` (plug connected, charging pending) decoded as `Connected=false`.
  Fixed condition to `Data[1] > 0 || Data[0] > 0`.
- **`RegisterACMode` mapped mode 0 to `"unknown"`** (`protocol/message.go`)
  *(contributes to [#56](https://github.com/buxtronix/phev2mqtt/issues/56))*:
  caused Home Assistant select widget to show a stale/errored state when AC
  turned off. `case 0` now maps to `"off"`; a `default` case handles genuinely
  unknown nibbles.
- **`NewFromBytes` used `fmt.Printf` for decode errors** (`protocol/message.go`):
  errors bypassed the `logrus` logger and `journald`. Changed to `log.Errorf`.

### Fixed — Client / Connection

- **`c.closed` data race** (`client/client.go`): `pinger`, `reader`, `writer`, and
  `Close`/`Connect` accessed a plain `bool` concurrently. Replaced with
  `sync/atomic.Bool`.
- **`manage()` goroutine leak** (`client/client.go`)
  *(contributes to [#34](https://github.com/buxtronix/phev2mqtt/issues/34))*:
  on TCP disconnect, `reader` set `l.stop = true` on all listeners but never
  closed their channels. `manage()` blocked forever on `for m := range ml.C`,
  leaking one goroutine per reconnect cycle. Fixed by calling `l.ProcessStop()`
  (which closes `l.C`) in the cleanup path.
- **`SetRegister` timer not reset on retry** (`client/client.go`): `time.After(10s)`
  was created once before a `goto SETREG` label; a `CmdInBadEncoding` reply reused
  the original (possibly near-expiry) timer. Replaced `goto` with an outer `for`
  loop so each attempt gets a fresh 10-second window.
- **Pinger goroutine leak** (`client/client.go`)
  *(contributes to [#34](https://github.com/buxtronix/phev2mqtt/issues/34))*:
  pinger used a blocking send on `c.Send`; a stalled writer goroutine would block
  the pinger indefinitely. Changed to a non-blocking send with a `default` case.
- **`startTimeout` increased from 20 s to 45 s** (`client/client.go`)
  *(addresses [#59](https://github.com/buxtronix/phev2mqtt/issues/59))*:
  the original timeout was too tight for slow WiFi association + TCP setup,
  causing "timed out waiting for start" failures on some hardware configurations.

### Fixed — MQTT / Home Assistant

- **`publishedDiscovery` was a package-level variable** (`cmd/mqtt.go`)
  *(addresses [#54](https://github.com/buxtronix/phev2mqtt/issues/54),
  contributes to [#56](https://github.com/buxtronix/phev2mqtt/issues/56))*:
  once set to `true` it was never cleared, so HA discovery was never re-sent
  after a broker restart — entities became permanently unavailable until
  phev2mqtt was manually restarted. Moved to a field on `mqttClient`; a
  `SetOnConnectHandler` callback resets it on every MQTT reconnect.
- **HA discovery messages published with `retain=false`** (`cmd/mqtt.go`)
  *(addresses [#54](https://github.com/buxtronix/phev2mqtt/issues/54),
  contributes to [#56](https://github.com/buxtronix/phev2mqtt/issues/56))*:
  entities disappeared from HA permanently if the broker restarted before the
  car reconnected. Changed to `retain=true` so the broker always holds the
  last discovery payload.
- **`0x4` sentinel in climate `modeMap`** (`cmd/mqtt.go`): `"mode":0x4` was an
  undocumented internal sentinel mixed into the protocol-value map. Removed;
  extracted `resolveClimateMode(lastPart, payload)` which handles both dispatch
  cases explicitly. `modeMap` now contains only the 4 real protocol values.
- **`lastWifiRestart` was a package-level variable** (`cmd/mqtt.go`): moved to a
  field on `mqttClient` so it is instance-local and safe for testing.
- **Dead `"OFF": 0x0` in climate `modeMap`** (`cmd/mqtt.go`): payload is
  lowercased before the map lookup, making the uppercase key unreachable.
- **`watch --wait` flag was ignored** (`cmd/watch.go`): the flag was registered but
  never read; the command ran forever regardless. Implemented timeout using
  `time.After`; default changed from 60 s to 0 (forever).
- **`register` command blocked forever if VIN never arrived** (`cmd/register.go`):
  `<-vinCh` had no timeout. Added 30-second deadline with a clear error message.
- **Headlights error log cited wrong register** (`cmd/mqtt.go`): error for register
  `0xa` (headlights) said `0xb` (parking lights).

### Added — Tests

Test count increased from 28 to 57 across three packages:

- **`protocol/message_test.go`**: `TestRegisterChargePlugDecode` (all 5 plug states),
  `TestRegisterACModeDecode` (all modes, durations, unknown nibble),
  `TestRegisterBatteryLevelDecode`, `TestRegisterVINDecode`,
  `TestRegisterACOperStatusDecode`, `TestRegisterBatteryWarningDecode`,
  `TestRegisterDecodeLengthGuards`.
- **`cmd/mqtt_test.go`**: `TestPublishRegisterVIN`, `TestPublishRegisteredDiscovery`
  (verifies per-instance field, not package-level var), `TestClimateACModeModeOff`,
  `TestClimateACModeUnknown`, `TestResolveClimateMode` (11 cases covering R4).
- **`client/client_test.go`** (new file): `TestClosedAtomicNoRace` (R1),
  `TestManageGoroutineExitsOnDisconnect` (R2), `TestSetRegisterFreshTimerOnRetry` (R3).

Note: `go test -race` is not available on Raspberry Pi 3B (39-bit kernel VMA;
ThreadSanitizer requires 48-bit). Run `-race` on any x86 CI runner.

### Added — Documentation

- **`FINDINGS.md`**: full first-pass audit of 18 issues with root-cause analysis.
- **`ARCHITECTURE.md`**: package structure, goroutine model, protocol framing summary,
  MQTT topic map, systemd setup notes.
- **`CONSISTENCY.md`**: second-pass review — 10 fixed issues (L1–L10) and 4
  previously deferred issues (R1–R4), each with fix description and test reference.
- **`README.md`** improvements: `ExecStartPre=/bin/sleep 20` with explanation,
  MAC address warning before registration, corrected door/lock topic values,
  corrected climate topic names, HA discovery behaviour clarified, flags table,
  Testing section, Troubleshooting section (bad-sum loop, entities unavailable,
  connection closed).

### Added — Scripts

- **`scripts/phev_wifi_monitor.sh`**: WiFi watchdog for Raspberry Pi deployments
  *(addresses [#34](https://github.com/buxtronix/phev2mqtt/issues/34))*:
  monitors the car's WiFi connection every 15 minutes; reconnects via `nmcli` if
  disconnected; restarts `phev2mqtt` if the TCP session is missing or has been
  silent for 5 minutes. Uses a lock file to prevent parallel runs.
- **`scripts/phev_conditional_reboot.sh`**: daily maintenance reboot
  *(contributes to [#34](https://github.com/buxtronix/phev2mqtt/issues/34))*:
  defers until the car is absent, then reboots once per calendar day to reset the
  WiFi driver. Runs at `:02` and `:32` via root cron, staggered from the watchdog.
  Shares the same lock file to prevent concurrent execution.

Both scripts require configuring `NETWORK_NAME` and (for the watchdog)
`NETWORK_PASSWORD` at the top of the file before deploying.

---

*For upstream changes prior to `c698b3a`, see the
[buxtronix/phev2mqtt](https://github.com/buxtronix/phev2mqtt) history.*
