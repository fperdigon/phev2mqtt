#!/bin/bash
# Daily maintenance reboot for phev2mqtt — runs every 30 minutes via cron.
#
# Reboots the Raspberry Pi once per calendar day, but ONLY when the car is
# not connected. This resets the WiFi driver and clears any kernel state
# that accumulates over time, improving long-term reliability.
#
# The reboot is deferred (retried every 30 min) until the car disconnects,
# so it never interrupts an active session.
#
# Uses the same lock file as phev_wifi_monitor.sh to prevent concurrent
# execution — if the watchdog is mid-reconnect, waits up to 120s.
#
# Cron entry (run as root, staggered to avoid firing at same time as watchdog):
#   2,32 * * * * /path/to/phev_conditional_reboot.sh
#
# Configuration: set NETWORK_NAME to match your car's WiFi SSID.

# SSID of your car's WiFi hotspot (format: REMOTE<id>, shown in your car's menu)
NETWORK_NAME="REMOTE<id>"

# Car's onboard hotspot gateway — default 192.168.8.46 for Mitsubishi Outlander PHEV
TARGET_IP=192.168.8.46

LOGFILE="/var/log/phev_wifi_monitor.log"
REBOOT_STAMP="/var/lib/phev-reboot-stamp"
LOCKFILE="/tmp/phev_wifi_monitor.lock"
TODAY=$(date +%Y-%m-%d)

log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') [reboot] $*" | tee -a "$LOGFILE"
}

# Only reboot once per calendar day
if [ -f "$REBOOT_STAMP" ] && [ "$(cat $REBOOT_STAMP)" = "$TODAY" ]; then
    exit 0
fi

# Wait for watchdog to finish if it is currently running (up to 120s)
exec 9>"$LOCKFILE"
if ! flock -w 120 9; then
    log "Watchdog still running after 120s — skipping reboot check."
    exit 0
fi

# Check car presence (OR: defer if EITHER NM connection is active OR ping succeeds)
car_connected=$(nmcli -t -f NAME,STATE connection show --active | grep -c "^$NETWORK_NAME:activated")
ping -c 3 -W 2 "$TARGET_IP" > /dev/null 2>&1
car_reachable=$?

if [ "$car_connected" -gt 0 ] || [ "$car_reachable" -eq 0 ]; then
    log "Car is connected — daily reboot deferred. Will retry in 30 min."
else
    log "Car not present — rebooting now to reset WiFi driver (daily maintenance)."
    echo "$TODAY" > "$REBOOT_STAMP"
    /sbin/reboot
fi
