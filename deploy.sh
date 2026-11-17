#!/usr/bin/env bash
#
# Deploy education-demo on this host's loopback address. It is NOT published.
#
#   1. build the front end (base path /education-demo/)
#   2. build and provenance-check the Go binary
#   3. install the systemd user unit and restart
#   4. refuse to finish if nginx publishes it
#
# The server has no login: anyone who reaches it can start sessions that run
# model calls. It was taken off the public site on 2026-11-17 for that reason,
# and this script no longer adds an nginx location. Give it a login before
# publishing it again.
#
set -euo pipefail

# One shared gate decides whether this tree may be deployed (main clone, default
# branch, clean, pushed, not behind, and the same for every tree the build reads).
# It lives in healthcheck/scripts/deploy-gate.sh. Do not inline or copy it.
( cd "$(dirname "$0")" && "$HOME/bin/deploy-gate" check )

REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="$HOME/bin"
SERVICE="education-demo.service"
BINARY="education-demo-server"
UNIT_DEST="$HOME/.config/systemd/user/$SERVICE"
# The public vhost, read only to make sure it does not publish this service.
NGINX_SITE="/etc/nginx/sites-enabled/kayushkin.com"

cd "$REPO_DIR"
export PATH="$HOME/.local/share/mise/shims:$PATH"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
export DBUS_SESSION_BUS_ADDRESS="${DBUS_SESSION_BUS_ADDRESS:-unix:path=${XDG_RUNTIME_DIR}/bus}"

echo "==> Testing Go..."
go test ./... 2>&1 | grep -v "^ok\|no test files" || true
go test ./... > /dev/null

echo "==> Building front end..."
cd "$REPO_DIR/web"
npm install --silent
npm run build
cd "$REPO_DIR"
if [ ! -f "$REPO_DIR/web/dist/index.html" ]; then
  echo "    REFUSING: web/dist/index.html is missing after the build." >&2
  exit 1
fi
# The built asset URLs must carry the prefix, or the page loads blank behind
# nginx with 404s that only show in the network tab. Cheaper to catch here.
if ! grep -q '/education-demo/assets/' "$REPO_DIR/web/dist/index.html"; then
  echo "    REFUSING: built index.html does not reference /education-demo/assets/." >&2
  echo "    vite base is wrong; the app would 404 every asset in production." >&2
  exit 1
fi

echo "==> Building $BINARY..."
go build -o "$BINARY" ./cmd/education-demo-server

echo "==> Checking provenance..."
buildinfo="$(go version -m "$BINARY")"
vcs_revision="$(printf '%s\n' "$buildinfo" | awk -F= '$1 ~ /[[:space:]]vcs\.revision$/ {print $2}')"
if [ -z "$vcs_revision" ]; then
  echo "    WARNING: no vcs.revision stamped; nothing ties this binary to a commit." >&2
else
  echo "    vcs.revision=$vcs_revision"
fi

echo "==> Installing binary and unit..."
mkdir -p "$BIN_DIR" "$(dirname "$UNIT_DEST")"
install -m 755 "$BINARY" "$BIN_DIR/$BINARY"
install -m 644 "$REPO_DIR/systemd/$SERVICE" "$UNIT_DEST"
systemctl --user daemon-reload
systemctl --user enable "$SERVICE" >/dev/null 2>&1 || true
systemctl --user restart "$SERVICE"

echo "==> Waiting for the service to answer..."
for _ in $(seq 1 30); do
  if curl -sf http://127.0.0.1:8316/education-demo/api/health >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! curl -sf http://127.0.0.1:8316/education-demo/api/health >/dev/null 2>&1; then
  echo "    FAILED: service is not answering on 127.0.0.1:8316." >&2
  systemctl --user status "$SERVICE" --no-pager -n 30 >&2 || true
  exit 1
fi
health="$(curl -s http://127.0.0.1:8316/education-demo/api/health)"
echo "    $health"

# Confirm the RUNNING process is the binary just built, not a survivor of a
# restart that silently failed.
#
# "Is it deployed?" was answered wrongly once by reading a route's status code,
# because an unmatched API path used to answer 200 with index.html. Comparing
# build ids answers it directly and cannot be fooled that way.
built_id="$(go version -m "$BINARY" | awk -F= '$1 ~ /[[:space:]]vcs\.revision$/ {print $2}')"
running_pid="$(systemctl --user show -p MainPID --value "$SERVICE" 2>/dev/null || true)"
if [ -n "$running_pid" ] && [ "$running_pid" != "0" ]; then
  running_exe="$(readlink -f "/proc/$running_pid/exe" 2>/dev/null || true)"
  if [ -n "$running_exe" ]; then
    running_id="$(go version -m "$running_exe" 2>/dev/null | awk -F= '$1 ~ /[[:space:]]vcs\.revision$/ {print $2}')"
    if [ -n "$built_id" ] && [ "$running_id" != "$built_id" ]; then
      echo "    FAILED: the running process is not the binary just built." >&2
      echo "      built:   $built_id" >&2
      echo "      running: ${running_id:-unknown}" >&2
      exit 1
    fi
    echo "    running binary matches the build (${built_id:-unstamped})"
  fi
fi
case "$health" in
  *'"agent_ready":false'*)
    # Not fatal, but it decides whether the demo is real or canned, so it is
    # never allowed to pass silently.
    echo "    WARNING: llm-bridge is unreachable. Sessions will run on fallback" >&2
    echo "    transcripts with participation-only monitoring." >&2
    ;;
esac

echo "==> Checking that nginx does not publish it..."
if sudo -n grep -q "location /education-demo" "$NGINX_SITE"; then
  echo "    FAILED: $NGINX_SITE has a location /education-demo." >&2
  echo "    This server has no login, so it must not be published. Remove the" >&2
  echo "    location from the vhost that owns it, or give the server a login first." >&2
  exit 1
fi
echo "    not published"
echo
echo "Deployed on loopback only."

# Last act: write this deploy to repo-store's ledger, so the next agent sees what is live.
( cd "$(dirname "$0")" && "$HOME/bin/deploy-gate" record )
