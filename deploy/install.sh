#!/usr/bin/env bash
set -euo pipefail

# Called by scripts/deploy.py. All deployment-specific values come from configuration.
: "${APP_ROOT:?missing APP_ROOT}"
: "${APP_PORT:?missing APP_PORT}"
: "${PUBLIC_URL:?missing PUBLIC_URL}"
: "${RELEASE_ID:?missing RELEASE_ID}"
: "${RELEASE_ARCHIVE:?missing RELEASE_ARCHIVE}"
APP_DOMAIN="${APP_DOMAIN:-}"
export DEBIAN_FRONTEND=noninteractive

if ! id flytogether >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/flytogether --create-home --shell /usr/sbin/nologin flytogether
fi
install -d -m 755 "$APP_ROOT/releases/$RELEASE_ID"
tar -xzf "$RELEASE_ARCHIVE" -C "$APP_ROOT/releases/$RELEASE_ID"
find "$APP_ROOT/releases/$RELEASE_ID/web" -type d -exec chmod 755 {} +
find "$APP_ROOT/releases/$RELEASE_ID/web" -type f -exec chmod 644 {} +
chmod 755 "$APP_ROOT/releases/$RELEASE_ID/server"
ln -sfn "$APP_ROOT/releases/$RELEASE_ID" "$APP_ROOT/current.next"
mv -Tf "$APP_ROOT/current.next" "$APP_ROOT/current"
install -d -o flytogether -g flytogether -m 750 /var/lib/flytogether

# Preserve existing Linx credentials while updating only public application settings.
python3 - <<'PY'
from pathlib import Path
import os
p=Path('/etc/flytogether.env')
values={}
if p.exists():
    for line in p.read_text().splitlines():
        if line and not line.startswith('#') and '=' in line:
            key,value=line.split('=',1)
            values[key]=value
values.update({
    'LISTEN_ADDR': ('127.0.0.1' if os.environ.get('APP_DOMAIN') else '0.0.0.0')+':'+os.environ['APP_PORT'],
    'WEB_DIR': os.environ['APP_ROOT']+'/current/web',
    'PUBLIC_URL': os.environ['PUBLIC_URL'],
})
values.setdefault('SAVE_PATH','/var/lib/flytogether/games.db')
values.setdefault('SAVE_RETENTION_HOURS','168')
values.setdefault('ROOM_HOST_RECLAIM_SECONDS','60')
values.setdefault('ROOM_DELEGATION_SECONDS','60')
values.setdefault('ADMIN_ACCESS_KEY','')
values.setdefault('DIAGNOSTICS_RETENTION_HOURS','168')
values.setdefault('DIAGNOSTICS_TRANSCRIPTS','true')
for key,value in {'LINGXI_MAX_CONNECTIONS':'4','LINGXI_SESSION_SECONDS':'900','LINGXI_REALTIME_DAILY_LIMIT':'2000','LINGXI_TTS_DAILY_LIMIT':'1000','SPECTATOR_CAPACITY':'8','SPECTATOR_JOINS_PER_MINUTE':'20','SPECTATOR_MAX_CONNECTIONS':'4','SPECTATOR_SESSION_SECONDS':'900','SPECTATOR_REALTIME_DAILY_LIMIT':'500'}.items():
    values.setdefault(key,value)
values.setdefault('LINGXI_REALTIME_URL','wss://xrobo-io.qiniuapi.com/v1/realtime')
for key in ['LINGXI_REALTIME_MODEL','LINGXI_REALTIME_API_KEY']:
    values.setdefault(key,'')
p.write_text(''.join(key+'='+value+'\n' for key,value in values.items()))
p.chmod(0o600)
PY

cat > /etc/systemd/system/flytogether.service <<EOF
[Unit]
Description=FlyTogether family board game
After=network-online.target
Wants=network-online.target

[Service]
User=flytogether
Group=flytogether
WorkingDirectory=$APP_ROOT/current
EnvironmentFile=/etc/flytogether.env
ExecStart=$APP_ROOT/current/server
Restart=on-failure
RestartSec=3
TimeoutStopSec=10
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/var/lib/flytogether
UMask=0077

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable flytogether >/dev/null
systemctl restart flytogether

if [ -n "$APP_DOMAIN" ]; then
  fresh_caddy=0
  caddy_changed=0
  if ! command -v caddy >/dev/null 2>&1; then
    apt-get update -qq
    apt-get install -y -qq caddy
    fresh_caddy=1
    caddy_changed=1
  fi
  install -d -m 755 /etc/caddy/flytogether.d
  if [ "$fresh_caddy" = 1 ]; then
    printf 'import /etc/caddy/flytogether.d/*.caddy\n' > /etc/caddy/Caddyfile
  elif ! grep -Fq 'import /etc/caddy/flytogether.d/*.caddy' /etc/caddy/Caddyfile; then
    printf '\nimport /etc/caddy/flytogether.d/*.caddy\n' >> /etc/caddy/Caddyfile
    caddy_changed=1
  fi
  candidate_caddy=$(mktemp)
  cat > "$candidate_caddy" <<EOF
$APP_DOMAIN {
    encode zstd gzip
    reverse_proxy 127.0.0.1:$APP_PORT
}
EOF
  if ! cmp -s "$candidate_caddy" /etc/caddy/flytogether.d/app.caddy; then
    install -m 644 "$candidate_caddy" /etc/caddy/flytogether.d/app.caddy
    caddy_changed=1
  fi
  rm -f "$candidate_caddy"
  caddy validate --config /etc/caddy/Caddyfile
  systemctl enable caddy >/dev/null
  if [ "$caddy_changed" = 1 ] || ! systemctl is-active --quiet caddy; then
    systemctl reload-or-restart caddy
  fi
  if command -v ufw >/dev/null 2>&1 && ufw status | grep -q 'Status: active'; then
    ufw allow 80/tcp
    ufw allow 443/tcp
  fi
fi

for attempt in 1 2 3 4 5; do
  if curl --max-time 5 -fsS "http://127.0.0.1:$APP_PORT/healthz"; then
    printf '\nFlyTogether service is healthy.\n'
    exit 0
  fi
  sleep 1
done
printf 'FlyTogether health check failed. Inspect systemctl status flytogether.\n' >&2
exit 1
