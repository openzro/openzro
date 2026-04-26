#!/bin/sh

# Check if openzro-ui is running
if pgrep -x -f /usr/bin/openzro-ui >/dev/null 2>&1;
then
  runner=$(ps --no-headers -o '%U' -p $(pgrep -x -f /usr/bin/openzro-ui) | sed 's/^[ \t]*//;s/[ \t]*$//')
  # Only re-run if it was already running
  pkill -x -f /usr/bin/openzro-ui >/dev/null 2>&1
  su -l - "$runner" -c 'nohup /usr/bin/openzro-ui > /dev/null 2>&1 &'
fi
