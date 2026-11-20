#!/usr/bin/env bash
set -euo pipefail

# Instalasi SatriaPanel untuk Ubuntu. Jalankan sebagai root dari folder repo:
#   sudo ./deploy/install.sh
#
# Script membuat user sistem, memasang binary, dan mengaktifkan service.
# Panel hanya mendengarkan di 127.0.0.1:8282.

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "Jalankan sebagai root: sudo $0" >&2
  exit 1
fi

APP_USER="${APP_USER:-satriapanel}"
APP_DIR="${APP_DIR:-/opt/satriapanel}"
DATA_DIR="${DATA_DIR:-/var/lib/satriapanel}"
BIN_SRC="${1:-./satriapanel}"

if [[ ! -x "$BIN_SRC" ]]; then
  echo "Binary tidak ditemukan/executable: $BIN_SRC" >&2
  echo "Build dulu: go build -o satriapanel ." >&2
  exit 1
fi

if ! id "$APP_USER" >/dev/null 2>&1; then
  useradd --system --home "$APP_DIR" --shell /usr/sbin/nologin "$APP_USER"
fi

install -d -m 0755 "$APP_DIR"
install -d -m 0750 -o "$APP_USER" -g "$APP_USER" "$DATA_DIR"
install -m 0755 "$BIN_SRC" "$APP_DIR/satriapanel"

if [[ ! -f /etc/systemd/system/satriapanel.service ]]; then
  install -m 0644 deploy/satriapanel.service /etc/systemd/system/satriapanel.service
fi

systemctl daemon-reload
systemctl enable --now satriapanel

echo
echo "SatriaPanel terpasang."
echo "  Data   : $DATA_DIR"
echo "  Panel  : http://127.0.0.1:8282 (lokal)"
echo
echo "Langkah berikutnya:"
echo "  1. Buat Cloudflare Tunnel dan arahkan panel.satriyadi.com ke 127.0.0.1:8282."
echo "  2. Lindungi hostname dengan Cloudflare Access."
echo "  3. Buka panel, buat akun admin pada wizard pertama."
echo "  4. Konfigurasi integrasi di Pengaturan/Backup/Cloudflared."
