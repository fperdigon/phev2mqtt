#!/bin/bash

# Reboot the RPi once per day when the car is not connected.
# Runs every 30 minutes via cron (staggered to :02 and :32 to avoid
# firing at the same second as the watchdog).
#
# Uses the same lock file as phev_wifi_monitor.sh to prevent concurrent
# execution — if the watchdog is mid-reconnect, we wait up to 120s for
# it to finish before checking car status.
#
# Car presence check uses OR logic: only reboots if BOTH the NM connection
# is inactive AND TARGET_IP is unreachable — a single glitch can't
# trigger an unwanted reboot.

# Load site-specific configuration (shared with phev_wifi_monitor.sh)
CONFIG_FILE="${PHEV_CONFIG:-/etc/phev/phev_wifi.env}"
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: config file not found: $CONFIG_FILE" >&2
    echo "Copy scripts/phev_wifi.env.example to $CONFIG_FILE and fill in values." >&2
    exit 1
fi
# shellcheck source=/dev/null
source "$CONFIG_FILE"

LOGFILE="${LOGFILE:-/var/log/phev_wifi_monitor.log}"
REBOOT_STAMP="/var/lib/phev-reboot-stamp"
LOCKFILE="${LOCKFILE:-/tmp/phev_wifi_monitor.lock}"
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
