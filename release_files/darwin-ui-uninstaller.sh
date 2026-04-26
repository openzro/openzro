#!/bin/sh

export PATH=$PATH:/usr/local/bin

# check if openzro is installed
OZ_BIN=$(which openzro)
if [ -z "$OZ_BIN" ]
then
  exit 0
fi
# start openzro daemon service
echo "openzro daemon service still running. You can uninstall it by running: "
echo "sudo openzro service stop"
echo "sudo openzro service uninstall"
