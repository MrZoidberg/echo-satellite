#!/system/bin/sh
# Runs only from a prepared, token-owned device-lab session. Gateway records
# and optional gateway-side WAVs, never this schedule, are the evidence.
set -eu

ROOT=${ROOT:-${0%/*}}
: "${ROOT:?device-lab ROOT is required}"
ECHOD=${DIAGNOSTIC:?device-lab DIAGNOSTIC is required}
BB=/data/adb/magisk/busybox
CONFIG=/data/local/etc/echo-satellite/echod.ini
: "${GATEWAY_URL:?device-lab --gateway-url is required}"
PROFILE=${CONDITIONING_PROFILE:-dot-gen2-qualified-v1}
WAKE_TRIALS=${WAKE_TRIALS:-20}
IDLE_SECONDS=${IDLE_SECONDS:-900}
OUT="$RESULTS/command-audio-qualification"
SCHEDULE="$OUT/schedule.txt"
LED_ROOT=/sys/bus/i2c/devices/0-003f
CYAN_GREEN=006e96
YELLOW=b4b400
CLEAR=000000000000000000000000000000000000

test -x "$ECHOD" && test -x "$BB" && test -f "$CONFIG"
case "$WAKE_TRIALS:$IDLE_SECONDS" in *[!0-9:]*|:*) exit 2 ;; esac
mkdir -p "$OUT" && chmod 700 "$OUT"
note() { "$BB" printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$1" >>"$SCHEDULE"; }
stop_cue() {
  test -n "${cue_pid:-}" || return 0
  kill -TERM "$cue_pid" 2>/dev/null || true
  wait "$cue_pid" 2>/dev/null || true
  "$BB" printf '%s\n' "$CLEAR" >"$LED_ROOT/frame" || return 1
  cue_pid=
}
ring_cue() {
  color=$1
  test -w "$LED_ROOT/frame" || { "$BB" printf '%s\n' 'LED cue unavailable' >&2; return 1; }
  frame=
  i=0
  while test "$i" -lt 12; do frame="${frame}${color}"; i=$((i + 1)); done
  (while :; do "$BB" printf '%s\n' "$frame" >"$LED_ROOT/frame" || exit 0; sleep 0.05; done) &
  cue_pid=$!
}
assert_agent() {
  test -n "${agent_pid:-}" && kill -0 "$agent_pid" 2>/dev/null &&
    test "$("$BB" readlink "/proc/$agent_pid/exe")" = "$ECHOD" || {
      "$BB" printf '%s\n' 'staged diagnostic agent is not running' >&2
      return 1
    }
}
wait_phase() {
  name=$1 seconds=$2 cue=${3:-}
  assert_agent
  note "$name begin seconds=$seconds cue=${cue:-none}"
  "$BB" printf 'COMMAND_AUDIO %s begin seconds=%s cue=%s\n' "$name" "$seconds" "${cue:-none}"
  test -z "$cue" || ring_cue "$cue"
  sleep "$seconds"
  stop_cue
  assert_agent
  note "$name end"
  "$BB" printf 'COMMAND_AUDIO %s end\n' "$name"
}
stop_agent() {
  test -n "${agent_pid:-}" || return 0
  assert_agent || return 1
  kill -TERM "$agent_pid"
  tries=0
  while kill -0 "$agent_pid" 2>/dev/null && test "$tries" -lt 50; do sleep 0.1; tries=$((tries + 1)); done
  if kill -0 "$agent_pid" 2>/dev/null; then "$BB" printf '%s\n' 'staged agent did not exit after TERM' >&2; return 1; fi
  wait "$agent_pid" 2>/dev/null || true
  rm -f "$ROOT/command-audio-echod.pid"
  agent_pid=
}
trap 'stop_cue; stop_agent; exit 130' HUP INT TERM

note "command-audio profile=$PROFILE raw_audio=disabled start"
"$ECHOD" --config "$CONFIG" --config-state "$ROOT/config.json" \
  --pairing-state "$ROOT/paired-gateway.json" --discovery disabled \
  --gateway-url "$GATEWAY_URL" --disable-updates --conditioning-profile "$PROFILE" \
  --log-file "$OUT/echod.jsonl" --log-format json >"$OUT/echod.stdout" 2>&1 &
agent_pid=$!
"$BB" printf '%s\n' "$agent_pid" >"$ROOT/command-audio-echod.pid"
wait_phase "telemetry-settle health.telemetry.v1" 35

trial=1
while test "$trial" -le "$WAKE_TRIALS"; do
  wait_phase "wake-$trial say_okay_nabu" 6 "$CYAN_GREEN"
  trial=$((trial + 1))
done
wait_phase "idle_music no_speech_to_device" "$IDLE_SECONDS"
for trial in 1 2 3; do wait_phase "continuous_quiet-$trial wake_then_continuous_speech" 66 "$CYAN_GREEN"; done
for trial in 1 2 3; do wait_phase "endpointed-$trial wake_command_then_silence" 12 "$CYAN_GREEN"; done
for trial in 1 2 3; do wait_phase "no_speech-$trial action_button_then_silence" 5 "$YELLOW"; done

note "command-audio operator schedule complete"
"$BB" printf '%s\n' "{\"profile\":\"$PROFILE\",\"raw_audio\":\"not_retained_on_device\",\"schedule\":\"operator trials completed; verify gateway records\"}" >"$OUT/summary.json"
rm -f "$OUT/echod.stdout" "$OUT/echod.jsonl"
stop_agent
stop_cue
trap - HUP INT TERM
"$BB" printf '%s\n' '{"artifacts":["command-audio-qualification/schedule.txt","command-audio-qualification/summary.json"]}' >"$RESULTS/manifest.json"
