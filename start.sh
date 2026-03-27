#!/bin/bash

cd "$(dirname "$0")"

# Load .env if exists
if [ -f .env ]; then
  export $(grep -v '^#' .env | xargs)
fi

# If already running, do nothing
if pgrep -f "kiro-discord-bot" > /dev/null 2>&1; then
  echo "bot already running (PID: $(pgrep -f kiro-discord-bot))"
  exit 0
fi

# Build
go build -o /tmp/kiro-discord-bot .

# Start bot with watchdog
(while true; do
  echo "[watchdog] starting bot..."
  /tmp/kiro-discord-bot >> /tmp/kiro-bot.log 2>&1
  echo "[watchdog] bot exited, restarting in 3s..."
  sleep 3
done) &

sleep 5
tail -3 /tmp/kiro-bot.log
