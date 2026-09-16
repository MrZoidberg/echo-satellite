#!/system/bin/sh
# Run only from a prepared, token-owned device-lab session.
set -eu

ROOT=${ROOT:-${0%/*}}
: "${ROOT:?device-lab ROOT is required}"
ECHOCTL=${DIAGNOSTIC:?device-lab DIAGNOSTIC is required}
BB=/data/adb/magisk/busybox
OUT="$RESULTS/task10-front-550mm"
SCHEDULE="$OUT/schedule.txt"
test -x "$ECHOCTL" && test -x "$BB"
mkdir -p "$OUT" && chmod 700 "$OUT"
note() { "$BB" printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$1" >>"$SCHEDULE"; }
cue() { note "cue thinking"; "$ECHOCTL" led test --state thinking --seconds 3 --clear >>"$SCHEDULE" 2>&1; }
record() {
  name=$1; note "capture $name start"
  "$ECHOCTL" mic record --seconds 10 --out "$OUT/$name.wav" --channels all --health-out "$OUT/$name.health.json" --print-levels >"$OUT/$name.record.txt"
  note "capture $name complete"
}
record_listening() {
  name=$1; note "capture $name listening start"
  "$ECHOCTL" led test --state listening --seconds 12 --clear >>"$SCHEDULE" 2>&1 & led_pid=$!
  sleep 1
  if ! record "$name"; then kill "$led_pid" 2>/dev/null || true; wait "$led_pid" 2>/dev/null || true; exit 1; fi
  wait "$led_pid"; note "capture $name listening complete"
}
score_silence() {
  grep -q '^  "xruns": 0,$' "$OUT/silence.health.json"
  grep -q '^  "dropped_frames": 0,$' "$OUT/silence.health.json"
  "$ECHOCTL" mic scorecard --input "$OUT/silence.wav" --capture-health "$OUT/silence.health.json" --position front --distance-mm 550 --condition silence --out "$OUT/silence.scorecard.json" >"$OUT/silence.scorecard.txt"
}
compare() {
  condition=$1 noise=$2 speech=$3
  "$ECHOCTL" mic compare --input "$OUT/$speech.wav" --noise "$OUT/$noise.wav" --capture-health "$OUT/$speech.health.json" --noise-capture-health "$OUT/$noise.health.json" --position front --distance-mm 550 --condition "$condition" --out "$OUT/$speech.comparison.json" >"$OUT/$speech.comparison.txt"
  "$BB" awk '
    /"clipping_fraction":/ { value=$2; gsub(/[, ]/, "", value); if (value + 0 > 0.001) bad=1; clips++ }
    /"max_block_duration_ns":/ { value=$2; gsub(/[, ]/, "", value); if (value + 0 > 80000000) bad=1; cadence++ }
    END { exit (clips == 0 || cadence == 0 || bad) }
  ' "$OUT/$speech.comparison.json"
}
note "Task 10 front distance_mm=550 sequence start"
record silence; cue; record room-noise-normal; cue; record_listening normal-speech; cue
record room-noise-quiet; cue; record_listening quiet-speech; cue
record room-noise-loud; cue; record_listening loud-speech; cue
score_silence
compare normal-speech room-noise-normal normal-speech
compare quiet-speech room-noise-quiet quiet-speech
compare loud-speech room-noise-loud loud-speech
rm -f "$OUT"/*.health.json "$OUT"/*.record.txt "$OUT"/*.comparison.txt "$OUT"/silence.scorecard.txt
"$BB" printf '%s\n' '{"artifacts":["task10-front-550mm/schedule.txt","task10-front-550mm/silence.scorecard.json","task10-front-550mm/normal-speech.comparison.json","task10-front-550mm/quiet-speech.comparison.json","task10-front-550mm/loud-speech.comparison.json"]}' >"$RESULTS/manifest.json"
note "Task 10 front distance_mm=550 sequence complete"
