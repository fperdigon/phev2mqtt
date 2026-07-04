# phev2mqtt - Mitsubishi Outlander PHEV to MQTT gateway

Utility to interact with a Mitsubishi Outlander PHEV via the Wifi remote
control protocol.

Inspired by https://github.com/phev-remote/ but written entirely in Go.

For further hacking, read the [protocol documentation](protocol/README.md).

Tested against a MY18 vehicle.

![Home Assistant Screenshot](phev-ha.png)

## Supported functionality

 * MQTT proxy to Phev
 * Home Assistant discovery
 * Register client to car
 * Fetch battery, charge, door, light status
 * Set lights and charge enable
 * Near-instant response to commands
 * *Only tested on a MY18 Phev*

Also includes some debugging utilities, and a vehicle emulator.

## Requirements

 * Go compiler

## Licence, etc

Licenced under the GPLv2.

Copyright 2021 Ben Buxton <bbuxton@gmail.com>

Contributions and PRs are welcome.

## Getting started.

### Compiling

#### Install Go

 * Download and install the latest [Go compiler](https://golang.org/dl/)
   * Your distro packager may have a version thats too old
   * For raspbian choose the ARMv6 release

#### Install PCAP dev libraries

 * Optionally, you may want to have libpcap-dev package installed (if building with `-tags pcap`.)

#### Download, extract, and compile phev2mqtt

 * Download the phev2mqtt archive
 * Extract it
 * Go into its the top level directory and run `go build`
 * Verify it runs with `./phev2mqtt -h`

### Connecting to the vehicle.

#### Configure Wifi client on system running mqtt2phev

On your computer running the phev2mqtt tools, configure a new Wifi connection to the
car's SSID,

#### Register the client to the car

Follow the [Mitsubishi instructions](https://www.mitsubishi-motors.com/en/products/outlander_phev/app/remote/)
to find the Wifi credentials provided with the car.

Verify that your Wifi connection to the car is established - your local IP address
should be 192.168.8.47.

Follow the [Mitsubishi instructions](https://www.mitsubishi-motors.com/en/products/outlander_phev/app/remote/)
and put the car into registration mode ("Setup Your Vehicle"). You may need to
re-establish the Wifi connection.

Register by running `phev2mqtt client register` and you should shortly see a message
indicating successful registration.

#### Testing the tool

Once connected to the car, you can sniff for messages by running *phev2mqtt client watch*.
The phone client needs to be disconnected for this to work.
You'll see a bunch of data go by - some of those will be decoded into readable
messages such as charge and AC status.

### MQTT Gateway

The primary feature of this code is to run as a proxy between the car and
MQTT. Registers with car status are sent to MQTT, both as raw register
values and decoded functional values. Commands sent on MQTT topics can
be used to control certain aspects of the vehicle.

Start the MQTT gateway with:

`./phev2mqtt client mqtt --mqtt_server tcp://<your_mqtt_address>:1883/ [--mqtt_username <username>] [--mqtt_password <password>]`

Key optional flags:

| Flag | Default | Description |
|---|---|---|
| `--mqtt_client_id` | `phev2mqtt` | MQTT client ID — must be unique per broker if running multiple instances |
| `--mqtt_topic_prefix` | `phev` | Prefix for all published topics |
| `--mqtt_disable_register_set_command` | `false` | Disable raw register writes via MQTT (safer for production) |
| `--ha_discovery` | `true` | Enable/disable Home Assistant auto-discovery |
| `--update_interval` | `5m` | How often to request a full state refresh from the car |
| `--wifi_restart_command` | *(empty)* | Shell command to restart WiFi if car connection is lost (empty = disabled) |

The following topics are published:

| Topic/prefix | Description |
|---|---|
| phev/register/[register] | Raw values of each register, as hex strings |
| phev/available | Wifi connection status to car. *online* or *offline* |
| phev/battery/level | Current drive battery level as a percent |
| phev/climate/state | Combined AC state. *off*, *cool*, *heat*, *windscreen*, or *terminated* |
| phev/climate/[mode] | Per-mode state. Modes are *cool*, *heat*, *windscreen*, each *on* or *off* |
| phev/charge/charging | Whether the battery is charging. *on* or *off* |
| phev/charge/plug | If the charging plug is *unplugged* or *connected*. |
| phev/charge/remaining | Minutes left, if charging. |
| phev/door/locked | Whether the car is locked. *closed* = locked, *open* = unlocked |
| ~~phev/door/front_left~~ | State of doors. *closed* or *open* |
| ~~phev/door/front_right~~ | State of doors. *closed* or *open* |
| phev/door/front_passenger | State of doors. *closed* or *open* |
| phev/door/driver | State of doors. *closed* or *open* |
| phev/door/rear_left | State of doors. *closed* or *open* |
| phev/door/rear_right | State of doors. *closed* or *open* |
| phev/door/bonnet | State of doors. *closed* or *open* |
| phev/door/boot | State of doors. *closed* or *open* |
| phev/lights/parking | Parking lights. *on* or *off* |
| phev/lights/head | Head lights. *on* or *off* |
| phev/lights/hazard | Hazard lights. *on* or *off* |
| phev/lights/interior | Interior lights. *on* or *off* |
| phev/vin | Discovered VIN of the car |
| phev/registrations | Number of wifi clients registered to the car |

The following topics are subscribed to and can be used to change state on the car:

| Topic/prefix | Description |
|---|---|
| phev/set/register/[register] | Set register 0x[register] to value 0x[payload] |
| phev/set/parkinglights | Set parking lights *on* or *off* |
| phev/set/headlights | Set head lights *on* or *off* |
| phev/set/cancelchargetimer | Cancel charge timer (any payload) |
| phev/set/climate/[mode] | Set ac/climate state (cool/heat/windscreen/off) for [payload] (10[on]/20/30) |
| phev/set/climate/state | `[payload]=reset` clears "terminated" state |
| phev/connection | Change car connection state to (on/off/restart) |

#### Home Assistant discovery

The client supports [Home Assistant MQTT Discovery](https://www.home-assistant.io/docs/mqtt/discovery/) by default.

Entities are registered automatically on first connection and re-registered after every MQTT broker restart — no manual restart needed. Search for "phev" in your entity list; your car should also appear as a device in the Devices tab.

You can disable this with `--ha_discovery=false` or change the discovery prefix, the default is `--ha_discovery_prefix=homeassistant`.

#### Raspbian setup with auto-start

It's useful to have the tool auto-start when running on e.g a Raspberry Pi. The following
describes how to set this up.

> **Important — MAC address:** The car stores the MAC address of the WiFi client
> that registered with it. It will **only accept connections from that exact MAC**.
> If your Pi uses MAC randomisation, you must disable it. If you need to use a
> specific MAC (e.g. when routing through a gateway), set it up *before* running
> `phev2mqtt client register` so the car stores the right address.

- Edit or add to `/etc/systemd/network/00-default.link` with the following:

```
[Match]
# This should be the 'real' (default) mac address of the Pi's wireless interface.
MACAddress=b8:27:eb:50:c0:52

[Link]
# This should be the MAC address to use to connect to the car, per above.
MACAddress=ee:4d:ec:de:7a:91
NamePolicy=kernel database onboard slot path

```

- Add the car's Wifi info to `/etc/wpa_supplicant/wpa_supplicant.conf`:

```
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
country=AU

network={
	ssid="REMOTE45bhds"
	scan_ssid=1
	psk="blahblahbla12314"
}

```

- Add the following to `/etc/systemd/system/phev2mqtt.service`, updating the MQTT address to
suit your setup:

```
[Unit]
Description=phev2mqtt PHEV to MQTT gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=exec
# Wait 20s before connecting — prevents a "bad sum" loop when the Pi
# reconnects too quickly after a reboot before the car's TCP stack is ready.
ExecStartPre=/bin/sleep 20
ExecStart=/usr/local/bin/phev2mqtt client mqtt \
    --mqtt_server tcp://192.168.0.88:1883 \
    --mqtt_username <mqtt_username> \
    --mqtt_password <mqtt_password>

Restart=always
RestartSec=5s

SyslogIdentifier=phev2mqtt
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

> **Important:** The `ExecStartPre=/bin/sleep 20` delay is essential. Without it,
> if the Pi reboots while the car is parked nearby, phev2mqtt reconnects before
> the car's session has fully reset, causing the car to repeatedly reject frames
> with "bad checksum" errors for hours.

- Copy the `phev2mqtt` binary to /usr/local/bin and make sure it's executable.

- Start the service with `sudo systemctl start phev2mqtt.service`

- Enable the service to run at boot, with `sudo systemctl enable phev2mqtt.service`.

- Restart the Pi and verify that it can connect to the car. Also run `ifconfig` and check
that the `wlan0` interface has the correct mac address. You should also see this interface
have the IP address `192.168.8.47`.

- Verify that the phev2mqtt service is communicating with the car, by checking
the logs: `sudo journalctl -f -u phev2mqtt -o cat`


## Running the tests

The test suite covers the protocol framing/decoding layer and the MQTT bridge logic.

```bash
go test ./...
```

To see per-test output:

```bash
go test ./... -v
```

To run only protocol tests:

```bash
go test ./protocol/... -v
```

To run only MQTT bridge tests:

```bash
go test ./cmd/... -v
```

The tests do not require a real car or MQTT broker — all protocol tests use
pre-recorded hex frames as test vectors, and the MQTT tests exercise the
logic directly against the internal structs.


## Troubleshooting

### Entities unavailable in Home Assistant after HA or broker restart

Home Assistant MQTT discovery messages are now published with `retain=true` and
are automatically re-sent after every broker reconnect. If entities still go
unavailable, check:
- The MQTT broker is reachable (`phev/available` topic should show `online`)
- `phev2mqtt` is running: `sudo systemctl status phev2mqtt`

### "Bad sum" loop — car not connecting after Pi reboot

Symptom: logs show repeated `Bad sum` messages; the car never transitions to
normal register updates.

Cause: the Pi reconnected to the car too quickly after a reboot, before the
car's TCP session fully reset.

Fix: ensure `ExecStartPre=/bin/sleep 20` is in your systemd service file (see
setup above). A 20-second delay before first connection is enough to avoid this.

### Stops working after a day / needs manual restart

If phev2mqtt loses connection to the car's WiFi and cannot recover, check:
- The car's WiFi goes to sleep when the car is locked and idle for a while
  — this is normal. phev2mqtt will reconnect when the car wakes (e.g. when
  you open the door).
- If connection never recovers, use `--wifi_restart_command` to automatically
  cycle the WiFi interface on the Pi after a configurable idle period.

### "Connection closed" immediately after connecting

- Ensure the Pi is registered with the car (`phev2mqtt client register`)
- Verify only one client is connecting at a time (the car only allows one)
- Check that no other app (e.g. the Mitsubishi phone app) is connected

### Sniffing the official client

Further development of this library can be done with a packet dump of the official
Mistubishi app.

A number of sniffer apps for phones are available for this. Two that the author have
used are *Packet Capture* and *PCAP Remote*. These do not require root access, yet
can successfully sniff the traffic into PCAP files for further analysis.

*Packet Capture* can save the PCAP files to your local phone storage which you can
then extract off the phone.

*PCAP Remote* is a little more involved, but allows for live sniffing of the traffic.

Once you have downloaded the PCAP file(s) from the phone, you can analyse them with
the command *phev2mqtt decode pcap <filename>*. First build a `phev2mqtt` with pcap features:
`go build -tags pcap`; you will need libpcap for this. Adjust the verbosity level (`-v`) between
`info`, `debug` and `trace` for more details.

Additionally, the flag `--latency` will use the PCAP packet timestamps to decode
the packets with original timings which can help pinpoint app events.

You can also specify *tcp:<host>:<port>* which will connect to that host/port
over TCP and decode that traffic - useful when live sniffing to a TCP service.

### Vehicle emulator

There is an emulator built in which can be used to test functionality without needing
a real car (and also reduces risk of putting your car into weird states).

Start it with `phev2mqtt emulator` and then you can point a client at it.

The official app will always try to connect to IP `192.168.8.46`, so you'll need
to ensure you run `phev2mqtt` on a machine with this IP and which you can
reach via WIFI. The author uses a Raspberry Pi setup as an AP (using hostapd)
and runs `phev2mqtt` on it, though you could also tunnel the TCP connection to
a dev machine.

The app should successfully be able to register with the emulator (it might take
a couple of goes).

Any settings sent by the app won't actually change state for now, but it can
be useful for sniffing the app.

## Auxiliary Scripts

The `scripts/` directory contains two maintenance scripts for Raspberry Pi deployments.
Both use `systemctl stop phev2mqtt && sleep 5 && systemctl start phev2mqtt` (never a direct
restart) so the car's WiFi driver has time to settle between operations.

**Before deploying either script**, edit the configuration variables at the top of the file:
- `NETWORK_NAME` — your car's WiFi SSID (format `REMOTE<id>`, visible in your car's WiFi setup menu)
- `NETWORK_PASSWORD` — the WiFi password from your car's setup screen

### scripts/phev_wifi_monitor.sh

WiFi watchdog that recovers from common failure modes automatically.

What it does:
- If the car's SSID is visible but not connected → reconnects via nmcli
- If connected but the car's gateway (`192.168.8.46`) is unreachable → reconnects
- If connected and reachable but phev2mqtt has no active TCP session → restarts the service
- If a TCP session exists but no data has been received in 5 minutes → restarts the service

Uses a lock file (`/tmp/phev_wifi_monitor.lock`) to prevent parallel runs.

**Deploy:**
```sh
sudo cp scripts/phev_wifi_monitor.sh /home/pi/phev_wifi_monitor.sh
# Edit NETWORK_NAME and NETWORK_PASSWORD at the top
sudo chmod +x /home/pi/phev_wifi_monitor.sh
# Add to root crontab (sudo crontab -e):
# */15 * * * * /home/pi/phev_wifi_monitor.sh
```

### scripts/phev_conditional_reboot.sh

Daily maintenance reboot that only fires when the car is not connected.

Rebooting the Pi once per day resets the WiFi driver and clears accumulated kernel
state that can cause silent connection failures after many days of uptime. The reboot
is deferred in 30-minute increments until the car is absent, so it never interrupts
an active session.

Also uses the same lock file as the watchdog to prevent concurrent execution.

**Deploy:**
```sh
sudo cp scripts/phev_conditional_reboot.sh /usr/local/bin/phev-conditional-reboot.sh
# Edit NETWORK_NAME at the top
sudo chmod +x /usr/local/bin/phev-conditional-reboot.sh
# Add to root crontab (sudo crontab -e), staggered from the watchdog:
# 2,32 * * * * /usr/local/bin/phev-conditional-reboot.sh
```

**Logs** for both scripts are written to `/var/log/phev_wifi_monitor.log`.
