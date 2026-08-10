#!/bin/sh
set -e

# The service runs unprivileged. Created before the files land so ownership can
# be set, and left alone on upgrade.
if ! id -u mycel >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --no-create-home --shell /usr/sbin/nologin \
                --home-dir /var/lib/mycel mycel
    elif command -v adduser >/dev/null 2>&1; then
        # Alpine
        adduser -S -D -H -h /var/lib/mycel -s /sbin/nologin mycel
    fi
fi
