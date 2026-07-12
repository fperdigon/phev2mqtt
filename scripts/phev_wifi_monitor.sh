#!/bin/bash

# Load site-specific configuration (credentials and endpoints — not in repo).
# Override the path by setting PHEV_CONFIG in the environment before calling.
CONFIG_FILE="${PHEV_CONFIG:-/etc/phev/phev_wifi.env}"
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: config file not found: $CONFIG_FILE" >&2
    echo "Copy scripts/phev_wifi.env.example to $CONFIG_FILE and fill in values." >&2
    exit 1
fi
# shellcheck source=/dev/null
source "$CONFIG_FILE"

# Operational defaults — override in the config file if needed
INTERFACE="${INTERFACE:-wlan0}"
LOGFILE="${LOGFILE:-/var/log/phev_wifi_monitor.log}"
LOCKFILE="${LOCKFILE:-/tmp/phev_wifi_monitor.lock}"
FAIL_COUNT_FILE="${FAIL_COUNT_FILE:-/tmp/phev_reconnect_fail_count}"
STALE_THRESHOLD_MS="${STALE_THRESHOLD_MS:-300000}"

log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') $*" | tee -a "$LOGFILE"
}

# Prevent parallel runs if a previous instance is still running
exec 9>"$LOCKFILE"
if ! flock -n 9; then
    log "Already running, exiting."
    exit 1
fi

reset_fail_count() {
    echo 0 > "$FAIL_COUNT_FILE"
}

get_fail_count() {
    cat "$FAIL_COUNT_FILE" 2>/dev/null || echo 0
}

# Reload the brcmfmac kernel module to clear a wedged SDIO firmware state.
# The chip can scan (passive) but gets stuck mid-authentication after a
# session drop; neither ip link, rfkill, nor nmcli can clear it — only a
# full driver/firmware reinit works.
# Sequence: unload brcmfmac_wcc (depends on brcmfmac) → unload brcmfmac →
# reload brcmfmac → disable power save (re-enabled by default on load) →
# rescan → bring connection up explicitly.
reload_driver() {
    log "Driver stuck after $(get_fail_count) consecutive failures — reloading brcmfmac module..."
    modprobe -r brcmfmac_wcc 2>/dev/null
    modprobe -r brcmfmac 2>/dev/null
    sleep 5
    modprobe brcmfmac 2>/dev/null
    sleep 8
    /usr/sbin/iw dev "$INTERFACE" set power_save off 2>/dev/null
    nmcli device wifi rescan ifname "$INTERFACE" 2>/dev/null
    sleep 3
    if nmcli connection up "$NETWORK_NAME" 2>/dev/null; then
        sleep 3
        log "Module reload succeeded — reconnected to $NETWORK_NAME. Restarting phev2mqtt..."
        systemctl stop phev2mqtt && sleep 5 && systemctl start phev2mqtt
        reset_fail_count
        return 0
    else
        log "Module reload did not restore connection."
        return 1
    fi
}

restart_wifi() {
    log "Reconnecting $INTERFACE to $NETWORK_NAME..."
    nmcli device disconnect "$INTERFACE" 2>/dev/null || true
    sleep 3

    # Primary: let NM autoconnect using the saved profile (reads PSK from
    # /etc/NetworkManager/system-connections/$NETWORK_NAME.nmconnection).
    # NM fires autoconnect within ~1s of disconnect.
    local i=0
    while [ $i -lt 10 ]; do
        sleep 3
        if nmcli -t -f NAME,STATE connection show --active | grep -q "^$NETWORK_NAME:activated"; then
            log "Reconnected to $NETWORK_NAME. Restarting phev2mqtt..."
            sleep 3
            systemctl stop phev2mqtt && sleep 5 && systemctl start phev2mqtt
            reset_fail_count
            return 0
        fi
        i=$((i+1))
    done

    # Fallback: explicit connect with password (used if saved profile is missing)
    log "Autoconnect did not fire — trying explicit connect..."
    if nmcli device wifi connect "$NETWORK_NAME" password "$NETWORK_PASSWORD" ifname "$INTERFACE" 2>/dev/null; then
        log "Reconnected (explicit) to $NETWORK_NAME. Restarting phev2mqtt..."
        sleep 3
        systemctl stop phev2mqtt && sleep 5 && systemctl start phev2mqtt
        reset_fail_count
        return 0
    else
        log "Failed to connect to $NETWORK_NAME."
        return 1
    fi
}

restart_if_stuck() {
    # Primary check: is there an active TCP session to the car at all?
    active_conn=$(ss -tn state established dst "$TARGET_IP:$TARGET_PORT" 2>/dev/null | grep -c "$TARGET_IP")
    if [ "$active_conn" -eq 0 ]; then
        log "WiFi up and car reachable, but phev2mqtt has no active TCP connection. Restarting..."
        systemctl stop phev2mqtt && sleep 5 && systemctl start phev2mqtt
        reset_fail_count
        return
    fi

    # Secondary check: is data actually flowing on that TCP session?
    # ss -ti reports lastrcv = ms since last byte received from the car.
    # If it exceeds the stale threshold the socket is alive but silent — restart.
    lastrcv=$(ss -ti state established dst "$TARGET_IP:$TARGET_PORT" 2>/dev/null | grep -oP 'lastrcv:\K[0-9]+' | head -1)
    if [ -n "$lastrcv" ] && [ "$lastrcv" -gt "$STALE_THRESHOLD_MS" ]; then
        log "TCP session exists but no data received from car in $((lastrcv/1000))s. Restarting phev2mqtt..."
        systemctl stop phev2mqtt && sleep 5 && systemctl start phev2mqtt
        reset_fail_count
        return
    fi

    log "All OK: connected to $NETWORK_NAME, $TARGET_IP reachable, phev2mqtt TCP session active."
    reset_fail_count
}

# Force a fresh scan before checking
/usr/sbin/iw dev "$INTERFACE" set power_save off 2>/dev/null
nmcli device wifi rescan ifname "$INTERFACE" 2>/dev/null
sleep 3

network_status=$(nmcli -t -f NAME,STATE connection show --active | grep "^$NETWORK_NAME:")

if [ -z "$network_status" ]; then
    available=$(nmcli device wifi list ifname "$INTERFACE" | grep "$NETWORK_NAME")
    if [ -n "$available" ]; then
        log "$NETWORK_NAME visible but not connected. Reconnecting..."
        if ! restart_wifi; then
            fail_count=$(( $(get_fail_count) + 1 ))
            echo "$fail_count" > "$FAIL_COUNT_FILE"
            log "Reconnect failed (consecutive failures: $fail_count)."
            # After 2 or 4 consecutive failures, try a module reload.
            # After 6 failures (2 reloads both failed), reboot.
            if [ "$fail_count" -eq 2 ] || [ "$fail_count" -eq 4 ]; then
                reload_driver
            elif [ "$fail_count" -ge 6 ]; then
                log "Module reload failed repeatedly — rebooting to clear stuck SDIO state."
                /sbin/reboot
            fi
        fi
    else
        log "$NETWORK_NAME not detected. Car may be out of range or off."
    fi
else
    if ! ping -c 3 -W 2 "$TARGET_IP" > /dev/null 2>&1; then
        log "Connected to $NETWORK_NAME but cannot reach $TARGET_IP. Reconnecting..."
        restart_wifi
    else
        restart_if_stuck
    fi
fi
