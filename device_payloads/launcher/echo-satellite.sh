#!/system/bin/sh
# echo-satellite-launcher-v1
#
# Magisk service.d launcher for the one installed Echo Satellite agent. It is
# intentionally not an updater or recovery supervisor: it only starts echod
# and applies bounded restart delay after an unexpected exit.

AGENT=/data/local/bin/echod
CONTROLLED_RESTART=75
delay=1

log_message() {
  log -t echo-satellite-launcher "$1"
}

next_delay() {
  case "$delay" in
    1) delay=2 ;;
    2) delay=4 ;;
    4) delay=8 ;;
    8) delay=16 ;;
    16) delay=32 ;;
    *) delay=60 ;;
  esac
}

while :; do
  if [ ! -x "$AGENT" ]; then
    log_message "agent is absent or not executable; retrying in ${delay}s"
    sleep "$delay"
    next_delay
    continue
  fi

  started=$(date +%s)
  "$AGENT"
  status=$?
  if [ "$status" -eq "$CONTROLLED_RESTART" ]; then
    delay=1
    log_message "controlled update restart"
    continue
  fi

  finished=$(date +%s)
  runtime=$((finished - started))
  if [ "$runtime" -ge 60 ]; then
    delay=1
  fi
  log_message "agent exited with status ${status}; retrying in ${delay}s"
  sleep "$delay"
  next_delay
done
