#!/system/bin/sh
set -eu
ROOT=${ROOT:?}
test "$(/data/adb/magisk/busybox id -u)" = 0
test "$(stat -c %u:%a "$ROOT")" = 0:700
test "$(cat "$ROOT/.owner")" = "$(cat "$ROOT/.lock")"
test -f "$ROOT/initial-state"
