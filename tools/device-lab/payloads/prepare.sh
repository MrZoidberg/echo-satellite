#!/system/bin/sh
set -eu
ROOT=${ROOT:?}
BB=/data/adb/magisk/busybox
test "$("$BB" id -u)" = 0
test "$("$BB" stat -c %u:%a "$ROOT")" = 0:700
test "$("$BB" stat -c %u:%a "$ROOT/.owner")" = 0:600
test "$("$BB" cat "$ROOT/.owner")" = "$("$BB" cat "$ROOT/.lock")"
STATE="$ROOT/initial-state"
test ! -e "$STATE" || exit 65
{
  "$BB" printf 'prepare_started=1\n'
  "$BB" sha256sum /data/local/bin/echod | "$BB" awk '{print "agent_digest=" $1}'
  "$BB" cat /proc/sys/kernel/random/boot_id | "$BB" sed 's/^/boot_id=/'
  getprop init.svc.ledcontroller | "$BB" sed 's/^/ledcontroller=/'
  getprop init.svc.mdnsd | "$BB" sed 's/^/mdnsd=/'
  "$BB" cat /sys/bus/i2c/devices/0-003f/boot_animation 2>/dev/null | "$BB" sed 's/^/boot_animation=/' || true
  "$BB" cat /sys/class/gpio/gpio444/value 2>/dev/null | "$BB" sed 's/^/gpio444=/' || true
  test -e /sys/class/gpio/gpio444 || echo gpio444_exported=0
  test ! -e /sys/class/gpio/gpio444 || echo gpio444_exported=1
  "$BB" cat /sys/class/gpio/gpio444/direction 2>/dev/null | "$BB" sed 's/^/gpio444_direction=/' || true
} >"$STATE"
chown root:root "$STATE"
chmod 600 "$STATE"
sync
sync
