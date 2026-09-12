#!/usr/bin/env bash
#
# Deploy education-demo to https://kayushkin.com/education-demo
#
#   1. build the front end (base path /education-demo/)
#   2. build and provenance-check the Go binary
#   3. install the systemd user unit and restart
#   4. install the nginx location if it is not already there
#   5. verify the live URL actually answers
#
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="$HOME/bin"
SERVICE="education-demo.service"
BINARY="education-demo-server"
UNIT_DEST="$HOME/.config/systemd/user/$SERVICE"
NGINX_SITE="/etc/nginx/sites-enabled/kayushkin.com"
# nginx.conf does `include /etc/nginx/sites-enabled/*` — EVERY file, not just
# the ones without a suffix. A backup written beside the original is therefore
# loaded as a second copy of the whole vhost, and nginx warns about a
# conflicting server name and silently ignores one of them. Backups go outside
# the included directory.
NGINX_BACKUP_DIR="/etc/nginx/backups"
PUBLIC_URL="https://kayushkin.com/education-demo"

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

echo "==> Checking nginx..."
if sudo -n grep -q "location /education-demo" "$NGINX_SITE" 2>/dev/null; then
  echo "    location already present"
else
  echo "    inserting location before the catch-all"
  sudo -n mkdir -p "$NGINX_BACKUP_DIR"
  sudo -n cp "$NGINX_SITE" "$NGINX_BACKUP_DIR/kayushkin.com.bak.$(date +%s)"
  # Insert before the final `location / {` so the more specific prefix wins.
  sudo -n python3 - "$NGINX_SITE" "$REPO_DIR/nginx-education-demo.conf" <<'PY'
import sys
site, snippet_path = sys.argv[1], sys.argv[2]
with open(site) as f:
    text = f.read()
with open(snippet_path) as f:
    snippet = "".join(l for l in f if not l.startswith("#") or "location" in l)
marker = "    # Main site (catch-all)"
if marker not in text:
    marker = "    location / {"
    if marker not in text:
        sys.exit("cannot find the catch-all location to insert before")
text = text.replace(marker, snippet.rstrip() + "\n\n" + marker, 1)
with open(site, "w") as f:
    f.write(text)
PY
  sudo -n nginx -t
  # A conflicting server name means a stray vhost copy is being loaded — most
  # likely a backup someone left in sites-enabled. Refuse rather than reload
  # into a config where one of two identical vhosts is silently ignored.
  if sudo -n nginx -t 2>&1 | grep -q "conflicting server name"; then
    echo "    REFUSING TO RELOAD: nginx reports a conflicting server name." >&2
    echo "    Something in /etc/nginx/sites-enabled/ duplicates a vhost." >&2
    sudo -n nginx -t 2>&1 | grep "conflicting server name" >&2
    exit 1
  fi
  sudo -n systemctl reload nginx
fi

echo "==> Verifying the public URL..."
code="$(curl -s -o /dev/null -w '%{http_code}' "$PUBLIC_URL/api/health")"
if [ "$code" != "200" ]; then
  echo "    FAILED: $PUBLIC_URL/api/health returned $code" >&2
  exit 1
fi
echo "    $PUBLIC_URL/api/health -> 200"
echo
echo "Deployed: $PUBLIC_URL"
