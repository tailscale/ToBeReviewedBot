#!/bin/sh

mkdir -p /root/tailscale
/usr/local/bin/tailscaled --statedir=/root/tailscale --socket=/tmp/tailscale.sock &
if [ ! -z "${TS_AUTHKEY}" ]; then
    export AUTH="--authkey=${TS_AUTHKEY}"
    echo "Bringing tailscale interface up with authkey"
else
    echo "Bringing tailscale interface up"
fi
/usr/local/bin/tailscale --socket=/tmp/tailscale.sock up ${AUTH} --hostname=tbr-bot
/ts-tbrbot
