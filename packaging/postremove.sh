#!/bin/sh
set -e

# Only on a real removal, not on the remove half of an upgrade.
case "${1:-}" in
    purge|0)
        if command -v systemctl >/dev/null 2>&1; then
            systemctl daemon-reload >/dev/null 2>&1 || true
        fi
        # The user is left in place: files under /etc/mycel and /var/lib/mycel
        # may still belong to it, and removing it would orphan them.
        ;;
esac
