#!/bin/bash
# linguine-watchdog.sh — cron watchdog for tg-linguine.
#
# Add to crontab (one minute interval):
#   * * * * * LINGUINE_DIR=~/Projects/tg-linguine LINGUINE_LOG=~/Projects/tg-linguine/watchdog.log ~/Projects/tg-linguine/scripts/linguine-watchdog.sh
#
# Required env vars:
#   LINGUINE_DIR — repo root (containing bin/tg-linguine and .env)
#   LINGUINE_LOG — watchdog's own log file (separate from bot.log)

DIR="${LINGUINE_DIR:?LINGUINE_DIR is not set}"
LOG="${LINGUINE_LOG:?LINGUINE_LOG is not set}"

cd "$DIR" || exit 1

# Match by "bin/tg-linguine" so the watchdog script itself
# (linguine-watchdog.sh) does not get counted as an instance.
COUNT=$(pgrep -f "bin/tg-linguine" 2>/dev/null | wc -l | tr -d ' ')

if [ "$COUNT" = "0" ]; then
    echo "$(date '+%Y/%m/%d %H:%M:%S') [watchdog] not running, starting..." >> "$LOG"
    nohup ./bin/tg-linguine >> "$LOG" 2>&1 &
    echo "$(date '+%Y/%m/%d %H:%M:%S') [watchdog] started, pid=$!" >> "$LOG"
elif [ "$COUNT" -gt 1 ]; then
    echo "$(date '+%Y/%m/%d %H:%M:%S') [watchdog] $COUNT instances found, killing all" >> "$LOG"
    pkill -9 -f "bin/tg-linguine" 2>/dev/null || true
    sleep 1
    nohup ./bin/tg-linguine >> "$LOG" 2>&1 &
    echo "$(date '+%Y/%m/%d %H:%M:%S') [watchdog] restarted, pid=$!" >> "$LOG"
fi
