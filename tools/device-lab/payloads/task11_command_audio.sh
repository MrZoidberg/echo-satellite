#!/system/bin/sh
# Runs only from a prepared, token-owned device-lab session. The human speaks
# the scheduled trials; gateway logs and the opt-in gateway WAV provide the
# authoritative turn outcomes.
set -eu

ROOT=${ROOT:-${0%/*}}
: "${ROOT:?device-lab ROOT is required}"
ECHOD=${DIAGNOSTIC:?device-lab DIAGNOSTIC is required}
BB=/data/adb/magisk/busybox
CONFIG=/data/local/etc/echo-satellite/echod.ini
OUT="$RESULTS/task11-command-audio"
SCHEDULE="$OUT/schedule.txt"

test -x "$ECHOD" && test -x "$BB" && test -f "$CONFIG"
mkdir -p "$OUT" && chmod 700 "$OUT"
note() { "$BB" printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$1" >>"$SCHEDULE"; }
wait_phase() { name=$1 seconds=$2; note "$name begin seconds=$seconds"; "$BB" printf 'TASK11 %s begin seconds=%s\n' "$name" "$seconds"; sleep "$seconds"; note "$name end"; "$BB" printf 'TASK11 %s end\n' "$name"; }
stop_agent() {
  test -n "${agent_pid:-}" || return 0
  kill -TERM "$agent_pid" 2>/dev/null || true
  tries=0
  while kill -0 "$agent_pid" 2>/dev/null && test "$tries" -lt 50; do
    sleep 0.1
    tries=$((tries + 1))
  done
  if kill -0 "$agent_pid" 2>/dev/null; then
    "$BB" printf 'staged agent did not exit after TERM\n' >&2
    return 1
  fi
  wait "$agent_pid" 2>/dev/null || true
}
trap 'stop_agent; exit 130' HUP INT TERM

note "task11 profile=dot-gen2-qualified-v1 raw_audio=disabled start"
"$ECHOD" --config "$CONFIG" --config-state "$ROOT/config.json" \
  --pairing-state "$ROOT/paired-gateway.json" \
  --disable-updates \
  --conditioning-profile dot-gen2-qualified-v1 --log-file "$OUT/echod.jsonl" \
  --log-format json >"$OUT/echod.stdout" 2>&1 &
agent_pid=$!
"$BB" printf '%s\n' "$agent_pid" >"$ROOT/task11-echod.pid"
note "agent started"

# Speak "okay nabu" once in each six-second window. Count accepted local wakes
# from the paired gateway's `device turn started` records, not this schedule.
for trial in 01 02 03 04 05 06 07 08 09 10 11 12 13 14 15 16 17 18 19 20; do
  wait_phase "wake-$trial say_okay_nabu" 6
done

# Play ordinary music or room audio for the entire window. There must be no
# gateway `device turn started` record with trigger=wake.
wait_phase "idle_music no_speech_to_device" 900

# In each window say the wake phrase in the first five seconds, then speak
# quietly continuously until the window ends, never pausing for 1.5 seconds.
for trial in 01 02 03; do
  wait_phase "continuous_quiet-$trial wake_then_continuous_speech" 66
done

# In each window say the wake phrase, issue one quiet command, then remain
# silent. Record speech-end and gateway audio.stop times in the host worksheet.
for trial in 01 02 03; do
  wait_phase "endpointed-$trial wake_command_then_silence" 12
done

# Press the Action button at the beginning of each window and remain silent.
for trial in 01 02 03; do
  wait_phase "no_speech-$trial action_button_then_silence" 5
done

note "task11 operator schedule complete"
"$BB" awk '
  BEGIN { print "{"; print "  \"profile\": \"dot-gen2-qualified-v1\","; print "  \"raw_audio\": \"not_retained_on_device\","; print "  \"schedule\": \"operator trials completed; verify gateway records\""; print "}" }
' >"$OUT/summary.json"
rm -f "$OUT/echod.stdout" "$OUT/echod.jsonl"
stop_agent
trap - HUP INT TERM
"$BB" printf '%s\n' '{"artifacts":["task11-command-audio/schedule.txt","task11-command-audio/summary.json"]}' >"$RESULTS/manifest.json"
