#!/usr/bin/env bash
set -euo pipefail

_pong() {
  for i in $(seq 100); do
    echo "${0} (${$}) PONG (${i}/100)"
  done
}

trap _pong USR1

for i in $(seq 100); do
  echo "${0} ($$) SETTING UP (${i}/100)"
  sleep 0.01
done

while true; do
  now="$(date +%s)"
  now_mod=$((now % 10))

  if [[ "${now_mod}" == 0 ]]; then
    echo "${0} (${$}) STILL HERE"
    sleep 1
  fi

  sleep 0.1
done
