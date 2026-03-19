#!/bin/sh
set -e

CREDENTIALS_FILE="${REGISTRY_MGR_CREDENTIALS_FILE:-/run/secrets/registry_credentials}"
GC_ENABLED="${GC_ENABLED:-true}"
GC_TIME="${GC_TIME:-03:00}"
REGISTRY_PID_FILE="/var/run/registry.pid"

# ---------------------------------------------------------------------------
# Parse credentials and generate htpasswd
# ---------------------------------------------------------------------------
if [ ! -f "$CREDENTIALS_FILE" ]; then
    echo "ERROR: credentials file not found at $CREDENTIALS_FILE" >&2
    exit 1
fi

line=$(cat "$CREDENTIALS_FILE" | tr -d '\n\r')
REGISTRY_USER="${line%%:*}"
REGISTRY_PASS="${line#*:}"

if [ -z "$REGISTRY_USER" ] || [ -z "$REGISTRY_PASS" ]; then
    echo "ERROR: invalid credentials file format (expected username:password)" >&2
    exit 1
fi

mkdir -p /auth
htpasswd -Bbn "$REGISTRY_USER" "$REGISTRY_PASS" > /auth/htpasswd
echo "Credentials loaded for user: $REGISTRY_USER"

# ---------------------------------------------------------------------------
# Keep-alive loop: starts the registry and restarts it on unexpected exit.
# ---------------------------------------------------------------------------
run_registry() {
    while true; do
        echo "[registry] starting"
        /bin/registry serve /etc/docker/registry/config.yml &
        echo $! > "$REGISTRY_PID_FILE"
        wait $(cat "$REGISTRY_PID_FILE") 2>/dev/null || true
        echo "[registry] exited unexpectedly, restarting in 2s"
        sleep 2
    done
}

# ---------------------------------------------------------------------------
# Calculate seconds until next occurrence of GC_TIME (HH:MM, 24h).
# Uses expr to force decimal interpretation — BusyBox $(( )) treats leading
# zeros as octal, which breaks values like 08 and 09.
# ---------------------------------------------------------------------------
seconds_until() {
    target_h=$(expr 0 + "${1%%:*}")
    target_m=$(expr 0 + "${1##*:}")

    now_h=$(expr 0 + "$(date +%H)")
    now_m=$(expr 0 + "$(date +%M)")
    now_s=$(expr 0 + "$(date +%S)")

    now_total=$(( now_h * 3600 + now_m * 60 + now_s ))
    target_total=$(( target_h * 3600 + target_m * 60 ))

    if [ "$target_total" -gt "$now_total" ]; then
        echo $(( target_total - now_total ))
    else
        echo $(( 86400 - now_total + target_total ))
    fi
}

# ---------------------------------------------------------------------------
# Garbage collection
# ---------------------------------------------------------------------------
run_gc() {
    echo "[gc] starting garbage collection at $(date)"

    # Kill the keep-alive loop then the registry process
    kill "$KEEPER_PID" 2>/dev/null || true
    kill "$(cat "$REGISTRY_PID_FILE")" 2>/dev/null || true
    wait "$KEEPER_PID" 2>/dev/null || true

    echo "[gc] running registry garbage-collect (readonly mode)"
    REGISTRY_STORAGE_MAINTENANCE_READONLY_ENABLED=true \
        /bin/registry garbage-collect /etc/docker/registry/config.yml --delete-untagged

    echo "[gc] garbage collection complete at $(date)"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
trap 'kill "$KEEPER_PID" 2>/dev/null; kill "$(cat "$REGISTRY_PID_FILE" 2>/dev/null)" 2>/dev/null; exit 0' TERM INT

if [ "$GC_ENABLED" = "true" ]; then
    while true; do
        run_registry &
        KEEPER_PID=$!

        sleep_secs=$(seconds_until "$GC_TIME")
        echo "[gc] next garbage collection in ${sleep_secs}s (at ${GC_TIME})"
        sleep "$sleep_secs"

        run_gc
    done
else
    echo "[gc] garbage collection disabled"
    run_registry &
    KEEPER_PID=$!
    wait "$KEEPER_PID"
fi
