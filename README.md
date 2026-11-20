# SatriaPanel

Panel deploy dan manajemen aplikasi untuk home server Ubuntu milik satu admin. SatriaPanel adalah **satu binary Go** dengan antarmuka web yang tertanam, penyimpanan **SQLite**, dan dijalankan sebagai layanan **systemd**. Panel dirancang untuk diakses secara privat melalui **Cloudflare Tunnel + Cloudflare Access**.

## Fitur

- **Deploy berbasis rilis** — setiap deploy membuat folder rilis baru. Deploy yang gagal tidak mematikan rilis aktif; rollback satu klik; auto-rollback saat health check gagal.
- **Antrean build serial** — hanya satu deploy berjalan pada satu waktu.
- **Port-first** — setiap aplikasi mendapat port unik dari kolam `3100–3999`, hanya didengarkan di `127.0.0.1`.
- **Native-first** — proses langsung, PM2, systemd, Supervisor, dan Docker/Compose sebagai pelengkap.
- **GitHub** — Personal Access Token terenkripsi, pemilihan repo/branch, deteksi runtime, dan webhook HMAC untuk auto deploy.
- **Database** — PostgreSQL, MySQL, dan SQLite. Editor tabel, SQL, riwayat query, impor CSV/SQL, ekspor CSV/SQL/JSON, backup dan restore.
- **Redis** — satu instance dengan ACL per aplikasi (`app_<slug>`, prefix `app_<slug>:*`), penjelajah key, TTL, dan flush prefix.
- **Cron internal** — penjadwal, preset, pratinjau jadwal, riwayat, output, timeout, dan larangan tumpang tindih.
- **File manager** — terbatas pada folder aplikasi, unggah/unduh, editor teks dengan konflik mtime, zip/unzip.
- **Terminal web** — xterm.js + WebSocket; PTY di Linux, pipe fallback di Windows.
- **Backup** — arsip lokal + Google Drive (scope `drive.file`), manifest SHA-256, retensi harian/mingguan, dan restore dengan verifikasi checksum.
- **Notifikasi** — pusat notifikasi panel, Telegram, dan gateway WhatsApp generik.
- **Audit & monitor** — audit log lengkap, metrik CPU/RAM/disk live via SSE, alert dengan deduplikasi.
- **MCP API** — endpoint Streamable HTTP `/mcp` di binary dan tunnel yang sama, token bercakupan (`read`, `deploy`, `cron`, `backup`, `data-write`, `admin`), konfirmasi untuk aksi destruktif, deploy/backup asinkron, dan audit setiap panggilan.
- **Keamanan** — AES-256-GCM untuk kredensial, bcrypt, sesi HttpOnly, CSRF header `X-Satria`, rate limit login, CSP ketat, dan tidak ada endpoint reveal kredensial.

## Persyaratan

Build:

- Go 1.26 atau lebih baru (dikembangkan dengan Go 1.27)
- Git, Node.js, npm, PM2, PHP, Python, Docker — sesuai runtime aplikasi yang dikelola

Produksi:

- Ubuntu 22.04/24.04
- systemd
- Cloudflare Tunnel (disarankan) dan Cloudflare Access untuk membatasi akses
- Redis opsional (`sudo apt install redis-server`)

## Build & jalankan lokal

```bash
go mod tidy
go build -o satriapanel .
./satriapanel -data ./data -addr 127.0.0.1:8282
```

Buka `http://127.0.0.1:8282`, lalu selesaikan wizard akun admin.

Mode data contoh (hanya untuk pengembangan):

```bash
./satriapanel -data ./data-dev -addr 127.0.0.1:8282 -demo
```

Data contoh membuat 3 aplikasi, rilis, deployment, cron, notifikasi, metrik, dan database SQLite contoh. Dua aplikasi demo dijalankan dengan Node sederhana pada port 3100 dan 3102. **Jangan gunakan `-demo` di produksi.**

### Flag

| Flag | Default | Keterangan |
|---|---|---|
| `-data` | `./data` | Folder data panel (SQLite, kunci, aplikasi, log, backup) |
| `-addr` | `127.0.0.1:8282` | Alamat bind panel |
| `-base-url` | kosong | Base URL publik; dapat diubah di Pengaturan |
| `-demo` | `false` | Seed data contoh |

## Instalasi produksi (ringkas)

```bash
sudo useradd --system --home /opt/satriapanel --shell /usr/sbin/nologin satriapanel
sudo mkdir -p /opt/satriapanel /var/lib/satriapanel
sudo cp satriapanel /opt/satriapanel/
sudo cp deploy/satriapanel.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now satriapanel
```

Secara default panel hanya mendengarkan `127.0.0.1:8282`, sehingga tidak terbuka ke internet. Akses publik dilakukan melalui Cloudflare Tunnel:

```yaml
# /etc/cloudflared/config.yml
tunnel: <TUNNEL_ID>
credentials-file: /etc/cloudflared/<TUNNEL_ID>.json
ingress:
  - hostname: panel.satriyadi.com
    service: http://127.0.0.1:8282
  - service: http_status:404
```

Pasang Cloudflare Access pada hostname panel agar hanya akun yang diizinkan dapat membuka login.

> Panel ini dapat menulis konfigurasi ingress dari halaman **Cloudflared & Port**. Panel membuat cadangan `.bak`, lalu memvalidasi dengan `cloudflared tunnel ingress validate` sebelum reload. Penulisan hanya terjadi setelah konfirmasi **TERAPKAN**.

## Struktur data

```
data/
  panel.db            SQLite panel
  secret.key          kunci AES-256-GCM (0600)
  apps/<slug>/        folder kerja aplikasi
    releases/         rilis terisolasi
    persistent/       data persisten
  logs/
    apps/             log stdout/stderr aplikasi
    deploy/           log deployment
  backups/            arsip backup + dump database
  databases/          database SQLite yang dikelola
  runtime/            skrip PM2
  tmp/                staging
```

## Keamanan

- Kredensial disimpan terenkripsi AES-256-GCM dengan `secret.key` terpisah.
- Nilai rahasia **tidak pernah** dikirim kembali ke browser; tidak ada tombol reveal.
- Semua mutasi API memerlukan header `X-Satria: 1` (proteksi CSRF).
- Login dibatasi 5 percobaan gagal per 15 menit per IP.
- Sesi HttpOnly, SameSite=Lax, berlaku 12 jam dengan idle timeout.
- Password admin di-hash dengan bcrypt cost 12.
- Content-Security-Policy membatasi script ke origin panel; tidak ada CDN atau font eksternal.
- Backup berisi `secret.key` dan seluruh data sensitif. Simpan arsip backup di tempat aman, dan batasi akses folder `data/backups`.

## MCP API

SatriaPanel menyediakan server MCP (Model Context Protocol) melalui Streamable HTTP di `https://<panel>/mcp`. Endpoint berjalan di binary dan tunnel yang sama, bukan layanan terpisah.

1. Buka **Pengaturan → MCP**.
2. Buat token, pilih scope, lalu salin token sekali saja. Token disimpan sebagai hash SHA-256.
3. Konfigurasikan klien MCP:

```json
{
  "mcpServers": {
    "satriapanel": {
      "type": "http",
      "url": "https://panel.satriyadi.com/mcp",
      "headers": { "Authorization": "Bearer spmcp_..." }
    }
  }
}
```

Scope: `read` (baca/status/log/berkas/skema/SELECT), `deploy` (deploy, rollback, start/stop/restart aplikasi/container), `cron`, `backup`, `data-write` (DML), dan `admin` (aksi destruktif seperti hapus aplikasi/database, ubah env, DDL, flush Redis, restore, tulis/hapus berkas, shell, apply ingress). `admin` mencakup seluruh scope lain.

- Token baru memakai `read` secara default dan hanya ditampilkan sekali.
- Pencabutan token berlaku seketika. Saklar utama dapat menonaktifkan endpoint tanpa menghapus token.
- Aksi destruktif memerlukan `confirm: true` dan `target` yang diulang persis (slug, nama database, ID backup, path, atau perintah).
- Deploy dan backup berjalan asinkron; pantau dengan `deploy_status` atau `backup_status`.
- Setiap panggilan berhasil/ditolak masuk **Audit** dengan sumber `MCP`, nama token, tool, target, dan hasil.
- MCP tidak pernah menyediakan kredensial tersimpan, pembuatan/pencabutan token MCP lain, atau perubahan password.
- Batas laju default 60 permintaan per menit per token, dapat diubah di Pengaturan.

## Integrasi

### GitHub

Buat Personal Access Token dengan akses repo. Untuk webhook, token memerlukan izin **Webhooks: Read/Write** (fine-grained) atau `admin:repo_hook` (classic). URL webhook dibuat otomatis: `https://<panel>/api/webhooks/github/<app-id>`.

### Google Drive

1. Buat OAuth Client tipe **Web application** di Google Cloud Console.
2. Tambahkan redirect URI `https://<panel>/api/integrations/google/callback`.
3. Simpan Client ID/Secret di halaman Backup, lalu otorisasi.
4. Scope yang diminta hanya `https://www.googleapis.com/auth/drive.file`.

### Telegram / WhatsApp

Telegram menggunakan Bot API (`chat_id` + token). WhatsApp memakai gateway HTTP generik yang menerima:

```json
{ "target": "62812...", "message": "..." }
```

## Pengembangan

```bash
gofmt -w .
go vet ./...
go build ./...
```

Aset web berada di `web/` dan ditanam dengan `go:embed`. Tidak ada proses build frontend; frontend adalah JavaScript vanilla modular. Font Fira Sans/Fira Code dan xterm.js disimpan lokal di `web/assets/fonts` dan `web/vendor`.

## Lisensi

Proyek pribadi. Hak cipta Wisnu / SatriaPanel.
