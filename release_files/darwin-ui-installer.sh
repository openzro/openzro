#!/bin/sh

export PATH=$PATH:/usr/local/bin:/opt/homebrew/bin

# check if openzro is installed
OZ_BIN=$(which openzro)
if [ -n "$OZ_BIN" ]
then
  echo "Stopping and uninstalling Openzro daemon"
  openzro service stop || true
  openzro service uninstall || true
fi

# check if openzro is installed
OZ_BIN=$(which openzro)
if [ -z "$OZ_BIN" ]
then
  echo "Openzro daemon is not installed. Please run: brew install openzro/tap/openzro"
  exit 1
fi
OZ_UI_VERSION=$1
OZ_VERSION=$(openzro version)
if [ "X-$OZ_UI_VERSION" != "X-$OZ_VERSION" ]
then
  echo "Openzro's daemon is running with a different version than the Openzro's UI:"
  echo "Openzro UI Version: $OZ_UI_VERSION"
  echo "Openzro Daemon Version: $OZ_VERSION"
  echo "Please run: brew install openzro/tap/openzro"
  echo "to update it"
fi

if [ -n "$OZ_BIN" ]
then
  echo "Stopping Openzro daemon"
  osascript -e 'quit app "Openzro UI"' 2> /dev/null || true
  openzro service stop 2> /dev/null || true
fi

# start openzro daemon service
echo "Starting Openzro daemon"
openzro service install 2> /dev/null || true
openzro service start || true

# start app
open /Applications/Openzro\ UI.app
