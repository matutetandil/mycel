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
#
# Generated inside a container, and owned by uid 1000, because of where these
# files are read. The Mycel container runs as uid 1000 and the key is bind
# mounted into it — and a bind mount on Linux keeps the ownership the host
# gave it. Written by whoever ran the script, mode 600, the container cannot
# read its own key: the connector fails at startup and takes the service with
# it. On a Mac that never shows, since Docker Desktop virtualises ownership
# and the file is readable whatever it says — so this was green locally and
# red on CI, which is the only place it was ever going to matter.
set -euo pipefail

DIR="$(cd "$(dirname "$0")/.." && pwd)/ssh"
mkdir -p "$DIR"

if [ -f "$DIR/id_ed25519" ] && [ -f "$DIR/authorized_keys" ]; then
  exit 0
fi

rm -f "$DIR/id_ed25519" "$DIR/id_ed25519.pub" "$DIR/authorized_keys"

docker run --rm -v "$DIR:/out" alpine:3.24 sh -c '
  apk add --no-cache openssh-keygen >/dev/null 2>&1
  ssh-keygen -t ed25519 -N "" -C "mycel-integration" -f /out/id_ed25519 >/dev/null
  cp /out/id_ed25519.pub /out/authorized_keys
  # The server refuses a key file others can read, and so does ssh.
  chmod 600 /out/id_ed25519 /out/authorized_keys
  # uid 1000 is the Mycel container and the ssh server`s runner user both.
  chown 1000:1000 /out/id_ed25519 /out/id_ed25519.pub /out/authorized_keys
'

# Assert the postcondition rather than hope for it: the container that will
# read this key runs as uid 1000, and whether it can is the whole question.
# A silent "no" here is two minutes of CI waiting for a port that will never
# open, so it is worth one second and a clear sentence.
if ! docker run --rm --user 1000:1000 -v "$DIR/id_ed25519:/key:ro" alpine:3.24 \
     sh -c 'cat /key >/dev/null 2>&1'; then
  echo "the generated key is not readable by uid 1000, which is what runs Mycel." >&2
  echo "the exec connector would fail at startup and take the service with it." >&2
  exit 1
fi
