#!/system/bin/sh
set -eu
ROOT=${ROOT:?}
STATE="$ROOT/initial-state"
test "$(/data/adb/magisk/busybox id -u)" = 0
test -f "$STATE" || exit 65
. "$STATE"
BB=/data/adb/magisk/busybox
test "$("$BB" sha256sum /data/local/bin/echod | "$BB" awk '{print $1}')" = "$agent_digest" || exit 72
case "${ledcontroller:-}" in running) start ledcontroller;; stopped) stop ledcontroller;; esac
case "${mdnsd:-}" in running) start mdnsd;; stopped) stop mdnsd;; esac
test -z "${boot_animation:-}" || echo "$boot_animation" > /sys/bus/i2c/devices/0-003f/boot_animation
if test "${gpio444_exported:-0}" = 1 && test ! -e /sys/class/gpio/gpio444; then echo 444 > /sys/class/gpio/export; fi
test -z "${gpio444_direction:-}" || echo "$gpio444_direction" > /sys/class/gpio/gpio444/direction
test -z "${gpio444:-}" || echo "$gpio444" > /sys/class/gpio/gpio444/value
test -z "${boot_animation:-}" || test "$(cat /sys/bus/i2c/devices/0-003f/boot_animation)" = "$boot_animation"
test -z "${gpio444:-}" || test "$(cat /sys/class/gpio/gpio444/value)" = "$gpio444"
test -z "${gpio444_direction:-}" || test "$(cat /sys/class/gpio/gpio444/direction)" = "$gpio444_direction"
if test "${gpio444_exported:-0}" = 0 && test -e /sys/class/gpio/gpio444; then echo 444 > /sys/class/gpio/unexport; fi
for pidfile in "$ROOT"/*.pid; do
  test -e "$pidfile" || continue
  pid=$("$BB" cat "$pidfile")
  if kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || exit 71
    tries=0
    while kill -0 "$pid" 2>/dev/null && test "$tries" -lt 50; do sleep 0.1; tries=$((tries + 1)); done
    kill -0 "$pid" 2>/dev/null && exit 71
  fi
  rm -f "$pidfile"
done
rm -rf "$ROOT"
sync
