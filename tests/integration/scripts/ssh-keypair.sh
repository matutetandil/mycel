#!/usr/bin/env bash
# Make the key pair the exec connector signs in with, for this run only.
#
# `exec { driver = "ssh" }` runs a command on another machine, and the
# connector authenticates with a key: a password is refused, because there is
# no terminal to type one into. So the suite needs a real pair — the public
# half authorised on the server, the private half readable by Mycel.
#
# Generated per run rather than committed, for the same reason the push
# connector's service account is: nothing key-shaped belongs in a repository,
# and a file that looks like a private key is what push protection stops at
# the worst possible moment.
set -euo pipefail

DIR="$(cd "$(dirname "$0")/.." && pwd)/ssh"
mkdir -p "$DIR"

if [ -f "$DIR/id_ed25519" ] && [ -f "$DIR/authorized_keys" ]; then
  exit 0
fi

rm -f "$DIR/id_ed25519" "$DIR/id_ed25519.pub" "$DIR/authorized_keys"
ssh-keygen -t ed25519 -N "" -C "mycel-integration" -f "$DIR/id_ed25519" >/dev/null
cp "$DIR/id_ed25519.pub" "$DIR/authorized_keys"

# The server refuses a key file the world can read, and so does ssh.
chmod 600 "$DIR/id_ed25519" "$DIR/authorized_keys"
