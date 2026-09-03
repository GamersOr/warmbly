#!/usr/bin/env bash
#
# Rebuild and restart a Docker-free Warmbly install after the checkout moved.
# This is what the updater runs in UPDATER_MODE=command (deploy/systemd/
# warmbly-updater.service), and what you run by hand after `git pull`. It is
# the "Upgrading" section of docs/content/docs/development/bare-metal.mdx as a
# script: build every artifact that exists on this host, install it, restart
# the backend first (migrations apply on its boot), then the rest.
#
#   scripts/upgrade-bare-metal.sh            # build + install + restart
#   scripts/upgrade-bare-metal.sh --pull     # git pull --ff-only first
#
# Run it as the user who owns the checkout. Install and restart steps use sudo;
# give that user this sudoers line (visudo) so the updater can run unattended:
#
#   deploy ALL=(root) NOPASSWD: /usr/bin/install, /bin/cp, /bin/rm, /bin/chown, /bin/chmod, /bin/systemctl, /bin/ln
#
set -euo pipefail

SRC="${WARMBLY_SRC:-/opt/warmbly/src}"
PREFIX="${WARMBLY_PREFIX:-/opt/warmbly}"
SUDO="sudo"
if [[ "$(id -u)" -eq 0 ]]; then SUDO=""; fi

log() { printf '==> %s\n' "$*"; }

if [[ "${1:-}" == "--pull" ]]; then
  log "pulling"
  git -C "$SRC" pull --ff-only
fi

cd "$SRC"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
COMMIT="$(git rev-parse HEAD 2>/dev/null || true)"
BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w -X github.com/warmbly/warmbly/internal/version.Version=$VERSION -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT"

# A service is built only if this host runs it: no Rust toolchain means no
# tracking rebuild, and a unit that is not installed is skipped.
has_unit() { systemctl list-unit-files "warmbly-$1.service" --no-legend 2>/dev/null | grep -q .; }

log "building Go services ($VERSION)"
export CGO_ENABLED=0
mkdir -p out
for cmd in backend forms consumer worker migrate warmblyctl updater; do
  go build -ldflags="$LDFLAGS" -o "out/$cmd" "./cmd/$cmd"
done

if has_unit tracking && command -v cargo >/dev/null 2>&1; then
  log "building tracking"
  (cd tracking && cargo build --release)
fi

if has_unit realtime && command -v mix >/dev/null 2>&1; then
  log "building realtime"
  (cd realtime && MIX_ENV=prod mix deps.get --only prod && MIX_ENV=prod mix compile && MIX_ENV=prod mix release --overwrite)
fi

if command -v pnpm >/dev/null 2>&1; then
  for app in web admin forms; do
    if [[ -d "$PREFIX/$app" ]]; then
      log "building $app"
      (cd "$app" && pnpm install --frozen-lockfile && pnpm build)
    fi
  done
fi

log "installing binaries"
$SUDO install -m 0755 out/backend out/forms out/consumer out/worker out/migrate out/warmblyctl out/updater "$PREFIX/bin/"
$SUDO ln -sf "$PREFIX/bin/warmblyctl" /usr/local/bin/warmblyctl
if [[ -f tracking/target/release/tracking ]] && has_unit tracking; then
  $SUDO install -m 0755 tracking/target/release/tracking "$PREFIX/bin/tracking"
fi
if [[ -d realtime/_build/prod/rel/realtime ]] && has_unit realtime; then
  $SUDO rm -rf "$PREFIX/realtime"
  $SUDO cp -r realtime/_build/prod/rel/realtime "$PREFIX/realtime"
  $SUDO chown -R warmbly:warmbly "$PREFIX/realtime"
fi

# Frontends: the runtime config.js is written by hand on a bare-metal install
# and a rebuilt dist/ would drop it, so keep it across the copy.
for app in web admin; do
  if [[ -d "$app/dist" && -d "$PREFIX/$app" ]]; then
    log "installing $app"
    cfg="$(mktemp)"
    [[ -f "$PREFIX/$app/config.js" ]] && cp "$PREFIX/$app/config.js" "$cfg"
    $SUDO rm -rf "$PREFIX/$app"
    $SUDO cp -r "$app/dist" "$PREFIX/$app"
    [[ -s "$cfg" ]] && $SUDO cp "$cfg" "$PREFIX/$app/config.js"
    rm -f "$cfg"
    $SUDO chmod -R a+rX "$PREFIX/$app"
  fi
done
if [[ -d forms/dist && -d "$PREFIX/forms" ]]; then
  log "installing forms"
  $SUDO rm -rf "$PREFIX/forms/dist"
  $SUDO cp -r forms/dist "$PREFIX/forms/dist"
fi

log "restarting backend"
$SUDO systemctl restart warmbly-backend
for i in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:8080/health >/dev/null 2>&1; then break; fi
  sleep 2
done

rest=()
for svc in forms consumer tracking realtime worker; do
  has_unit "$svc" && rest+=("warmbly-$svc")
done
if [[ ${#rest[@]} -gt 0 ]]; then
  log "restarting ${rest[*]}"
  $SUDO systemctl restart "${rest[@]}"
fi

log "done: $VERSION"
