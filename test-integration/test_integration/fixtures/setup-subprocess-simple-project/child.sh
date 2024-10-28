#!/usr/bin/env bash
set -euo pipefail

# This _pong function and associated trap ensures that any SIGUSR1 sent during `predict`
# will cause this process to write a decent amount of text to stdout. In the event that
# stream redirection is not working correctly, this process will likely be in a defunct
# state before the first SIGUSR1 can be sent.
_pong() {
  for i in $(seq 100); do
    echo "${0} (${$}) PONG (${i}/100)"
  done
}

trap _pong USR1

# This loop simulates a setup period for filling up any stdout buffer.
for i in $(seq 100); do
  echo "${0} ($$) SETTING UP (${i}/100)"
  sleep 0.01
done

# This loop simulates periodic writes to stdout while the background process is running
# for the purpose of ensuring the file descriptor is still usable.
while true; do
  now="$(date +%s)"
  now_mod=$((now % 10))

  if [[ "${now_mod}" == 0 ]]; then
    echo "${0} (${$}) STILL HERE"
    sleep 1
  fi

  sleep 0.1
done
