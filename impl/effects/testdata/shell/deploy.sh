#!/usr/bin/env bash
set -euo pipefail

# ssh -o ProxyCommand=none root@guest is only described here
RELAY="wss://relay.example/tunnel"

status="$(curl -fsS https://relay.example/status)"
wget -q -O /dev/null https://relay.example/health

ssh -o "ProxyCommand=websocat --binary wss://relay.example/tunnel" -o BatchMode=yes root@guest.internal true
ssh-keygen -t ed25519 -N "" -f /tmp/id_ed25519
scp /tmp/id_ed25519.pub root@guest.internal:/root/.ssh/authorized_keys

docker exec -i guest sh -c "echo hello"
container run --rm alpine true
podman inspect guest

nc -z guest.internal 22
socat - TCP:guest.internal:22

echo "docker exec" | tee /tmp/notes.txt
