# PRD — SatriaPanel

## Panel Deploy & Manajemen Aplikasi untuk Server Sendiri

| | |
|---|---|
| **Nama produk** | SatriaPanel |
| **Domain akses** | panel.satriyadi.com |
| **Versi dokumen** | Draft v0.3 — 20 November 2026 |
| **Status** | PRD awal, disusun dari Konsep Awal SatriaPanel Draft v0.16 (final). Draft v0.2 menambahkan Bab 17 — Desain Antarmuka (sistem desain + 6 mockup). Draft v0.3 menambahkan Antarmuka MCP (Keputusan #14, FR-MCP, Bab 18). Pembangunan belum diotorisasi. |
| **Pemilik produk** | Wisnu Satriyadi (pemakai tunggal, sekaligus Admin) |
| **Dokumen sumber** | Konsep Awal — SatriaPanel Draft v0.16, 20 November 2026 (13 Keputusan Terkunci) |

Dokumen ini adalah spesifikasi kebutuhan produk SatriaPanel. Seluruh Keputusan #1–#13 dari dokumen konsep dibawa ke PRD ini **tanpa diubah** dan dirinci menjadi kebutuhan fungsional bernomor (FR), aturan bisnis, model data, daftar layar, dan kriteria penerimaan. Hal yang belum diputuskan dicatat terbuka di Bab 14 — tidak ditebak dan tidak dianggap selesai.

---

## Daftar Isi

1. Pendahuluan
2. Gambaran Umum Produk
3. Peran dan Hak Akses
4. Alur Proses Utama
5. Kebutuhan Fungsional
6. Aturan Bisnis
7. Model Data Panel
8. Daftar Layar
9. Integrasi Eksternal
10. Kebutuhan Non-Fungsional
11. Batasan dan Asumsi
12. Fase Pengembangan dan Kriteria Penerimaan
13. Keputusan Desain Terkunci
14. Hal yang Masih Terbuka
15. Risiko dan Mitigasi
17. Desain Antarmuka
18. Antarmuka MCP (MCP API)
19. Riwayat Versi

---

## 1. Pendahuluan

### 1.1 Latar Belakang

Wisnu mengelola aplikasi-aplikasi buatannya sendiri (antara lain aplikasi web PHP, Node.js, Python, dan Go) dan berencana menjalankan **home server** di rumah — server Ubuntu yang sama arahnya dengan rencana home server untuk Flow API. Selama ini deploy dan pengelolaan aplikasi dilakukan manual lewat terminal: clone repo, install dependensi, mengatur proses dengan PM2/systemd, mengatur port, dan menerbitkan akses lewat Cloudflare Tunnel (cloudflared) satu per satu.

Panel yang sudah ada tidak memenuhi cara kerja tersebut sekaligus:

- **Coolify, Dokploy, CapRover** bersifat Docker-sentris — tidak ada mode native PM2/systemd/PHP-FPM, dan tidak ada editor visual database bawaan.
- **1Panel** adalah panel server umum, bukan panel deploy Git — tidak ada alur GitHub repo privat + webhook push-to-deploy sebagai fitur inti.
- **aaPanel** (inspirasi utama produk ini) paling dekat bentuknya, tetapi: metode GitHub Account-nya hanya mendukung repo **publik** (repo privat harus lewat URL + SSH key manual per project dan alurnya terpencar per jenis project); project Go hanya menerima **binary jadi** (tidak ada `go build` dari source); modelnya **domain-first** (ikat domain → Nginx → SSL), bukan port-first untuk cloudflared; editor databasenya tidak terpadu (phpMyAdmin untuk MySQL, manajer terpisah untuk PostgreSQL, tanpa editor visual SQLite); dan terlalu besar untuk 1 admin / 1 server (FTP, mail server, DNS manager, fitur hosting multi-pelanggan, plugin berbayar).

Karena itu Wisnu memutuskan membangun panel sendiri yang ringan dan spesifik: **SatriaPanel** (Keputusan #3).

### 1.2 Tujuan Produk

1. Menjadikan deploy aplikasi dari Git/GitHub — termasuk repo privat — sebagai pekerjaan 1-klik yang aman (deploy gagal tidak mematikan versi yang sedang berjalan, dan rollback selalu tersedia).
2. Mengelola seluruh hidup aplikasi di satu tempat: proses (start/stop/restart/log), port & penerbitan lewat cloudflared, database + editor visual, Redis, Docker, cron, berkas, terminal, dan backup.
3. Menjaga data tetap selamat lewat auto backup terjadwal ke Google Drive beserta restore yang bisa dilakukan dari panel.
4. Tetap ringan di home server: panel adalah 1 binary Go yang mandiri dari runtime yang dikelolanya.

### 1.3 Ruang Lingkup

**Di dalam lingkup (Fase 1):** 1 server, 1 Admin; deploy Git/GitHub (PAT) manual + webhook; runtime PHP/Node.js/Python/Go native (PHP-FPM/PM2/systemd/Supervisor) dan mode Docker/Docker Compose; alokasi port + pengelolaan cloudflared penuh; provisioning + editor visual penuh PostgreSQL/MySQL/SQLite; Redis 1 instance bersama; manajemen cron penuh; auto backup ke Google Drive + restore; terminal web; file manager; notifikasi (panel + WhatsApp/Telegram); monitor & alert; audit log.

**Di luar lingkup Fase 1:** multi-server, multi-user/RBAC, template aplikasi 1-klik, target backup selain Google Drive (mis. S3), upload ZIP sebagai sumber deploy (Fase 2), FTP server, mail server, DNS manager untuk domain publik, dan fitur hosting untuk pelanggan/pihak lain dalam bentuk apa pun.

### 1.4 Definisi

| Istilah | Arti |
|---|---|
| **Aplikasi** | Satu unit yang dikelola panel: satu repo/sumber, satu runtime, satu port lokal, satu proses (atau satu proyek Compose). |
| **Rilis (release)** | Hasil satu deploy yang berhasil dibangun, tersimpan sebagai folder tersendiri; rilis yang aktif adalah yang sedang melayani. |
| **Deploy** | Proses mengambil kode (clone/pull), memasang dependensi, membangun, lalu mengaktifkan rilis baru. |
| **Port-first** | Model di mana setiap aplikasi pertama-tama mendapat port lokal unik; domain/HTTPS diurus Cloudflare Tunnel, bukan panel. |
| **Kredensial** | Rahasia yang disimpan panel: PAT GitHub, token/kredensial Cloudflare, kredensial Google OAuth, password database. Selalu terenkripsi dan tersamar. |
| **Pekerjaan bawaan** | Tugas terjadwal milik panel sendiri (auto backup, pembersihan retensi, pembersihan rilis lama) yang berjalan di scheduler internal panel. |

---

## 2. Gambaran Umum Produk

### 2.1 Bentuk Produk

SatriaPanel adalah **satu binary Go** yang memuat core/agent panel beserta aset antarmuka web di dalamnya (Keputusan #7). Ia berjalan sebagai **layanan systemd** di home server Ubuntu, menyimpan seluruh datanya dalam **satu berkas SQLite**, dan diakses lewat **panel.satriyadi.com** yang diterbitkan melalui Cloudflare Tunnel + Cloudflare Access (Keputusan #4, #5, #10). Pemasangan panel = menaruh 1 berkas binary + 1 unit systemd.

Panel bersifat **mandiri dari runtime yang dikelolanya**: bila Node.js/PM2, Python, PHP, atau Docker bermasalah, panel tetap hidup untuk memperbaikinya.

### 2.2 Prinsip Desain

1. **Port-first, bukan domain-first.** Setiap aplikasi mendapat 1 port lokal unik dari kolam port yang dikelola panel; panel menjamin tidak ada bentrok. Aplikasi cukup mendengarkan `localhost:<port>`; domain & HTTPS adalah urusan Cloudflare Tunnel.
2. **Native-first, Docker pelengkap** (Keputusan #2). Mode jalan dipilih per aplikasi, bawaan = native.
3. **Satu antarmuka proses.** Tombol Start/Stop/Restart/Log/Auto-restart selalu sama, apa pun driver di baliknya (PM2, systemd, Supervisor, Docker).
4. **Deploy = rilis, bukan menimpa.** Setiap deploy masuk folder rilis baru; deploy gagal tidak mematikan rilis aktif; rollback 1-klik ke rilis sebelumnya.
5. **Panel tidak dibuka telanjang ke internet.** Panel setara akses root ke server — ia hanya boleh dijangkau lewat tunnel + Cloudflare Access + login panel, dengan audit log.
6. **Rahasia tidak pernah tampil utuh.** Semua kredensial terenkripsi saat disimpan dan tersamar di antarmuka; setelah disimpan, tidak ada layar yang menampilkannya kembali secara utuh.

### 2.3 Arsitektur Ringkas

```
Internet ──► Cloudflare (Access + DNS) ──► cloudflared (di home server)
                                                │
                    ┌───────────────────────────┼───────────────────────────┐
                    ▼                           ▼                           ▼
          panel.satriyadi.com          app1.satriyadi.com          app2.satriyadi.com
                    │                           │                           │
              SatriaPanel                 localhost:3101            localhost:3102
              (binary Go, systemd,               │                           │
               data: SQLite panel)        Aplikasi A (PM2)          Aplikasi B (PHP-FPM)
                    │
        ┌───────────┼──────────────────┬──────────────────┐
        ▼           ▼                  ▼                  ▼
   Scheduler    Proses & deploy   Database server    Google Drive API
   internal     (git, PM2,        (PostgreSQL,       (auto backup,
   (cron +      systemd, Docker)  MySQL, SQLite,     scope drive.file)
   backup)                        Redis bersama)
```

Komponen di dalam binary yang sama: antarmuka web, API internal panel, agent pelaksana perintah (git/pm2/systemd/docker), scheduler internal (cron + pekerjaan bawaan), dan klien integrasi (GitHub API, Cloudflare API, Google Drive API).

### 2.4 Teknologi yang Terkunci dan yang Terbuka

Terkunci: bahasa **Go**, distribusi **1 binary** dengan aset web tertanam, layanan **systemd**, data panel **SQLite**, server target **Ubuntu**. Terbuka (detail teknis, Bab 14): framework/library Go, cara membangun antarmuka web, dan mekanisme pengelolaan multi-versi runtime.

---

## 3. Peran dan Hak Akses

### 3.1 Peran Tunggal: Admin

Sesuai Keputusan #1, Fase 1 hanya mengenal **satu peran: Admin** (Wisnu sendiri), dengan **satu akun login panel**. Tidak ada RBAC, tidak ada akun kedua, tidak ada hak akses bertingkat. Multi-user + peran adalah Fase 3 dan arsitektur data disiapkan agar penambahannya tidak membongkar ulang model yang ada (tabel pengguna sejak awal berbentuk jamak, walau Fase 1 hanya berisi 1 baris).

### 3.2 Hak Admin

Admin dapat melakukan seluruh fungsi panel: mengelola aplikasi & deploy, runtime & proses, port & cloudflared, database & editor visual (termasuk mengubah data), Redis, Docker, cron, file manager, terminal web, backup & restore, pengaturan & kredensial, serta membaca audit log dan notifikasi.

### 3.3 Batasan Akses

- Panel hanya dapat diakses melalui `panel.satriyadi.com` (tunnel). Port panel di server **tidak** dibuka ke jaringan publik dan tidak ada port-forwarding (Keputusan #4).
- Lapisan akses berurutan: Cloudflare Access (identitas) → login panel (username + password Admin). Keduanya wajib lolos.
- Terminal web hanya tersedia di dalam sesi panel yang sah (Bab 5, FR-TERM).

---

## 4. Alur Proses Utama

### 4.1 Menambah Aplikasi Baru (dari GitHub)

1. Admin membuka menu Aplikasi → **Tambah Aplikasi**, memilih sumber **GitHub**.
2. Panel menampilkan daftar repo dari PAT yang tersimpan (termasuk privat); Admin memilih repo dan branch.
3. Panel mendeteksi isi repo dan mengusulkan: runtime, versi runtime, perintah install/build/start, dan mode jalan (native/Docker) — Admin dapat mengubah usulan.
4. Panel mengalokasikan port bebas dari kolam port dan menampilkannya; Admin dapat mengganti ke port manual yang tidak bentrok.
5. Admin mengisi/meninjau variabel lingkungan (`.env`).
6. Admin (opsional) mengikat database dan/atau alokasi Redis; panel menawarkan menyuntikkan string koneksinya ke env secara otomatis.
7. Admin menyimpan → panel membuat catatan aplikasi, menyiapkan folder kerja, dan menawarkan **Deploy Sekarang**.
8. Bila hostname diisi (mis. `app1.satriyadi.com`) dan kredensial Cloudflare tersedia, panel menambahkan ingress ke config cloudflared, me-reload cloudflared, dan membuatkan rute DNS-nya (Bab 9.2). Bila kredensial belum diisi, panel menampilkan potongan config untuk dipasang manual (mode config-saja, Keputusan #5).

### 4.2 Deploy

1. Pemicu: tombol **Deploy** / **Redeploy**, atau webhook GitHub (push ke branch terpilih, bila auto-deploy aktif).
2. Panel mengantrekan deploy — **hanya 1 build berjalan dalam satu waktu** (Bab 15); deploy lain menunggu antrean.
3. Panel mengambil kode ke folder rilis baru (`releases/<waktu>-<commit>`): clone penuh untuk deploy pertama, ambil commit terbaru untuk berikutnya.
4. Panel menjalankan perintah install lalu build sesuai runtime; seluruh keluaran tampil sebagai **log deploy live** dan tersimpan per deploy.
5. Panel menjalankan pemeriksaan kesehatan awal (build selesai tanpa error; untuk mode Docker: image terbentuk).
6. Bila langkah 3–5 gagal: deploy ditandai **Gagal**, rilis aktif **tidak berubah**, aplikasi tetap berjalan pada versi lama, kegagalan dicatat + dinotifikasikan.
7. Bila berhasil: panel mengarahkan symlink/folder aktif ke rilis baru, (re)start proses lewat driver-nya, lalu menjalankan **health check** ke `localhost:<port>` aplikasi.
8. Bila health check gagal setelah start: panel otomatis mengembalikan proses ke rilis sebelumnya dan menandai deploy sebagai gagal-terpulihkan (rollback otomatis), lalu menotifikasi Admin.
9. Panel mencatat riwayat deploy (waktu, pemicu, commit, hasil, durasi) dan membersihkan rilis lama melebihi batas simpan (Bab 6).

### 4.3 Rollback

1. Admin membuka riwayat rilis aplikasi → memilih rilis lama → **Rollback**.
2. Panel meminta konfirmasi, mengarahkan folder aktif ke rilis terpilih, me-restart proses, dan menjalankan health check.
3. Hasil rollback dicatat sebagai entri riwayat deploy tersendiri (jenis: rollback).

### 4.4 Auto-Deploy via Webhook

1. Saat aplikasi GitHub dibuat/diedit, panel menawarkan membuat webhook di repo lewat API GitHub (izin Webhooks pada PAT).
2. Push ke branch terpilih mengirim peristiwa ke endpoint webhook panel (lewat tunnel, divalidasi dengan secret webhook per aplikasi).
3. Panel menjalankan alur 4.2. Webhook dengan secret tidak sah ditolak dan dicatat.

### 4.5 Backup Otomatis dan Restore

1. Scheduler internal menjalankan pekerjaan bawaan **Auto Backup** sesuai jadwal (bawaan harian 02:00 waktu server).
2. Panel membuat dump setiap database, menyalin data panel + config cloudflared + berkas persisten & `.env` aplikasi, memampatkan per item, dan menyusun `manifest.json` (isi, ukuran, checksum SHA-256).
3. Panel mengunggah paket ke folder `SatriaPanel-Backup/<nama-server>/<tanggal>/` di Google Drive (API Drive, scope `drive.file`), memverifikasi hasil unggahan, lalu menerapkan retensi (7 harian + 4 mingguan) dengan menghapus yang kedaluwarsa di Drive.
4. Hasil (berhasil/gagal, ukuran, durasi) tercatat di riwayat backup dan dinotifikasikan bila gagal.
5. **Restore:** Admin memilih paket backup di Drive dari panel → panel mengunduh, memverifikasi checksum terhadap manifest → Admin memilih item yang dipulihkan → panel membuat **backup pengaman** dari keadaan berjalan saat itu → memulihkan item terpilih → mencatat + menotifikasi hasil. Restore selalu meminta konfirmasi eksplisit dan tidak pernah berjalan otomatis.

### 4.6 Cron Aplikasi (contoh: Laravel)

1. Admin membuat pekerjaan cron terikat ke aplikasi Laravel dengan preset **Tiap Menit** dan perintah `php artisan schedule:run`.
2. Pada setiap jadwalnya, scheduler menjalankan perintah di folder rilis aktif aplikasi, sebagai user aplikasi, dengan `.env` aplikasi dimuat.
3. Setiap eksekusi tercatat (durasi, kode keluar, output); kegagalan memicu notifikasi.

---

## 5. Kebutuhan Fungsional

Penomoran: `FR-<MODUL>-<nomor>`. Kolom Fase menunjukkan fase ketersediaan; semua kebutuhan di bab ini Fase 1 kecuali dinyatakan lain.

### 5.1 Autentikasi & Sesi — FR-AUTH

| ID | Kebutuhan |
|---|---|
| FR-AUTH-01 | Panel menyediakan login dengan username + password untuk 1 akun Admin. Password disimpan sebagai hash kuat (bukan teks terang). |
| FR-AUTH-02 | Sesi login berakhir otomatis setelah masa tidak aktif yang dapat diatur; Admin dapat keluar dari semua sesi. |
| FR-AUTH-03 | Percobaan login gagal dibatasi (jeda/blokir sementara setelah beberapa kali gagal) dan setiap kegagalan dicatat di audit log. |
| FR-AUTH-04 | Admin dapat mengganti password setelah login; penggantian mengakhiri sesi lain yang sedang aktif. |
| FR-AUTH-05 | Seluruh halaman dan API panel menolak akses tanpa sesi yang sah — tidak ada halaman data yang terbuka tanpa login. |

### 5.2 Dashboard & Monitor — FR-DASH

| ID | Kebutuhan |
|---|---|
| FR-DASH-01 | Dashboard menampilkan keadaan server: CPU, RAM, disk (terpakai/total), uptime, dan beban sistem, diperbarui berkala tanpa muat ulang halaman. |
| FR-DASH-02 | Dashboard menampilkan ringkasan aplikasi: jumlah aplikasi, yang berjalan/berhenti/bermasalah, deploy terakhir, dan backup terakhir. |
| FR-DASH-03 | Monitor per aplikasi menampilkan pemakaian CPU/RAM prosesnya dan status terakhir health check. |
| FR-DASH-04 | Alert: panel memantau ambang yang dapat diatur (bawaan usulan: disk ≥ 90%, RAM ≥ 90%, aplikasi berhenti tidak wajar, Redis menipis) dan memunculkan alert di panel + notifikasi keluar (FR-NOTIF) saat ambang terlampaui. |
| FR-DASH-05 | Grafik pemakaian CPU/RAM/disk dalam rentang waktu (1 jam / 24 jam / 7 hari) dari data monitor yang disimpan panel. |

### 5.3 Aplikasi & Deploy — FR-APP

| ID | Kebutuhan |
|---|---|
| FR-APP-01 | Admin dapat membuat, mengubah, menonaktifkan, dan menghapus Aplikasi. Penghapusan meminta konfirmasi dan menawarkan pilihan menghapus folder kerja/rilisnya juga. |
| FR-APP-02 | Setiap Aplikasi memiliki: nama, slug, sumber (GitHub / URL Git HTTPS / URL Git SSH), repo & branch, folder kerja, runtime + versi, mode jalan (native/Docker/Compose), perintah install/build/start, port, hostname tunnel (opsional), dan status auto-deploy. |
| FR-APP-03 | Panel mendeteksi runtime & perintah bawaan dari isi repo (`package.json`, `composer.json`, `requirements.txt`/`pyproject.toml`, `go.mod`, `Dockerfile`, `docker-compose.yml`) dan mengusulkannya saat aplikasi dibuat; usulan dapat diubah Admin. |
| FR-APP-04 | Admin dapat mengelola variabel lingkungan per aplikasi lewat editor `.env` di panel; nilai sensitif tidak ditampilkan utuh setelah disimpan. Perubahan env menandai aplikasi "perlu restart" dan menawarkan restart. |
| FR-APP-05 | Deploy manual (Deploy / Redeploy) menjalankan alur Bab 4.2 dengan log live yang dapat digulir balik, dan log tersimpan per deploy. |
| FR-APP-06 | Setiap deploy menghasilkan rilis baru; panel menyimpan riwayat rilis & deploy (waktu, pemicu, commit, hasil, durasi, log). |
| FR-APP-07 | Rollback 1-klik ke rilis sebelumnya yang tersimpan (alur Bab 4.3). |
| FR-APP-08 | Deploy gagal tidak mengubah rilis aktif; kegagalan health check setelah start memicu rollback otomatis ke rilis sebelumnya (Bab 4.2 langkah 8). |
| FR-APP-09 | Build berjalan satu per satu (antrean deploy); antrean terlihat di panel dan deploy dalam antrean dapat dibatalkan sebelum berjalan. |
| FR-APP-10 | Auto-deploy: panel dapat membuat & menghapus webhook GitHub per aplikasi; peristiwa push yang sah memicu deploy; secret webhook per aplikasi memvalidasi setiap peristiwa. |
| FR-APP-11 | Panel menampilkan status aplikasi waktu-nyata: berjalan/berhenti/deploy/bermasalah, port, hostname, rilis aktif (commit), dan waktu deploy terakhir. |
| FR-APP-12 | (Fase 2) Upload ZIP sebagai sumber deploy untuk aplikasi tanpa repo Git. |

### 5.4 Integrasi GitHub — FR-GH

| ID | Kebutuhan |
|---|---|
| FR-GH-01 | Admin menyimpan 1 PAT GitHub di Pengaturan; token terenkripsi, tampil tersamar, tidak pernah tampil utuh lagi (Keputusan #6). |
| FR-GH-02 | Panel menampilkan daftar repo yang dapat diakses token (termasuk privat) beserta daftar branch-nya untuk dipilih saat membuat/mengubah aplikasi. |
| FR-GH-03 | Clone/pull repo privat dilakukan lewat HTTPS memakai token; token tidak ditulis ke log, tidak masuk ke berkas konfigurasi repo yang tersimpan di disk dalam teks terang di luar kebutuhan git, dan tidak tampil di antarmuka. |
| FR-GH-04 | Panel memperingatkan bila token mendekati kedaluwarsa (untuk token berumur terbatas) dan menyediakan penggantian token tanpa menghapus aplikasi yang ada. |
| FR-GH-05 | Bila token tidak sah/kedaluwarsa, panel menampilkan pesan yang jelas pada fungsi yang membutuhkannya (daftar repo, deploy, webhook) tanpa mematikan fungsi panel lain. |

### 5.5 Runtime & Pengelola Proses — FR-RUN

| ID | Kebutuhan |
|---|---|
| FR-RUN-01 | Runtime yang didukung: **PHP** (PHP-FPM pool per aplikasi; versi PHP per aplikasi dari daftar versi yang terpasang), **Node.js** (PM2), **Python** (venv per aplikasi; gunicorn/uvicorn; driver systemd/PM2/Supervisor), **Go** (`go build` dari source saat deploy; binary dijalankan via systemd). |
| FR-RUN-02 | Versi runtime dapat dipilih per aplikasi dari versi yang tersedia di server (mis. Node 20/22, Python 3.11/3.12, PHP 7.4–8.4, Go sesuai toolchain terpasang). Mekanisme penyediaan multi-versi adalah detail teknis (Bab 14). |
| FR-RUN-03 | Antarmuka proses seragam untuk semua driver (PM2, systemd, Supervisor, Docker): Start, Stop, Restart, status, log stdout/stderr live, pemakaian CPU/RAM per proses. |
| FR-RUN-04 | Proses aplikasi menyala otomatis saat server boot (sesuai driver: unit systemd / resurrect PM2 / kebijakan restart Docker). |
| FR-RUN-05 | Proses yang crash direstart otomatis oleh driver-nya; kejadian crash tercatat dan, bila berulang melewati ambang, memicu alert (FR-DASH-04). |
| FR-RUN-06 | Admin dapat mengatur restart terjadwal per aplikasi (opsional) melalui mekanisme cron panel (FR-CRON). |
| FR-RUN-07 | Log proses dapat dilihat live, digulir balik, dicari, dan diunduh; log lama diputar (rotasi) agar tidak memenuhi disk. |

### 5.6 Port & Cloudflared — FR-PORT

| ID | Kebutuhan |
|---|---|
| FR-PORT-01 | Panel mengelola kolam port: rentang port dapat diatur (bawaan usulan 3100–3999); alokasi otomatis memilih port bebas pertama; pilihan manual divalidasi tidak bentrok dengan aplikasi lain maupun layanan sistem yang diketahui. |
| FR-PORT-02 | Tabel pemetaan selalu tersedia: Aplikasi → Port lokal → Hostname tunnel → Status health check terakhir. |
| FR-PORT-03 | Panel mengelola layanan cloudflared (systemd): status, start/stop/restart, dan log-nya terlihat dari panel (Keputusan #5). |
| FR-PORT-04 | Setiap aplikasi/hostname ditambah, diubah, atau dihapus: panel menulis ulang ingress config cloudflared, **memvalidasinya sebelum diterapkan**, me-reload cloudflared, dan mencatat perubahan; config yang tidak sah tidak pernah diterapkan (config lama tetap berlaku). |
| FR-PORT-05 | Panel membuatkan rute DNS hostname aplikasi di zona Cloudflare milik Wisnu lewat API Cloudflare, dan menghapusnya saat aplikasi/hostname dihapus (dengan konfirmasi). |
| FR-PORT-06 | Kredensial Cloudflare (API token terbatas pada zona domain Wisnu dan/atau kredensial tunnel) disimpan terenkripsi dan tersamar (Keputusan #5). |
| FR-PORT-07 | Mode config-saja: bila kredensial Cloudflare belum diisi, panel tetap mengalokasikan port dan menampilkan potongan ingress config untuk dipasang manual; fungsi kelola-otomatis nonaktif sampai kredensial tersedia. |
| FR-PORT-08 | Health check: panel memeriksa berkala setiap port aplikasi; hasil terakhir dan riwayat gangguan tampil di daftar aplikasi & dashboard. |
| FR-PORT-09 | Panel itu sendiri diterbitkan lewat tunnel yang sama sebagai `panel.satriyadi.com` dan dilindungi Cloudflare Access (Keputusan #10). |

### 5.7 Database & Editor Visual — FR-DB

| ID | Kebutuhan |
|---|---|
| FR-DB-01 | Engine yang didukung: **PostgreSQL, MySQL/MariaDB, dan SQLite** — dalam **satu editor visual terpadu** (Keputusan #9). |
| FR-DB-02 | Provisioning 1-klik: buat database + user + password untuk PostgreSQL dan MySQL/MariaDB (password dibuatkan panel, tampil satu kali, lalu tersamar); untuk SQLite, buat berkas `.db` di folder aplikasi. |
| FR-DB-03 | Panel menyimpan daftar koneksi database (engine, host, port, nama, user, password terenkripsi & tersamar) dan dapat menyuntikkan string koneksi terpilih ke env aplikasi (sebagai variabel yang dapat dinamai Admin). |
| FR-DB-04 | Pohon skema: database → tabel/view → kolom (nama, tipe, nullable, kunci utama) dan indeks; dapat dimuat ulang dan dicari. |
| FR-DB-05 | Jelajah data: tabel tampil sebagai grid dengan paging, pengurutan per kolom, dan saringan sederhana (kolom = / mengandung / > / < nilai). |
| FR-DB-06 | Edit baris langsung di grid: tambah, ubah, hapus baris. **Pengaman wajib:** hapus dan ubah meminta konfirmasi yang menampilkan ringkasan perubahan; tabel tanpa kunci utama hanya dapat diubah lewat query (dengan peringatan); **semua perubahan data lewat editor tercatat di audit log** (tabel, jenis perubahan, jumlah baris, waktu — tanpa mencatat isi data sensitif secara penuh). |
| FR-DB-07 | Kotak query SQL: menjalankan query terhadap koneksi terpilih, menampilkan hasil sebagai grid atau pesan kesalahan engine apa adanya, menghentikan query yang berjalan terlalu lama (batas waktu dapat diatur), dan menyimpan riwayat query. |
| FR-DB-08 | Impor: berkas SQL dan CSV ke tabel/database terpilih, dengan laporan hasil (baris sukses/gagal). Ekspor: hasil query/tabel ke SQL dan CSV. |
| FR-DB-09 | Backup & restore per database dari panel (dump/restore manual), terpisah dari auto backup terjadwal (FR-BAK). |
| FR-DB-10 | Admin dapat menghapus database yang dikelola panel dengan konfirmasi berlapis (ketik nama database) — penghapusan tidak pernah terjadi dari satu klik. |
| FR-DB-11 | Editor juga dapat membuka database yang tidak dibuat panel (koneksi manual: SQLite dari berkas, PostgreSQL/MySQL dari host/port/kredensial) agar database aplikasi lama tetap dapat dikelola. |

### 5.8 Redis — FR-REDIS

| ID | Kebutuhan |
|---|---|
| FR-REDIS-01 | Panel mengelola **1 instance Redis bersama** sebagai layanan server (Keputusan #8): status, start/stop/restart, versi, pemakaian memori, dan jumlah key tampil di menu Redis. |
| FR-REDIS-02 | Setiap aplikasi yang diaktifkan Redis-nya mendapat **user ACL sendiri** yang polanya dibatasi ke prefix aplikasinya (`app_<slug>:*`) beserta password yang dibuatkan panel. |
| FR-REDIS-03 | String koneksi Redis (host, port, user, password, prefix) dapat disuntikkan otomatis ke env aplikasi saat deploy/restart. |
| FR-REDIS-04 | Penjelajah key terlingkup per aplikasi: cari menurut pola prefix, lihat tipe & nilai (string/hash/list/set/zset ditampilkan terbaca), lihat/atur TTL, hapus key (konfirmasi). Panel tidak menyediakan jalan pintas untuk melihat key di luar prefix aplikasi yang dipilih dari penjelajah per aplikasi. |
| FR-REDIS-05 | Flush hanya dapat dilakukan per prefix aplikasi (menghapus key berawalan prefix itu) dengan konfirmasi; tidak ada tombol flush-seluruh-instance di alur biasa. |
| FR-REDIS-06 | Menonaktifkan Redis pada aplikasi mencabut user ACL-nya (dengan konfirmasi) tanpa mengganggu aplikasi lain. |

### 5.9 Docker — FR-DOCKER

| ID | Kebutuhan |
|---|---|
| FR-DOCKER-01 | Menu Docker menampilkan daftar container (nama, image, status, port, aplikasi terkait), image, volume, dan network, beserta log per container. |
| FR-DOCKER-02 | Aplikasi mode Docker dibangun dari `Dockerfile` di repo; aplikasi mode Compose memperlakukan `docker-compose.yml` repo sebagai satu Aplikasi (layanan-layanannya tampil sebagai satu kesatuan). |
| FR-DOCKER-03 | Admin dapat menjalankan aplikasi dari image publik (nama image + tag) tanpa repo Git. |
| FR-DOCKER-04 | Aksi container: start/stop/restart, hapus (konfirmasi), dan prune image tak terpakai (konfirmasi, menampilkan perkiraan ruang yang dibebaskan). |
| FR-DOCKER-05 | Aplikasi Docker/Compose mendapat alokasi port panel yang sama dan diterbitkan lewat cloudflared dengan cara yang sama seperti aplikasi native (FR-PORT). Untuk Compose dengan banyak layanan, Admin memilih layanan & port internal yang diterbitkan. |

### 5.10 Manajemen Cron — FR-CRON

| ID | Kebutuhan |
|---|---|
| FR-CRON-01 | Menu **Cron** tersendiri menampilkan semua pekerjaan: nama, jenis (aplikasi/sistem/bawaan), jadwal (penjelasan manusiawi), status aktif, jalan terakhir, jalan berikutnya, dan hasil terakhir (Keputusan #12). |
| FR-CRON-02 | Pekerjaan cron aplikasi terikat ke satu aplikasi: berjalan di folder rilis aktif aplikasi, sebagai user aplikasi, dengan `.env` aplikasi dimuat. Pekerjaan sistem berjalan sebagai perintah shell bebas dengan folder kerja yang dapat diatur. |
| FR-CRON-03 | Jadwal dibuat lewat preset (tiap menit, tiap jam, harian pada jam tertentu, mingguan, bulanan) atau ekspresi cron 5-bidang kustom; panel menampilkan penjelasan jadwal dalam bahasa manusia dan pratinjau 3 waktu jalan berikutnya, serta menolak ekspresi tidak sah sebelum disimpan. |
| FR-CRON-04 | Aksi per pekerjaan: aktif/nonaktifkan, **Jalankan Sekarang**, sunting, duplikat, hapus (konfirmasi). Semua perubahan tercatat di audit log. |
| FR-CRON-05 | Setiap eksekusi tercatat: waktu mulai, durasi, status (berhasil/gagal/dilewati/timeout), kode keluar, dan output (stdout/stderr) yang dapat dibuka dari panel. Riwayat dibersihkan otomatis sesuai retensi (bawaan 30 hari, dapat diatur). |
| FR-CRON-06 | Pengaman jalan: timeout per pekerjaan (dapat diatur per pekerjaan) dan anti-tumpang-tindih — jadwal berikutnya dilewati & dicatat sebagai "dilewati" bila eksekusi sebelumnya masih berjalan. |
| FR-CRON-07 | Kegagalan eksekusi (kode keluar ≠ 0 atau timeout) memicu notifikasi (FR-NOTIF) dan tanda jelas di daftar cron. |
| FR-CRON-08 | Pekerjaan bawaan panel (auto backup, pembersihan retensi backup & riwayat, pembersihan rilis lama) tampil di daftar yang sama, jadwalnya dapat diatur, tetapi tidak dapat dihapus; penonaktifan hanya lewat saklar resminya masing-masing (mis. saklar auto backup di menu Backup). |
| FR-CRON-09 | Semua pekerjaan dijalankan oleh **scheduler internal panel**; panel tidak menulis ke crontab sistem. Bila layanan panel berhenti, pekerjaan tidak berjalan — keadaan ini terlihat dari status scheduler di dashboard. |

### 5.11 File Manager — FR-FILE

| ID | Kebutuhan |
|---|---|
| FR-FILE-01 | File manager menjelajahi folder aplikasi dan folder yang diizinkan panel: daftar berkas (nama, ukuran, waktu ubah), masuk folder, kembali. |
| FR-FILE-02 | Editor teks bawaan untuk berkas teks (termasuk `.env` dan berkas config): buka, sunting, simpan — dengan peringatan sebelum menimpa berkas yang berubah di disk sejak dibuka. |
| FR-FILE-03 | Aksi berkas: unggah, unduh, ganti nama, pindah, hapus (konfirmasi), buat berkas/folder baru, dan ekstrak/buat arsip ZIP. |
| FR-FILE-04 | File manager **tidak** menyediakan jalan pintas ke berkas kredensial panel maupun berkas data panel (SQLite panel, kunci enkripsi) — folder data panel dikecualikan dari penjelajahan. |
| FR-FILE-05 | Semua penghapusan dan penimpaan berkas lewat file manager tercatat di audit log. |

### 5.12 Terminal Web — FR-TERM

| ID | Kebutuhan |
|---|---|
| FR-TERM-01 | Terminal web menyediakan sesi shell interaktif ke server dari dalam panel, tanpa SSH terpisah (Keputusan #13). |
| FR-TERM-02 | Terminal hanya dapat dibuka dari sesi panel yang sah dan telah lolos Cloudflare Access; tidak ada endpoint terminal yang dapat dijangkau tanpa sesi panel. |
| FR-TERM-03 | Setiap sesi terminal tercatat di audit log: waktu buka, waktu tutup, dan alamat asal sesi. Isi perintah tidak direkam sebagai log panel (yang dicatat adalah sesinya). |
| FR-TERM-04 | Sesi terminal berakhir otomatis mengikuti berakhirnya sesi panel atau masa tidak aktif sesi (FR-AUTH-02). |
| FR-TERM-05 | Membuka terminal menampilkan peringatan bahwa sesi ini setara akses penuh ke server. |

### 5.13 Backup & Restore (Google Drive) — FR-BAK

| ID | Kebutuhan |
|---|---|
| FR-BAK-01 | Auto backup berjalan terjadwal lewat scheduler internal (bawaan: harian pukul 02:00 waktu server; jadwal dapat diubah dari menu Cron/Backup) dan dapat dipicu manual lewat tombol **Backup Sekarang**. |
| FR-BAK-02 | Cakupan backup: dump semua database yang dikelola panel; berkas SQLite panel + konfigurasi cloudflared/tunnel + kredensial terenkripsi; berkas persisten per aplikasi (folder data/upload yang ditandai pada aplikasi) dan `.env` setiap aplikasi. Kode sumber aplikasi **tidak** di-backup (sudah ada di Git). Snapshot Redis bersifat opsional per pengaturan (bawaan: tidak ikut). |
| FR-BAK-03 | Setiap paket backup berisi berkas terkompresi per item dan `manifest.json` berisi daftar item, ukuran, dan checksum SHA-256 per berkas. |
| FR-BAK-04 | Unggahan memakai **API Google Drive langsung dari panel** (Keputusan #11): OAuth dengan scope `drive.file` saja; refresh token terenkripsi; folder tujuan `SatriaPanel-Backup/<nama-server>/<tanggal>/`. (Nama folder disesuaikan dari draf konsep yang menulis `CloudPanel-Backup`, mengikuti nama final produk di Keputusan #10.) |
| FR-BAK-05 | Retensi: 7 backup harian terakhir + 4 mingguan (±35 hari) dipertahankan di Drive; yang lebih tua dihapus otomatis oleh panel dan penghapusannya tercatat di riwayat backup. |
| FR-BAK-06 | Riwayat backup tampil di panel: waktu, pemicu (jadwal/manual), status, ukuran total, durasi, dan pesan kesalahan bila gagal. Kegagalan backup memicu notifikasi (FR-NOTIF). |
| FR-BAK-07 | Restore dilakukan dari panel: daftar paket di Drive → unduh → verifikasi checksum terhadap manifest (paket yang gagal verifikasi tidak dapat direstore) → pilih item → konfirmasi eksplisit → panel membuat backup pengaman keadaan berjalan → pulihkan item terpilih → catat & notifikasi hasil. |
| FR-BAK-08 | Koneksi Google diatur lewat panduan di panel: panel menampilkan alamat redirect OAuth-nya, Admin membuat OAuth client di Google Cloud Console, memasukkan Client ID & Secret (terenkripsi, tersamar), lalu login Google sekali dari panel. Panel menampilkan status koneksi Drive dan dapat memutuskan koneksi (mencabut token tersimpan). |
| FR-BAK-09 | Bila koneksi Drive belum diatur atau token tidak sah, auto backup ditandai nonaktif dengan alasan yang jelas di menu Backup & dashboard — panel tidak berpura-pura membackup. |

### 5.14 Notifikasi — FR-NOTIF

| ID | Kebutuhan |
|---|---|
| FR-NOTIF-01 | Notifikasi di dalam panel (kotak masuk/lonceng): deploy berhasil/gagal, rollback otomatis, cron gagal, backup gagal, alert ambang (FR-DASH-04), token GitHub mendekati kedaluwarsa, cloudflared bermasalah. Notifikasi dapat ditandai dibaca dan dibersihkan. |
| FR-NOTIF-02 | Notifikasi keluar panel ke **WhatsApp dan/atau Telegram** untuk kejadian yang dipilih Admin per jenis kejadian (pengaturan per jenis: panel saja / keluar juga). Saluran persis dan cara menghubungkannya adalah detail di Bab 14. |
| FR-NOTIF-03 | Kegagalan pengiriman notifikasi keluar tidak menggagalkan proses yang memicunya; kegagalan pengiriman tercatat. |
| FR-NOTIF-04 | Notifikasi tidak pernah memuat isi kredensial atau isi data aplikasi; hanya nama aplikasi/kejadian, waktu, dan ringkasan hasil. |

### 5.15 Pengaturan & Kredensial — FR-SET

| ID | Kebutuhan |
|---|---|
| FR-SET-01 | Menu Pengaturan memuat: profil server (nama server — dipakai untuk folder backup), kolam port, batas simpan rilis, retensi riwayat cron & log, ambang alert, dan masa tidak aktif sesi. |
| FR-SET-02 | Pengelolaan kredensial terpusat: PAT GitHub (FR-GH-01), kredensial Cloudflare (FR-PORT-06), koneksi Google Drive (FR-BAK-08), dan saluran notifikasi keluar — semuanya terenkripsi, tersamar, dengan status & tombol uji koneksi masing-masing. |
| FR-SET-03 | Kunci enkripsi panel dibuat saat pemasangan pertama dan disimpan terpisah dari berkas SQLite panel; kehilangan kunci berarti kredensial tersimpan tidak dapat dibaca kembali (panel harus menampilkannya sebagai keadaan yang jelas saat pemasangan, dan kunci termasuk yang di-backup bersama data panel dalam keadaan terenkripsi). |
| FR-SET-04 | Uji koneksi per kredensial menampilkan hasil yang dapat ditindaklanjuti (berhasil / ditolak / kedaluwarsa / tidak terjangkau) tanpa menampilkan isi kredensial. |

### 5.16 Audit Log — FR-AUDIT

| ID | Kebutuhan |
|---|---|
| FR-AUDIT-01 | Audit log mencatat kejadian penting: login (berhasil/gagal), deploy & rollback, start/stop/restart proses, perubahan env, perubahan data lewat editor database (FR-DB-06), perubahan cron, buka/tutup sesi terminal, restore backup, perubahan kredensial & pengaturan, dan penghapusan apa pun. |
| FR-AUDIT-02 | Setiap entri berisi: waktu, aktor (Admin), jenis kejadian, objek terdampak, dan ringkasan — tanpa isi kredensial dan tanpa isi data sensitif secara penuh. |
| FR-AUDIT-03 | Audit log dapat disaring (jenis kejadian, rentang waktu, aplikasi) dan diekspor; entri audit tidak dapat diubah atau dihapus dari antarmuka panel. |

### 5.17 Antarmuka MCP — FR-MCP

| ID | Kebutuhan |
|---|---|
| FR-MCP-01 | Panel menyediakan server MCP (Model Context Protocol) dengan transport **Streamable HTTP** pada endpoint `/mcp`, tertanam dalam binary panel yang sama — bukan proses atau layanan terpisah (detail di Bab 18). |
| FR-MCP-02 | Otentikasi memakai **token MCP khusus** yang dibuat dari menu Pengaturan: nama token, himpunan scope, waktu buat, pemakaian terakhir, dan tombol cabut. Token hanya ditampilkan utuh satu kali saat dibuat dan disimpan sebagai hash; token panel-login dan kredensial lain (FR-SET) tidak berlaku di endpoint ini. |
| FR-MCP-03 | Setiap tool memiliki scope minimum (Bab 18.3); server menolak panggilan di luar scope token dengan kesalahan yang jelas. Scope ditegakkan di server, bukan di klien. |
| FR-MCP-04 | Katalog tool mencakup seluruh fungsi panel termasuk yang destruktif (Keputusan #14), dipetakan dari modul FR yang ada (katalog lengkap di Bab 18.4). |
| FR-MCP-05 | Tool destruktif (hapus aplikasi/database, flush Redis, restore backup, query penulisan/DDL, tulis/hapus berkas, perintah shell) wajib menerima parameter konfirmasi eksplisit beserta pengenal target yang diulang oleh pemanggil; tanpa itu server menolak menjalankan. |
| FR-MCP-06 | Operasi lama (deploy, backup, restore) bersifat asinkron di MCP seperti di panel: tool memulai operasi dan mengembalikan ID; status dipantau lewat tool status terkait. Tidak ada panggilan MCP yang menunggu sampai operasi lama selesai. |
| FR-MCP-07 | Seluruh panggilan MCP — berhasil maupun ditolak — tercatat di audit log dengan nama token, nama tool, target, dan hasil (FR-AUDIT), dan tampil di layar Audit Log dengan penanda sumber "MCP". |
| FR-MCP-08 | Endpoint MCP hanya terjangkau lewat jalur yang sama dengan panel (tunnel yang sama); tidak ada port MCP terpisah yang terbuka. Panel menerapkan batas laju panggilan per token. |
| FR-MCP-09 | Yang **tidak** diekspos lewat MCP dalam keadaan apa pun: membaca isi kredensial tersimpan (PAT GitHub, token Cloudflare, refresh token Drive), membuat/mencabut token MCP lain, dan mengubah password Admin. |
| FR-MCP-10 | Menu Pengaturan menampilkan daftar token MCP beserta scope & pemakaian terakhirnya, dan saklar utama untuk menonaktifkan endpoint MCP sepenuhnya tanpa menghapus token yang ada. |

---

## 6. Aturan Bisnis

1. **Satu aplikasi = satu port.** Port unik per aplikasi ditegakkan panel; tidak ada dua aplikasi berbagi port, dan port panel sendiri berada di luar kolam port aplikasi.
2. **Deploy tidak pernah mematikan yang sedang berjalan.** Rilis aktif hanya berganti setelah build berhasil dan start lolos; kegagalan di titik mana pun mempertahankan rilis aktif (atau mengembalikannya lewat rollback otomatis).
3. **Satu build dalam satu waktu.** Deploy antre; cron dan backup tetap berjalan, tetapi backup tidak dimulai saat ada build yang berjalan (ditunda sampai build selesai, dicatat sebagai tertunda).
4. **Batas simpan rilis.** Panel menyimpan sejumlah rilis terakhir per aplikasi (bawaan usulan: 5, dapat diatur); rilis aktif tidak pernah ikut dibersihkan. Pembersihan dilakukan pekerjaan bawaan dan tercatat.
5. **Kredensial hanya masuk, tidak pernah keluar.** Setelah disimpan, kredensial tidak dapat ditampilkan utuh dari layar mana pun; satu-satunya jalan mengganti adalah menimpa dengan yang baru.
6. **Editor database menghormati kunci utama.** Ubah/hapus baris di grid hanya untuk tabel berkunci utama; tabel tanpa kunci utama hanya dapat diubah lewat query dengan peringatan.
7. **Flush Redis hanya per prefix.** Tidak ada operasi panel yang menghapus seluruh isi instance Redis bersama dalam satu aksi biasa.
8. **Restore selalu didahului backup pengaman** dan konfirmasi eksplisit; tidak ada restore otomatis dalam keadaan apa pun.
9. **Config cloudflared yang tidak sah tidak diterapkan.** Validasi mendahului reload; kegagalan validasi mempertahankan config lama.
10. **Pekerjaan bawaan tidak dapat dihapus**, hanya dijadwal ulang atau dinonaktifkan lewat saklar resminya.
11. **Semua penghapusan berkonfirmasi**, dan penghapusan database berkonfirmasi berlapis (ketik nama).
12. **Waktu panel = waktu server.** Semua jadwal (cron, backup) dan cap waktu mengikuti zona waktu server; zona waktu server ditampilkan di menu Cron dan Backup agar tidak salah tafsir.
13. **MCP tunduk pada scope token.** Tool MCP tidak pernah memiliki kuasa melebihi scope token pemanggilnya, dan tool destruktif selalu memerlukan konfirmasi eksplisit per panggilan (FR-MCP-05) — kepemilikan token saja tidak cukup.
14. **Kredensial tidak keluar lewat MCP.** Tidak ada tool MCP yang mengembalikan isi kredensial tersimpan dalam bentuk apa pun (FR-MCP-09).

---

## 7. Model Data Panel

Seluruh data panel disimpan dalam **satu berkas SQLite**. Tabel berikut adalah kebutuhan minimum; tipe & indeks adalah detail teknis selama makna kolomnya dipertahankan. Kolom kredensial bertanda *(enkripsi)* selalu disimpan terenkripsi (FR-SET-03).

| Tabel | Kolom utama | Keterangan |
|---|---|---|
| `admin_users` | id, username, password_hash, created_at, updated_at | Fase 1 berisi tepat 1 baris (Keputusan #1); bentuk jamak untuk Fase 3. |
| `apps` | id, name, slug (unik), source_type (github/git_https/git_ssh/image), repo_url, branch, runtime, runtime_version, mode (native/docker/compose), install_cmd, build_cmd, start_cmd, port (unik), hostname, auto_deploy (bool), webhook_secret *(enkripsi)*, workdir, persistent_dirs (daftar folder data), status, active_release_id, created_at, updated_at | Satu baris per Aplikasi. |
| `app_env` | id, app_id → apps, key, value *(enkripsi)*, is_secret (bool) | Variabel lingkungan per aplikasi. |
| `releases` | id, app_id, commit_hash, dir_path, built_at, status (aktif/arsip/gagal) | Satu baris per rilis yang dibangun. |
| `deployments` | id, app_id, release_id, trigger (manual/webhook/rollback), status (antre/berjalan/berhasil/gagal/gagal-terpulihkan), log_path, started_at, finished_at | Riwayat deploy & rollback. |
| `databases` | id, engine (postgresql/mysql/sqlite), name, host, port, username, password *(enkripsi)*, file_path (SQLite), managed_by_panel (bool), app_id (nullable), created_at | Database yang dibuat panel maupun koneksi manual (FR-DB-11). |
| `redis_allocations` | id, app_id (unik), acl_user, acl_password *(enkripsi)*, prefix, created_at | Alokasi Redis per aplikasi (FR-REDIS-02). |
| `cron_jobs` | id, name, kind (app/system/builtin), app_id (nullable), command, workdir, schedule_expr, enabled, timeout_seconds, builtin_key (nullable), created_at, updated_at | Pekerjaan bawaan memiliki `builtin_key` dan tidak dapat dihapus. |
| `cron_runs` | id, job_id, started_at, finished_at, status (berhasil/gagal/dilewati/timeout), exit_code, output (teks, dipotong pada batas wajar) | Riwayat eksekusi, retensi bawaan 30 hari. |
| `backups` | id, trigger (jadwal/manual/pengaman), status, drive_folder, total_size, manifest_summary, started_at, finished_at, error | Riwayat backup & restore merujuk ke baris ini. |
| `credentials` | id, kind (github_pat/cloudflare/google_oauth/notif_channel), label, secret *(enkripsi)*, meta (JSON non-rahasia: mis. tanggal kedaluwarsa token, status koneksi), updated_at | Satu tempat untuk semua kredensial (FR-SET-02). |
| `notifications` | id, kind, title, body, app_id (nullable), is_read, created_at | Kotak masuk notifikasi panel (FR-NOTIF-01). |
| `audit_logs` | id, actor, action, object_type, object_id, summary, created_at | Tidak dapat diubah/dihapus dari antarmuka (FR-AUDIT-03). |
| `settings` | key, value | Pengaturan panel (FR-SET-01): nama server, kolam port, batas rilis, retensi, ambang alert, sesi. |
| `metric_samples` | id, scope (server/app), app_id (nullable), cpu_pct, mem_bytes, disk_pct (server), sampled_at | Sampel monitor untuk grafik (FR-DASH-05), diputar berkala. |

Relasi penting: `apps` menjadi pusat — env, rilis, deploy, alokasi Redis, cron aplikasi, dan database terikat menunjuk ke `apps`. Menghapus aplikasi menonaktifkan dulu seluruh keterkaitannya (proses berhenti, webhook dihapus, ingress dicabut) sebelum catatannya dihapus.

---

## 8. Daftar Layar

Struktur menu mengikuti kebiasaan aaPanel yang diadopsi di konsep (Aplikasi, Database, Docker, Files, Terminal, Cron, Redis, Backup, Monitor, Pengaturan), disesuaikan dengan model port-first:

| # | Layar | Isi utama | FR terkait |
|---|---|---|---|
| L-01 | Login | Username + password Admin | FR-AUTH |
| L-02 | Dashboard | Monitor server, ringkasan aplikasi, deploy & backup terakhir, alert aktif | FR-DASH |
| L-03 | Aplikasi (daftar) | Tabel aplikasi: status, runtime, port, hostname, rilis aktif, aksi cepat (deploy/start/stop/restart/log) | FR-APP, FR-RUN |
| L-04 | Aplikasi (tambah/edit) | Formulir sumber Git/GitHub, deteksi runtime, perintah, port, hostname, env | FR-APP, FR-GH |
| L-05 | Detail Aplikasi | Tab: Ringkasan & proses · Deploy & rilis (riwayat, rollback) · Env · Log · Database terikat · Cron aplikasi | FR-APP, FR-RUN, FR-CRON |
| L-06 | Log Deploy | Log live saat deploy berjalan; log tersimpan per deploy | FR-APP-05 |
| L-07 | Database (daftar) | Daftar database/koneksi per engine, provisioning 1-klik | FR-DB-01..03 |
| L-08 | Editor Database | Pohon skema, grid data + edit baris, kotak query + riwayat, impor/ekspor, backup/restore per database | FR-DB-04..10 |
| L-09 | Redis | Status instance, alokasi per aplikasi, penjelajah key per prefix, flush per prefix | FR-REDIS |
| L-10 | Docker | Container/image/volume/network, log, prune | FR-DOCKER |
| L-11 | Cron | Daftar pekerjaan (aplikasi/sistem/bawaan), buat/edit (preset/ekspresi + pratinjau), riwayat eksekusi + output | FR-CRON |
| L-12 | Files | File manager + editor teks | FR-FILE |
| L-13 | Terminal | Terminal web + peringatan sesi | FR-TERM |
| L-14 | Backup | Status koneksi Drive, jadwal, Backup Sekarang, riwayat, daftar paket di Drive, restore | FR-BAK |
| L-15 | Cloudflared & Port | Status cloudflared, tabel pemetaan port → hostname, mode config-saja bila aktif | FR-PORT |
| L-16 | Notifikasi | Kotak masuk notifikasi panel | FR-NOTIF-01 |
| L-17 | Audit Log | Daftar audit dengan saringan & ekspor | FR-AUDIT |
| L-18 | Pengaturan | Profil server, kolam port, retensi & ambang, kredensial (GitHub/Cloudflare/Google/notifikasi) + uji koneksi, ganti password | FR-SET, FR-AUTH-04 |

Seluruh layar responsif untuk laptop dan ponsel (Admin mengelola dari mana pun lewat tunnel), dengan fungsi yang sama di kedua bentuk — tidak ada fungsi yang hanya tersedia di satu jenis perangkat.

Sistem desain visual dan contoh tampilan keenam layar utama diatur di **Bab 17 — Desain Antarmuka**.

---

## 9. Integrasi Eksternal

### 9.1 GitHub (API + Git + Webhook)

- **Otentikasi:** 1 PAT (Keputusan #6). Dianjurkan fine-grained PAT terbatas pada repo yang di-deploy dengan izin Contents (read) dan Webhooks (read/write); jenis token akhir adalah pilihan Wisnu saat membuatnya.
- **Pemakaian:** daftar repo & branch (API), clone/pull HTTPS, pembuatan/penghapusan webhook (API).
- **Webhook masuk:** endpoint panel menerima peristiwa push, memvalidasi secret per aplikasi dan branch sebelum memicu deploy (FR-APP-10).
- **Kegagalan:** token ditolak/kedaluwarsa → fungsi GitHub menampilkan kesalahan yang jelas; panel dan aplikasi yang sudah berjalan tidak ikut mati (FR-GH-05).

### 9.2 Cloudflare (Tunnel + DNS + Access)

- **cloudflared** berjalan sebagai layanan systemd yang dikelola panel: config ingress ditulis & divalidasi panel, reload dilakukan panel (FR-PORT-03..04).
- **DNS:** rute hostname aplikasi dibuat/dihapus lewat API Cloudflare dengan token terbatas pada zona domain Wisnu (FR-PORT-05).
- **Access:** `panel.satriyadi.com` dilindungi Cloudflare Access sebagai lapisan identitas sebelum login panel (Bab 3.3).
- **Cadangan:** mode config-saja bila kredensial belum tersedia (FR-PORT-07).

### 9.3 Google Drive (API)

- **Otentikasi:** OAuth 2.0 dengan scope `drive.file` saja (Keputusan #11); refresh token terenkripsi. Wisnu membuat OAuth client sekali di Google Cloud Console dengan panduan panel (FR-BAK-08).
- **Pemakaian:** membuat folder `SatriaPanel-Backup/<nama-server>/<tanggal>/`, mengunggah paket + manifest, memverifikasi unggahan, menghapus paket kedaluwarsa (retensi), dan mengunduh paket saat restore.
- **Batasan scope:** dengan `drive.file`, panel secara teknis tidak dapat melihat/mengubah berkas Drive lain di luar buatannya sendiri — batas ini disengaja dan dipertahankan.

### 9.4 Saluran Notifikasi Keluar

- WhatsApp dan/atau Telegram untuk notifikasi keluar (FR-NOTIF-02). Mekanisme persisnya (bot Telegram resmi vs gateway WhatsApp) adalah hal terbuka di Bab 14; kebutuhan fungsionalnya tidak bergantung pada pilihan itu: kirim ringkasan kejadian, tanpa kredensial, kegagalan kirim tidak menggagalkan proses pemicu.

---

## 10. Kebutuhan Non-Fungsional

| ID | Aspek | Kebutuhan |
|---|---|---|
| NFR-01 | Ringan | Panel (di luar build aplikasi) berjalan dengan pemakaian memori kecil yang wajar untuk binary Go (target kerja: ≤ 150 MB RAM saat diam) agar tidak berebut sumber daya dengan aplikasi di home server. |
| NFR-02 | Pemasangan | Pemasangan = 1 berkas binary + 1 unit systemd + berkas data panel; tanpa dependensi runtime untuk panelnya sendiri. Pembaruan panel = mengganti binary dan me-restart layanan, dengan migrasi data panel otomatis dan dapat kembali ke binary sebelumnya. |
| NFR-03 | Kemandirian | Panel tetap berfungsi saat runtime yang dikelolanya bermasalah (Node/PM2/Python/PHP/Docker mati tidak mematikan panel). |
| NFR-04 | Keamanan | Seluruh kredensial terenkripsi saat disimpan (kunci terpisah dari berkas data, FR-SET-03); password Admin ter-hash kuat; tidak ada kredensial di log; panel hanya dapat dijangkau lewat tunnel + Access + login (Bab 3.3). |
| NFR-05 | Ketersediaan panel | Panel menyala otomatis saat server boot (systemd) dan restart otomatis bila crash. |
| NFR-06 | Keandalan deploy | Deploy yang terputus di tengah (panel/server restart) tidak meninggalkan rilis setengah aktif: saat panel menyala kembali, keadaan aplikasi diverifikasi terhadap rilis aktif terakhir yang tercatat. |
| NFR-07 | Kinerja antarmuka | Halaman utama panel termuat < 3 detik pada koneksi tunnel biasa; log live dan monitor diperbarui lewat koneksi persisten (WebSocket/sejenis), bukan muat ulang halaman. |
| NFR-08 | Batas sumber daya | Build berjalan satu per satu (Bab 6 aturan 3); pemakaian disk oleh rilis, log, dan riwayat dibatasi oleh retensi yang dapat diatur. |
| NFR-09 | Bahasa antarmuka | Antarmuka panel berbahasa Indonesia. |
| NFR-10 | Pencatatan waktu | Semua cap waktu disimpan konsisten (UTC) dan ditampilkan dalam zona waktu server; zona waktu server ditampilkan di tempat jadwal dibuat (Cron, Backup). |

---

## 11. Batasan dan Asumsi

**Batasan (dari Keputusan Terkunci):**

1. Satu server dan satu Admin di Fase 1 (Keputusan #1); server pertama adalah home server di rumah (Keputusan #4) — bila internet/listrik rumah mati, panel dan aplikasi tidak terjangkau, dan konsekuensi ini diterima.
2. Integrasi GitHub hanya PAT di Fase 1 (Keputusan #6) — bukan GitHub App, bukan Deploy Key.
3. Panel ditulis dalam Go sebagai 1 binary (Keputusan #7); tidak ada komponen panel yang bergantung pada Node.js/Python/PHP.
4. Database yang didukung editor: PostgreSQL, MySQL/MariaDB, SQLite (Keputusan #9). MongoDB tidak termasuk Fase 1 (tidak disebut dalam kebutuhan asli; lihat Bab 14).
5. Redis hanya 1 instance bersama (Keputusan #8); tidak ada mode instance per aplikasi.
6. Target backup Fase 1 hanya Google Drive (Keputusan #11).

**Asumsi:**

1. Home server menjalankan Ubuntu dengan systemd, cloudflared dapat dipasang, dan domain `satriyadi.com` dikelola di Cloudflare dengan zona yang dapat diakses API token Wisnu.
2. Wisnu memiliki akun GitHub dengan repo privat yang akan di-deploy, dan akun Google dengan ruang Drive yang cukup untuk retensi backup ±35 hari.
3. Runtime server (PHP-FPM, Node.js/PM2, Python, Go toolchain, Docker, PostgreSQL, MySQL/MariaDB, Redis) dipasang di server sebagai layanan sistem; panel mengelolanya, dan pemasangan awal runtime di luar panel dapat dibantu panduan panel (daftar periksa pemasangan adalah bagian dari panduan, bukan fungsi panel Fase 1).
4. Jadwal pemakaian panel mengikuti kesiapan home server (server disiapkan terpisah, searah rencana home server Flow API).

---

## 12. Fase Pengembangan dan Kriteria Penerimaan

### 12.1 Fase 1 — Rilis Lengkap (seluruh kebutuhan Bab 5 kecuali FR-APP-12)

Cakupan: FR-AUTH, FR-DASH, FR-APP (selain FR-APP-12), FR-GH, FR-RUN, FR-PORT, FR-DB, FR-REDIS, FR-DOCKER, FR-CRON, FR-FILE, FR-TERM, FR-BAK, FR-NOTIF, FR-SET, FR-AUDIT, beserta Bab 6, 7, 9, dan 10.

**Kriteria penerimaan Fase 1** (seluruhnya harus lulus pada home server):

1. Panel terpasang sebagai 1 binary + layanan systemd, dapat diakses hanya lewat `panel.satriyadi.com` (Access + login), dan port panel tidak terbuka ke publik.
2. Admin dapat menambah aplikasi dari repo GitHub **privat** (daftar repo & branch tampil dari PAT), deploy berhasil untuk masing-masing satu contoh nyata: PHP, Node.js, Python, dan Go (Go dibangun dari source di server).
3. Push ke branch terpilih memicu auto-deploy lewat webhook; deploy yang sengaja dibuat gagal **tidak** mematikan versi berjalan, dan health check yang gagal memicu rollback otomatis.
4. Rollback manual ke rilis sebelumnya berhasil dalam 1 tindakan dari riwayat rilis.
5. Setiap aplikasi terbit di hostname-nya sendiri lewat tunnel tanpa Admin menyentuh config cloudflared/DNS secara manual; config yang tidak sah (diuji dengan data rusak) tidak diterapkan.
6. Editor database: untuk PostgreSQL, MySQL, dan SQLite — pohon skema tampil, grid data dapat diurut/saring, baris dapat ditambah/diubah/dihapus (dengan konfirmasi + tercatat di audit), query dapat dijalankan, impor/ekspor SQL & CSV berhasil, backup/restore per database berhasil.
7. Redis: dua aplikasi tidak dapat membaca key satu sama lain (diuji dengan user ACL masing-masing); flush satu aplikasi tidak menyentuh key aplikasi lain; string koneksi tersuntik ke env.
8. Docker: aplikasi dari Dockerfile dan proyek Compose berjalan dan terbit lewat port panel; prune bekerja dengan konfirmasi.
9. Cron: pekerjaan `php artisan schedule:run` tiap menit berjalan dengan env aplikasi; riwayat + output tercatat; pekerjaan yang digantung sengaja (timeout) ditandai timeout dan tidak menumpuk; pekerjaan bawaan tampil dan tidak dapat dihapus.
10. Auto backup berjalan pada jadwalnya, paket + manifest muncul di Drive pada folder yang benar, dan **restore dari paket tersebut berhasil** memulihkan satu database contoh setelah isinya sengaja diubah; checksum yang dirusak membuat paket ditolak untuk restore.
11. Terminal web hanya terbuka dalam sesi panel yang sah dan sesinya tercatat di audit; file manager tidak dapat membuka folder data panel.
12. Notifikasi: kegagalan deploy, cron, dan backup muncul di kotak masuk panel dan terkirim ke saluran keluar yang diatur; tidak ada notifikasi yang memuat kredensial.
13. Audit log memuat seluruh kejadian pada pengujian 1–12 dan tidak dapat dihapus dari antarmuka.
14. MCP: klien MCP standar dapat terhubung ke `/mcp` dengan token MCP; panggilan tool di luar scope token ditolak; tool destruktif tanpa parameter konfirmasi ditolak; deploy lewat MCP berjalan asinkron dan statusnya terbaca sampai selesai; seluruh panggilan tercatat di audit log berpenanda MCP.
15. MCP: token yang dicabut berhenti bekerja seketika; tidak ada tool yang dapat mengembalikan isi kredensial tersimpan (diuji dengan meminta PAT GitHub & token Cloudflare lewat MCP — harus ditolak/tidak tersedia).

### 12.2 Fase 2 — Penyempurnaan

- FR-APP-12: upload ZIP sebagai sumber deploy.
- Penyempurnaan yang muncul dari pemakaian Fase 1, diprioritaskan Wisnu saat fase ini dimulai.

Kriteria penerimaan: aplikasi tanpa repo Git dapat di-deploy dari ZIP dengan alur rilis & rollback yang sama seperti aplikasi Git.

### 12.3 Fase 3 — Skala

- Multi-server / agent jarak jauh (1 panel, banyak server).
- Multi-user + peran (RBAC).
- Template aplikasi 1-klik.
- Target backup tambahan (mis. S3).

Kriteria penerimaan Fase 3 ditetapkan pada PRD fase tersebut.

---

## 13. Keputusan Desain Terkunci

Seluruh keputusan diambil Wisnu pada 20 November 2026 dalam penyusunan Konsep Awal SatriaPanel (Draft v0.1 → v0.16). PRD ini tidak mengubah satu pun.

| # | Keputusan | Isi ringkas |
|---|---|---|
| #1 | Cakupan & pemakai | Hanya untuk Wisnu: 1 admin, 1 server dulu; multi-user/multi-server Fase 3. |
| #2 | Model runtime | Native-first ala aaPanel (PHP-FPM/PM2/systemd/Supervisor), Docker pelengkap; mode per aplikasi, bawaan native. |
| #3 | Bangun vs fork | Bangun sendiri, ringan & spesifik; bukan fork panel mana pun; aaPanel inspirasi bentuk, bukan kode. |
| #4 | Server target pertama | Home server di rumah via cloudflared; VPS menyusul bila perlu. |
| #5 | Pengelolaan cloudflared | Kelola penuh oleh panel (tunnel, ingress config, DNS otomatis); config-saja hanya cadangan. |
| #6 | Integrasi GitHub | PAT — 1 token untuk semua repo; terenkripsi, tersamar, peringatan kedaluwarsa. |
| #7 | Bahasa panel | Go — 1 binary (core/agent + aset web tertanam), layanan systemd, mandiri dari runtime yang dikelola. |
| #8 | Redis | 1 instance bersama; pisah per aplikasi via user ACL + prefix key. |
| #9 | Editor database | Penuh sejak Fase 1 untuk PostgreSQL + MySQL + SQLite. |
| #10 | Nama & domain | **SatriaPanel** — panel.satriyadi.com. |
| #11 | Koneksi Google Drive | API Drive langsung dari panel; OAuth scope `drive.file`; refresh token terenkripsi; tanpa rclone. |
| #12 | Manajemen cron | Ikut Fase 1 — menu Cron lengkap sejak awal; scheduler internal panel. |
| #13 | Isi Fase 2 → Fase 1 | Terminal web, file manager, notifikasi, monitor & alert seluruhnya masuk Fase 1. |
| #14 | Antarmuka MCP | Panel menyediakan MCP API (Streamable HTTP di `/mcp`) dengan cakupan **penuh — termasuk tool destruktif** — dijaga scope token khusus per token (Bab 18). |

---

## 14. Hal yang Masih Terbuka

Butir berikut **belum diputuskan** dan tidak menghalangi PRD ini dibaca, tetapi harus ditetapkan sebelum/selama pembangunan fase terkait. Usulan asisten dicantumkan sebagai bahan, bukan keputusan.

| # | Hal terbuka | Usulan asisten | Perlu ditetapkan sebelum |
|---|---|---|---|
| T-01 | Framework/library Go dan cara membangun antarmuka web (server-side vs SPA tertanam) | Kerangka ringan + aset tertanam, tanpa dependensi Node saat build panel | Pembangunan Fase 1 dimulai |
| T-02 | Mekanisme multi-versi runtime (ala Software Store aaPanel vs versi sistem) | Panel membaca versi yang terpasang di server (nvm/pyenv/php-fpm multi-versi dikelola di luar panel di Fase 1) | FR-RUN-02 dibangun |
| T-03 | Rentang kolam port bawaan | 3100–3999 | — (sudah berupa usulan di FR-PORT-01, dapat diubah di Pengaturan) |
| T-04 | Batas simpan rilis per aplikasi | 5 rilis | — (dapat diubah di Pengaturan) |
| T-05 | Saluran notifikasi keluar persisnya | Telegram (bot resmi, paling sederhana & andal) sebagai saluran pertama; WhatsApp menyusul bila gateway yang dipakai jelas | FR-NOTIF-02 dibangun |
| T-06 | Ambang alert bawaan (disk/RAM/aplikasi/Redis) | Disk ≥ 90%, RAM ≥ 90% berkelanjutan, aplikasi berhenti, backup gagal | — (dapat diubah di Pengaturan) |
| T-07 | Apakah MongoDB ikut didukung editor/provisioning | Tidak di Fase 1 (tidak ada dalam kebutuhan asli); nilai kembali di Fase 2 | Fase 2 |
| T-08 | Spesifikasi home server (CPU/RAM/disk) dan jumlah aplikasi awal | Mengikuti kesiapan home server rencana Flow API; PRD mengasumsikan build satu-per-satu cukup untuk skala itu | Pemasangan pertama |
| T-09 | Port bawaan panel | 8282 (di luar kolam port aplikasi) | Pemasangan pertama |
| T-10 | 2FA untuk login panel | Cloudflare Access berperan sebagai lapisan identitas kedua di Fase 1; 2FA bawaan panel dinilai kembali di Fase 2 | Fase 2 |
| T-11 | Pustaka/SDK MCP untuk Go | SDK MCP resmi untuk Go bila sudah stabil saat dibangun, atau implementasi JSON-RPC minimal yang lulus uji kesesuaian MCP; keputusan dicatat saat pembangunan | FR-MCP dibangun |

---

## 15. Risiko dan Mitigasi

| Risiko | Dampak | Mitigasi yang dibangun ke produk |
|---|---|---|
| Panel bobol = server bobol (panel setara akses root) | Sangat tinggi | Akses berlapis (tunnel + Access + login), pembatasan percobaan login, audit log, tidak ada port panel ke publik, terminal web hanya dalam sesi sah & tercatat (Bab 3.3, FR-AUTH, FR-TERM). |
| Deploy menjalankan kode bebas dari repo (build script) | Tinggi bila kelak multi-user | Diterima sadar untuk server pribadi 1 admin; isolasi antar-pengguna adalah prasyarat Fase 3 sebelum multi-user dibuka. |
| Build besar (Go/Node) mengganggu aplikasi berjalan di server kecil | Sedang | Antrean build satu-per-satu (Bab 6 aturan 3), monitor & alert (FR-DASH). |
| Edit data produksi lewat editor visual | Sedang–tinggi | Konfirmasi ubah/hapus, audit log perubahan, backup & restore per database, auto backup harian (FR-DB-06, FR-BAK). |
| Home server mati (listrik/internet) | Sedang (diterima di Keputusan #4) | Auto backup di Drive menjaga data; restore dapat dilakukan ke server pengganti dari panel. |
| Token PAT dicuri/disalahgunakan | Tinggi | Token terenkripsi & tidak pernah tampil utuh; anjuran fine-grained PAT terbatas repo & izin; peringatan kedaluwarsa; ganti token mudah (FR-GH). |
| Kunci enkripsi panel hilang | Tinggi (kredensial tak terbaca) | Kunci termasuk yang di-backup bersama data panel dalam keadaan terenkripsi; keadaan ini dijelaskan saat pemasangan (FR-SET-03). |
| Cakupan Fase 1 sangat besar (Keputusan #9, #12, #13) | Jadwal pembangunan panjang | Kriteria penerimaan per modul (Bab 12.1) memungkinkan pembangunan & pengujian bertahap per modul di dalam Fase 1 tanpa mengubah keputusan cakupan. |

---

---

## 17. Desain Antarmuka

### 17.1 Sumber dan Metode

Bab ini disusun dengan bantuan skill **UI/UX Pro Max** (uupm.cc / NextLevelBuilder, lisensi MIT) yang dipasang atas permintaan Wisnu pada 20 November 2026. Alur yang dijalankan sesuai kontrak skill tersebut: pencarian sistem desain untuk produk *"devops server management admin panel dashboard dark"* (dial: kepadatan 9/10 — padat khas dasbor, variasi 4/10 — seimbang/modern, gerak 3/10 — halus), dilengkapi pencarian domain `chart` (grafik deret waktu pemantauan real-time), `ux` (tabel data padat), dan `color` (warna status).

Hasil mentah mesin pencari kemudian **diverifikasi kesesuaiannya** dengan produk ini sebelum dipakai — sesuai aturan skill itu sendiri bahwa hasil adalah rekomendasi, bukan perintah:

- **Diadopsi utuh:** palet warna gelap slate + hijau status, pasangan huruf Fira Code / Fira Sans, pola dasbor pemantauan real-time, dan aturan grafiknya.
- **Disesuaikan:** gaya *Glassmorphism* yang direkomendasikan mesin **tidak** dipakai sebagai gaya utama — panel admin padat data membutuhkan permukaan solid yang terbaca cepat dan ringan di peramban; efek kaca (blur + tepi terang) hanya dipakai terbatas pada bilah atas, menu melayang, dan dialog modal. Gaya utama yang dipakai adalah **dasbor gelap padat data** (*dark data-dense dashboard*, sejalan dengan rekomendasi sekunder hasil pencarian produk: Dark Mode + Data-Dense Dashboard + Minimalism).

Mockup di Bab 17.8 adalah **contoh desain** (data contoh), bukan tangkapan layar aplikasi jadi — aplikasi belum dibangun. Huruf pada mockup memakai huruf pengganti yang tersedia di mesin pembuat (Liberation Sans / DejaVu Sans Mono); huruf produk yang sebenarnya adalah Fira Sans / Fira Code (Bab 17.4).

### 17.2 Gaya dan Prinsip Visual

1. **Mode gelap sebagai satu-satunya mode Fase 1.** Panel dipakai untuk pemantauan — gelap mengurangi silau dan menonjolkan warna status. Mode terang bukan cakupan Fase 1.
2. **Status selalu berwarna + berlabel.** Hijau/kuning/merah tidak pernah berdiri sendiri: setiap status warna disertai teks (Berjalan/Berhenti/Gagal) dan/atau ikon, agar tidak bergantung pada persepsi warna saja.
3. **Padat tapi terpilah.** Kepadatan tinggi khas dasbor (jarak 8–32 px), kartu per kelompok informasi, satu layar satu pekerjaan utama.
4. **Satu aksen tindakan.** Hijau aksen hanya untuk tindakan utama dan status sehat; tindakan berbahaya (hapus, flush, restore) selalu berwarna netral/merah dan berkonfirmasi — tidak pernah sewarna tombol utama.
5. **Gerak halus dan bermakna.** Transisi 150–300 ms untuk umpan balik arah/keadaan; tidak ada animasi dekoratif pada data yang berubah real-time; `prefers-reduced-motion` dihormati.

### 17.3 Token Warna

| Peran | Hex | Pemakaian |
|---|---|---|
| Background | `#0F172A` | Latar halaman |
| Card | `#1B2336` | Permukaan kartu/panel |
| Muted (permukaan) | `#272F42` | Kepala tabel, tombol sekunder, lacak saklar |
| Primary | `#1E293B` | Permukaan utama gelap, bilah sisi |
| Secondary | `#334155` | Elemen sekunder |
| Border | `#475569` | Garis tepi kartu & pemisah |
| Foreground | `#F8FAFC` | Teks utama |
| Muted Foreground | `#94A3B8` | Teks sekunder, label, keterangan |
| Accent / CTA | `#22C55E` | Tombol utama, status sehat/berjalan/berhasil |
| On Accent | `#0F172A` | Teks di atas aksen |
| Warning | `#F59E0B` | Peringatan, ambang mendekati, status dilewati |
| Destructive | `#EF4444` | Kesalahan, berhenti, gagal, tindakan berbahaya |
| Info / seri data kedua | `#38BDF8` | Garis data kedua pada grafik (mis. RAM), tautan |

Kontras teks utama terhadap latar memenuhi ≥ 4,5:1; teks sekunder `#94A3B8` di atas `#0F172A` dipakai hanya untuk teks ≥ 12 px dan bukan untuk isi data kritis.

### 17.4 Tipografi

| Peran | Huruf | Pemakaian |
|---|---|---|
| Heading & isi | **Fira Sans** (300–700) | Judul layar, label, isi tabel, tombol |
| Data teknis | **Fira Code** (400–700) | Port, commit, ekspresi cron, log, alamat, nilai yang harus dibaca persis |

Ukuran dasar isi 14–16 px, tinggi baris 1,5; teks isi tidak pernah lebih kecil dari 12 px. Angka pada kartu ringkasan memakai ukuran besar (24–28 px, tebal) agar terbaca sekilas.

### 17.5 Tata Letak dan Kepadatan

- **Kerangka tetap:** bilah sisi kiri 216 px (logo, menu 13 kelompok sesuai Bab 8, kartu server di bawah) + bilah atas 64 px (judul layar, lencana status server, notifikasi, akun). Kerangka ini sama di semua layar agar navigasi dapat ditebak.
- **Kisi kartu:** konten tersusun sebagai kartu berjarak 12–16 px; daftar selalu berbentuk tabel dengan kepala baris berwarna permukaan muted.
- **Responsif:** pada lebar ≤ 768 px bilah sisi menyusut menjadi ikon/lembar geser, tabel lebar berubah menjadi gulir mendatar atau tampilan kartu (mengikuti panduan UX tabel dari skill) — fungsi tidak ada yang hilang, sesuai Bab 8.
- **Keadaan kosong & memuat:** setiap daftar memiliki keadaan kosong yang menjelaskan langkah pertama (mis. "Belum ada aplikasi — Tambah Aplikasi"), dan pemuatan data real-time menampilkan penanda waktu pembaruan terakhir; data basi ditandai, tidak ditampilkan seolah baru.

### 17.6 Grafik dan Data

Mengikuti rekomendasi domain `chart` untuk pemantauan deret waktu:

| Data | Jenis grafik | Aturan |
|---|---|---|
| CPU & RAM (24 jam / 7 hari) | Grafik garis dua seri + penanda anomali | Lonjakan di luar pola ditandai lingkaran merah + label waktu; legenda selalu tampil; seri dibedakan oleh warna **dan** label |
| Disk & kuota | Donat / batang ukur | Angka persis selalu tertulis di samping grafik, tidak dibaca dari bentuk |
| Riwayat deploy & backup | Garis waktu (daftar kronologis berstatus) | Bukan grafik batang — yang dicari Admin adalah kejadian, bukan agregat |
| Pemakaian per aplikasi | Batang mendatar terurut | Diurut dari terbesar; ambang alert digambar sebagai garis batas |

Grafik real-time wajib memiliki jeda/penanda waktu pembaruan dan tidak menggeser tata letak saat data masuk (ruang dicadangkan).

### 17.7 Aturan UX yang Mengikat

Diambil dari kategori prioritas skill (aksesibilitas → interaksi → kinerja → konsistensi):

1. Kontras ≥ 4,5:1 untuk teks; cincin fokus keyboard selalu terlihat — tidak pernah dihilangkan.
2. Semua elemen yang dapat diklik menunjukkan keadaan hover dan kursor penunjuk; target sentuh di ponsel ≥ 44×44 px.
3. Ikon memakai SVG (keluarga Lucide/Heroicons), **bukan emoji**; tombol ikon selalu memiliki label aksesibel.
4. Kesalahan formulir ditampilkan di samping kolomnya dengan penjelasan yang dapat ditindaklanjuti (mengikuti FR-GH-05, FR-PORT-04) — bukan hanya pesan umum di atas halaman.
5. Tindakan berkonfirmasi (Bab 6) menampilkan ringkasan akibatnya di dialog, dengan tombol berbahaya berwarna destructive dan terpisah jauh dari tombol utama.
6. Kredensial selalu tampil tersamar (pola titik + 4 karakter terakhir bila relevan) dan tidak ada tombol "tampilkan" untuk rahasia yang sudah disimpan (Bab 6 aturan 5).
7. Log dan terminal memakai huruf mono, dapat digulir balik, dan memiliki penanda baris terakhir agar Admin tahu data masih mengalir.

### 17.8 Mockup Layar Utama

Enam layar yang paling menentukan pengalaman panel. Layar lain mengikuti pola yang sama (daftar berbentuk tabel berkartu, detail berbentuk tab, formulir satu kolom berlabel).

#### UI-01 — Login (L-01, FR-AUTH)

Kartu masuk terpusat di atas latar gelap: logo + nama produk, domain, kolom username & password berlabel, tombol Masuk seaksen penuh, dan lencana keterangan bahwa Cloudflare Access sudah memeriksa identitas sebelum halaman ini. Catatan kaki mengingatkan bahwa percobaan masuk tercatat — disengaja terlihat, sebagai penangkal.

[[GAMBAR: ui/ui-01-login.png]]

#### UI-02 — Dashboard (L-02, FR-DASH)

Empat kartu ringkasan (CPU, RAM, Disk, Aplikasi) dengan angka besar dan sub-label berwarna status; grafik garis CPU & RAM 24 jam dengan penanda anomali berlabel waktu; panel Alert Aktif di kanan; tabel Status Aplikasi di bawah dengan titik + label status dan waktu deploy terakhir. Semua yang dibutuhkan untuk menjawab "apakah serverku sehat?" ada dalam satu layar tanpa klik.

[[GAMBAR: ui/ui-02-dashboard.png]]

#### UI-03 — Aplikasi & Log Deploy (L-03/L-06, FR-APP, FR-RUN)

Daftar aplikasi berbentuk tabel: runtime + driver, port (mono), status berlabel, commit aktif (mono, terpotong 7 karakter), dan aksi Deploy/Log per baris. Panel bawah menampilkan log deploy live huruf mono dengan baris keberhasilan berwarna aksen dan catatan ketersediaan rollback — deploy yang sedang berjalan tidak pernah menjadi kotak hitam.

[[GAMBAR: ui/ui-03-aplikasi-deploy.png]]

#### UI-04 — Editor Database (L-08, FR-DB)

Tiga wilayah: pohon skema multi-engine di kiri (PostgreSQL, MySQL, SQLite dalam satu pohon), kotak kueri SQL di kanan atas dengan tombol Jalankan dan keterangan batas waktu, dan grid data di bawahnya. Baris yang sedang diubah disorot kuning redup; tombol Tambah Baris/Impor/Ekspor berada di bawah grid dengan pengingat bahwa perubahan berkonfirmasi dan tercatat di audit log.

[[GAMBAR: ui/ui-04-editor-database.png]]

#### UI-05 — Cron (L-11, FR-CRON)

Tabel pekerjaan campuran (aplikasi, sistem, bawaan) dengan jadwal dalam bahasa manusia, jalan berikutnya (mono), hasil terakhir berlabel warna, dan saklar aktif. Panel bawah menunjukkan pratinjau pembuat jadwal: ekspresi, artinya dalam bahasa manusia, dan 3 waktu jalan berikutnya — ekspresi tidak sah tidak akan pernah sampai tersimpan (FR-CRON-03).

[[GAMBAR: ui/ui-05-cron.png]]

#### UI-06 — Backup & Google Drive (L-14, FR-BAK)

Kartu koneksi Drive menampilkan status terhubung, akun tersamar, cakupan scope `drive.file`, dan folder tujuan; kartu jadwal & retensi menampilkan jadwal otomatis dan tombol Backup Sekarang. Tabel riwayat memperlihatkan pemicu, isi ringkas, ukuran, status, dan tombol Restore per paket berhasil — termasuk satu baris gagal (Drive tidak terjangkau) yang ditampilkan apa adanya, karena riwayat yang jujur adalah bagian dari desain.

[[GAMBAR: ui/ui-06-backup.png]]

---

---

## 18. Antarmuka MCP (MCP API)

### 18.1 Tujuan dan Posisi

Keputusan #14: SatriaPanel menyediakan **MCP API** agar panel dapat dioperasikan oleh klien MCP — asisten AI dan perkakas agent lain — lewat tool yang terdefinisi, bukan lewat otomatisasi layar. MCP adalah antarmuka *ketiga* panel setelah antarmuka web (Bab 8) dan tidak menggantikannya: ketiganya memakai mesin yang sama (scheduler, deploy, editor, backup) sehingga aturan bisnis Bab 6 berlaku identik.

Cakupan yang dipilih Wisnu adalah **penuh — termasuk tool destruktif** — dengan pengaman berupa scope token (18.3), konfirmasi eksplisit per panggilan destruktif (18.6), dan audit menyeluruh (18.7). Bab ini adalah kontraknya.

### 18.2 Transport dan Protokol

| Aspek | Ketentuan |
|---|---|
| Transport | MCP **Streamable HTTP** (sesuai spesifikasi MCP yang berlaku saat dibangun) |
| Endpoint | `https://panel.satriyadi.com/mcp` — domain & tunnel yang sama dengan panel; tidak ada port tambahan |
| Protokol | JSON-RPC 2.0 sesuai spesifikasi MCP: `initialize`, `tools/list`, `tools/call`, `ping`; negosiasi versi protokol saat `initialize` |
| Format hasil | Hasil tool berupa teks terstruktur/JSON; kesalahan berupa kesalahan tool MCP dengan kode & pesan yang dapat ditindaklanjuti (mengikuti gaya FR-GH-05) |
| Penemuan | `tools/list` hanya mengembalikan tool yang tercakup scope token pemanggil — tool di luar scope tidak diiklankan |

### 18.3 Otentikasi dan Scope Token

- Token dibuat Admin dari Pengaturan (FR-MCP-02): format token acak kuat, ditampilkan **satu kali**, disimpan sebagai hash. Token yang hilang tidak dapat dilihat lagi — buat baru, cabut lama.
- Setiap token membawa himpunan scope berikut; token baru secara bawaan hanya mendapat `read`.

| Scope | Isi kuasa |
|---|---|
| `read` | Semua pembacaan: status server & aplikasi, daftar, log, riwayat, isi berkas (baca), skema & query SELECT |
| `deploy` | Deploy, rollback, start/stop/restart aplikasi & container, suntik env non-rahasia |
| `cron` | Buat/ubah/aktifkan/jalankan pekerjaan cron |
| `backup` | Memicu backup, membaca status & daftar paket (bukan restore) |
| `data-write` | Query penulisan (INSERT/UPDATE/DELETE) pada database lewat tool query |
| `admin` | Seluruh tool destruktif: hapus aplikasi/database, flush Redis per aplikasi, restore backup, DDL, tulis/hapus berkas, perintah shell, terapkan ingress |

- Scope `admin` tidak mencakup membaca kredensial (tetap dilarang untuk semua scope, FR-MCP-09) dan tidak dapat dipakai untuk mengelola token MCP lain.
- Mencabut token berlaku seketika; menonaktifkan endpoint MCP (FR-MCP-10) menolak semua token tanpa menghapusnya.

### 18.4 Katalog Tool

Nama tool mengikuti pola `<objek>_<aksi>`. Kolom Scope = scope minimum.

**Server & Tunnel**

| Tool | Scope | Keterangan |
|---|---|---|
| `server_status` | read | CPU/RAM/disk/uptime + ringkasan alert (setara FR-DASH) |
| `tunnel_status` | read | Status cloudflared & pemetaan port → hostname (FR-PORT-02/08) |
| `tunnel_apply_ingress` | admin | Menulis ulang ingress dari keadaan panel & me-reload cloudflared (FR-PORT-04) |

**Aplikasi & Deploy**

| Tool | Scope | Keterangan |
|---|---|---|
| `apps_list` | read | Daftar aplikasi + status + rilis aktif |
| `app_get` | read | Detail satu aplikasi (tanpa isi env rahasia) |
| `app_logs` | read | Log proses aplikasi, dapat dibatasi jumlah baris & dicari |
| `app_deploy` | deploy | Memulai deploy; mengembalikan ID deploy (asinkron, FR-MCP-06) |
| `deploy_status` | read | Status & log deploy menurut ID |
| `app_releases` | read | Riwayat rilis aplikasi |
| `app_rollback` | deploy | Rollback ke rilis yang disebutkan |
| `app_start` / `app_stop` / `app_restart` | deploy | Kendali proses (FR-RUN-03) |
| `app_create` | admin | Membuat aplikasi baru (sumber Git/GitHub, runtime, port, hostname) |
| `app_delete` | admin | Menghapus aplikasi — destruktif, wajib konfirmasi (18.6) |
| `app_env_set` | admin | Mengubah env aplikasi; nilai rahasia tidak pernah dikembalikan |

**Database & Redis**

| Tool | Scope | Keterangan |
|---|---|---|
| `db_list` | read | Daftar database/koneksi |
| `db_schema` | read | Pohon skema satu database (FR-DB-04) |
| `db_query` | read (SELECT) / data-write (tulis) / admin (DDL) | Menjalankan SQL; jenis perintah menentukan scope yang dituntut; batas waktu sama dengan editor |
| `db_delete` | admin | Menghapus database — destruktif, wajib konfirmasi + nama database diulang |
| `redis_allocations` | read | Alokasi Redis per aplikasi (tanpa password) |
| `redis_keys` | read | Jelajah key dalam prefix satu aplikasi (FR-REDIS-04) |
| `redis_flush_app` | admin | Flush prefix satu aplikasi — destruktif, wajib konfirmasi |

**Docker, Cron, Berkas, Terminal**

| Tool | Scope | Keterangan |
|---|---|---|
| `docker_list` | read | Container/image/volume (FR-DOCKER-01) |
| `docker_action` | deploy (start/stop/restart) / admin (hapus, prune) | Aksi container |
| `cron_list` | read | Semua pekerjaan + status terakhir |
| `cron_upsert` | cron | Buat/ubah pekerjaan (jadwal divalidasi seperti FR-CRON-03) |
| `cron_run_now` | cron | Jalankan pekerjaan sekarang; mengembalikan ID eksekusi |
| `cron_runs` | read | Riwayat eksekusi + output |
| `cron_delete` | cron | Hapus pekerjaan (pekerjaan bawaan tetap tidak dapat dihapus, Bab 6) |
| `file_list` / `file_read` | read | Jelajah & baca berkas (folder data panel tetap dikecualikan, FR-FILE-04) |
| `file_write` / `file_delete` | admin | Tulis/hapus berkas — destruktif, wajib konfirmasi |
| `shell_run` | admin | Menjalankan perintah shell di server; perintah & hasil tercatat penuh di audit; ada batas waktu & batas ukuran keluaran |

**Backup & Audit**

| Tool | Scope | Keterangan |
|---|---|---|
| `backup_list` | read | Riwayat backup + paket di Drive |
| `backup_start` | backup | Memicu backup sekarang; mengembalikan ID backup |
| `backup_status` | read | Status backup menurut ID |
| `backup_restore` | admin | Restore item dari paket — destruktif, wajib konfirmasi; tetap didahului backup pengaman (Bab 6 aturan 8) |
| `audit_list` | read | Membaca audit log dengan saringan (FR-AUDIT-03) |

### 18.5 Pola Operasi Lama

Deploy, backup, dan restore tidak pernah menahan panggilan MCP sampai selesai. Tool pemulainya mengembalikan ID dan keadaan awal; klien memantau lewat `deploy_status`/`backup_status`. Bila koneksi klien terputus, operasi tetap berjalan di panel — sama seperti deploy dari antarmuka web.

### 18.6 Aturan Keamanan Tool Destruktif

1. Tool berscope `admin` yang bersifat destruktif (bertanda "wajib konfirmasi" di 18.4) menuntut dua parameter: `confirm: true` dan pengenal target yang diulang persis (slug aplikasi, nama database, atau ID paket). Ketidakcocokan pengenal = penolakan.
2. Penolakan karena konfirmasi/scope tercatat di audit sebagai percobaan yang ditolak.
3. `shell_run` tidak menerima perintah yang ditujukan untuk membaca berkas kredensial panel atau kunci enkripsi (daftar larangan di sisi server); hasilnya dipotong pada batas ukuran dan sisanya ditandai terpotong.
4. Tidak ada tool yang menghapus atau mengubah audit log.

### 18.7 Audit dan Batas Laju

- Setiap `tools/call` menghasilkan entri audit: waktu, nama token, tool, target, ringkasan parameter (tanpa nilai rahasia), hasil/penolakan (FR-MCP-07).
- Batas laju per token (bawaan usulan: 60 panggilan/menit, dapat diatur) melindungi server dari klien yang berputar; pelampauan mengembalikan kesalahan batas laju yang jelas.
- Pemakaian terakhir token diperbarui pada setiap panggilan dan tampil di Pengaturan, agar token yatim mudah dikenali dan dicabut.

### 18.8 Menghubungkan Klien MCP

Klien MCP apa pun yang mendukung Streamable HTTP + header Authorization dapat terhubung. Contoh konfigurasi generik:

```json
{
  "mcpServers": {
    "satriapanel": {
      "url": "https://panel.satriyadi.com/mcp",
      "headers": { "Authorization": "Bearer <TOKEN-MCP>" }
    }
  }
}
```

Token diisi dari token yang dibuat di Pengaturan panel (18.3) dan diperlakukan sebagai rahasia penuh: tidak dibagikan, tidak dimasukkan ke repo, dan dicabut dari panel bila diduga bocor.

### 18.9 Batasan Bab Ini

- Versi protokol MCP persis dan pustaka Go yang dipakai adalah hal terbuka T-11; kontrak tool di bab ini tidak bergantung pada pilihan itu.
- Otentikasi OAuth 2.1 (alur otorisasi MCP penuh) tidak dipakai di Fase 1 — token Bearer khusus dipilih karena panel ini satu Admin; OAuth dinilai kembali bila kelak ada klien pihak ketiga.

---

## 19. Riwayat Versi

| Versi | Tanggal | Isi |
|---|---|---|
| Draft v0.1 | 20 November 2026 | PRD pertama, disusun utuh dari Konsep Awal SatriaPanel Draft v0.16 (final, 13 Keputusan Terkunci). 16 bab: kebutuhan fungsional 16 modul (FR-AUTH s.d. FR-AUDIT), aturan bisnis, model data panel (15 tabel), daftar 18 layar, integrasi GitHub/Cloudflare/Google Drive, kebutuhan non-fungsional, fase + kriteria penerimaan, tabel keputusan terkunci, 10 hal terbuka (T-01 s.d. T-10), dan risiko. Pembangunan belum diotorisasi. |
| Draft v0.2 | 20 November 2026 | Menambahkan **Bab 17 — Desain Antarmuka** atas permintaan Wisnu, disusun dengan skill UI/UX Pro Max (dipasang pada hari yang sama): sistem desain (gaya dasbor gelap padat data dengan efek kaca terbatas, token warna 13 peran, tipografi Fira Sans/Fira Code, aturan tata letak, grafik, dan 7 aturan UX yang mengikat) + **6 mockup tertanam** (UI-01 Login, UI-02 Dashboard, UI-03 Aplikasi & Log Deploy, UI-04 Editor Database, UI-05 Cron, UI-06 Backup) di folder `ui/`. Bab Riwayat Versi berpindah nomor menjadi Bab 18. Tidak ada keputusan atau kebutuhan yang berubah dari v0.1. Pembangunan belum diotorisasi. |
| Draft v0.3 | 20 November 2026 | Menambahkan **Antarmuka MCP (Keputusan #14, dipilih Wisnu: cakupan penuh termasuk destruktif, dijaga scope token khusus)**: modul FR-MCP (10 kebutuhan) di Bab 5, aturan bisnis 13–14, kriteria penerimaan Fase 1 butir 14–15, hal terbuka T-11, dan **Bab 18 — Antarmuka MCP** (transport Streamable HTTP di `/mcp`, 6 scope token, katalog 40 tool, pola asinkron, aturan konfirmasi destruktif, audit & batas laju, contoh konfigurasi klien). Bab Riwayat Versi berpindah nomor menjadi Bab 19. Keputusan #1–#13 dan isi v0.2 tidak berubah. |

---

*Akhir PRD SatriaPanel Draft v0.3 — menunggu tinjauan Wisnu. Koreksi cukup dengan menyebut nomor bab, nomor FR, nomor UI, atau nomor keputusan.*
