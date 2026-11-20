package panel

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// seedDemo mengisi data contoh hanya saat flag -demo dipakai. Tidak pernah
// dijalankan pada instalasi produksi tanpa flag tersebut.
func (p *Panel) seedDemo() error {
	if p.setting("demo_seeded", "") == "true" {
		return nil
	}
	var appCount int
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM apps`).Scan(&appCount)
	if appCount > 0 {
		_ = p.setSetting("demo_seeded", "true")
		return nil
	}
	now := time.Now().UTC()
	type demo struct {
		name, slug, source, repo, branch, runtime, start, status, health string
		port                                                             int
		hostname                                                         string
		releaseDir                                                       string
		startable                                                        bool
	}
	demos := []demo{
		{"Toko Satria", "toko-satria", "github", "https://github.com/satriyadi/toko-satria", "main", "node", "node server.js", "berjalan", "sehat", 3100, "toko.satriyadi.com", "", true},
		{"API Inventaris", "api-inventaris", "github", "https://github.com/satriyadi/api-inventaris", "main", "go", "./app", "berhenti", "gagal", 3101, "api.satriyadi.com", "", false},
		{"Landing Page", "landing-page", "git_https", "https://github.com/satriyadi/landing.git", "main", "static", "node server.js", "berjalan", "sehat", 3102, "satriyadi.com", "", true},
	}
	appIDs := make([]int64, len(demos))
	for i := range demos {
		d := &demos[i]
		dir := p.appWorkdir(d.slug)
		relDir := filepath.Join(dir, "releases", fmt.Sprintf("%d-demo", now.Unix()-int64((len(demos)-i)*3600)))
		_ = os.MkdirAll(filepath.Join(dir, "persistent", "uploads"), 0o750)
		_ = os.MkdirAll(relDir, 0o750)
		html := fmt.Sprintf("<!doctype html><html lang=\"id\"><meta charset=\"utf-8\"><title>%s</title><body style=\"font-family:sans-serif;background:#0f172a;color:#f8fafc;padding:3rem\"><h1>%s</h1><p>Aplikasi contoh SatriaPanel berjalan di port $PORT.</p></body></html>", d.name, d.name)
		_ = os.WriteFile(filepath.Join(relDir, "index.html"), []byte(html), 0o640)
		server := "const http=require('http');const port=process.env.PORT||3000;http.createServer((req,res)=>{res.writeHead(200,{'Content-Type':'text/html'});res.end(" + fmt.Sprintf("%q", html) + ");}).listen(port,'127.0.0.1');"
		_ = os.WriteFile(filepath.Join(relDir, "server.js"), []byte(server), 0o640)
		_ = os.WriteFile(filepath.Join(relDir, ".env"), []byte("APP_ENV=production\nPORT="+fmt.Sprint(d.port)+"\n"), 0o600)
		d.releaseDir = relDir
	}

	for i, d := range demos {
		install, build, start := runtimeDefaults(d.runtime)
		if d.runtime == "static" {
			install, build = "", ""
		}
		if d.startable {
			start = d.start
		}
		res, err := p.db.Exec(`INSERT INTO apps(name,slug,source_type,repo_url,branch,runtime,mode,driver,install_cmd,build_cmd,start_cmd,port,hostname,auto_deploy,workdir,persistent_dirs,status,health_status,health_path,last_deploy_at,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			d.name, d.slug, d.source, d.repo, d.branch, d.runtime, "native", "direct", install, build, start, d.port, d.hostname, i == 0, p.appWorkdir(d.slug), `["uploads"]`, d.status, d.health, "/", now.Add(-time.Duration(i+1)*24*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			return err
		}
		appID, _ := res.LastInsertId()
		appIDs[i] = appID
		relRes, _ := p.db.Exec(`INSERT INTO releases(app_id,commit_hash,dir_path,image_tag,built_at,status) VALUES(?,?,?,?,?,?)`,
			appID, fmt.Sprintf("a1b2c3%d", i), d.releaseDir, "", now.Add(-time.Duration(i+1)*time.Hour).Format(time.RFC3339), "aktif")
		relID, _ := relRes.LastInsertId()
		_, _ = p.db.Exec(`UPDATE apps SET active_release_id=? WHERE id=?`, relID, appID)
		state := "berhasil"
		errText := ""
		if i == 1 {
			state = "gagal"
			errText = "Health check http://127.0.0.1:3101/ gagal setelah 17 detik; rilis aktif dikembalikan."
		}
		logPath := filepath.Join(p.logsDir(), "deploy", fmt.Sprintf("%d-deploy-demo.log", appID))
		_, _ = p.db.Exec(`INSERT INTO deployments(app_id,release_id,trigger,status,commit_hash,log_path,error,started_at,finished_at,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, appID, relID, "manual", state, fmt.Sprintf("a1b2c3%d", i),
			logPath, errText,
			now.Add(-time.Duration(i+1)*time.Hour).Format(time.RFC3339), now.Add(-time.Duration(i+1)*time.Hour+45*time.Second).Format(time.RFC3339), now.Add(-time.Duration(i+1)*time.Hour).Format(time.RFC3339))
		_, _ = p.db.Exec(`INSERT INTO app_env(app_id,key,value,is_secret) VALUES(?,?,?,?)`, appID, "APP_NAME", p.encryptMust(d.slug), 0)
		_, _ = p.db.Exec(`INSERT INTO app_env(app_id,key,value,is_secret) VALUES(?,?,?,?)`, appID, "API_KEY", p.encryptMust(randomToken(18)), 1)
		_, _ = p.db.Exec(`INSERT INTO app_env(app_id,key,value,is_secret) VALUES(?,?,?,?)`, appID, "DATABASE_URL", p.encryptMust("postgres://demo:rahasia@127.0.0.1:5432/demo"), 1)
		_ = os.MkdirAll(filepath.Dir(logPath), 0o750)
		_ = os.WriteFile(logPath, []byte(fmt.Sprintf("Deploy demo untuk %s\nCommit: a1b2c3%d\nHealth check %s\n", d.name, i, state)), 0o640)
	}

	nowStr := now.Format(time.RFC3339)
	// Cron aplikasi contoh
	var cronIDs []int64
	for _, c := range []struct {
		name, command, app, schedule string
		enabled                      int
		timeout                      int
	}{
		{"Bersihkan cache toko", "npm run cache:clear", fmt.Sprint(appIDs[0]), "0 3 * * *", 1, 300},
		{"Cek stok harian", "echo 'stok ok'", fmt.Sprint(appIDs[1]), "0 6 * * *", 0, 120},
	} {
		res, _ := p.db.Exec(`INSERT INTO cron_jobs(name,kind,app_id,command,workdir,schedule_expr,enabled,timeout_seconds,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			c.name, "app", c.app, c.command, "", c.schedule, c.enabled, c.timeout, nowStr, nowStr)
		id, _ := res.LastInsertId()
		cronIDs = append(cronIDs, id)
	}
	if len(cronIDs) > 0 {
		for i := 0; i < 3; i++ {
			status := "berhasil"
			if i == 1 {
				status = "gagal"
			}
			started := now.Add(-time.Duration(i+2) * time.Hour)
			_, _ = p.db.Exec(`INSERT INTO cron_runs(job_id,started_at,finished_at,status,exit_code,output) VALUES(?,?,?,?,?,?)`,
				cronIDs[0], started.Format(time.RFC3339), started.Add(4*time.Second).Format(time.RFC3339), status, map[string]int{"berhasil": 0, "gagal": 1}[status],
				map[string]string{"berhasil": "Cache dibersihkan: 128 entri.\n", "gagal": "Error: perintah keluar dengan kode 1\n"}[status])
		}
	}

	notes := []struct {
		kind, title, body string
		app               int64
	}{
		{"deploy", "Deploy berhasil: Toko Satria", "Rilis #1 aktif dan health check lulus.", appIDs[0]},
		{"alert", "Penggunaan disk mencapai 91%", "Pertimbangkan membersihkan image Docker lama.", 0},
		{"cron", "Cron gagal: Cek stok harian", "Perintah keluar dengan kode 1.", appIDs[1]},
		{"backup", "Backup belum dikonfigurasi", "Aktifkan backup terjadwal di menu Backup.", 0},
	}
	for i, n := range notes {
		_, _ = p.db.Exec(`INSERT INTO notifications(kind,title,body,app_id,is_read,created_at) VALUES(?,?,?,?,?,?)`, n.kind, n.title, n.body, nullableID(n.app), i > 1, now.Add(-time.Duration(i+1)*30*time.Minute).Format(time.RFC3339))
	}
	for _, a := range []string{"auth.login", "app.create", "deploy.success", "settings.update"} {
		_, _ = p.db.Exec(`INSERT INTO audit_logs(actor,action,object_type,object_id,summary,ip,created_at) VALUES('admin',?,'demo','','Data contoh SatriaPanel','127.0.0.1',?)`, a, now.Format(time.RFC3339))
	}
	for i := 0; i < 60; i++ {
		at := now.Add(-time.Duration(60-i) * time.Minute)
		cpu := 12.0 + float64((i*7)%18)
		mem := 3800000000.0 + float64((i*3)%12)*100000000
		_, _ = p.db.Exec(`INSERT INTO metric_samples(scope,cpu_pct,mem_bytes,disk_pct,sampled_at) VALUES('server',?,?,?,?)`,
			cpu, int64(mem), 71.0+float64(i%3), at.Format(time.RFC3339))
	}

	// Database SQLite contoh + riwayat query
	_ = os.MkdirAll(p.dbPathDir(), 0o750)
	demoDBPath := filepath.Join(p.dbPathDir(), "demo-toko.db")
	_ = os.Remove(demoDBPath)
	if d, err := sql.Open("sqlite", demoDBPath); err == nil {
		_, _ = d.Exec(`CREATE TABLE pelanggan (id INTEGER PRIMARY KEY AUTOINCREMENT, nama TEXT NOT NULL, email TEXT UNIQUE, kota TEXT, aktif INTEGER DEFAULT 1, dibuat_at TEXT);`)
		_, _ = d.Exec(`CREATE TABLE pesanan (id INTEGER PRIMARY KEY AUTOINCREMENT, pelanggan_id INTEGER NOT NULL REFERENCES pelanggan(id), total INTEGER, status TEXT, dibuat_at TEXT);`)
		for i := 1; i <= 12; i++ {
			_, _ = d.Exec(`INSERT INTO pelanggan(nama,email,kota,aktif,dibuat_at) VALUES(?,?,?,?,?)`, fmt.Sprintf("Pelanggan %02d", i), fmt.Sprintf("pelanggan%02d@example.com", i), []string{"Jakarta", "Bandung", "Surabaya", "Yogyakarta"}[i%4], map[bool]int{true: 1, false: 0}[i%5 != 0], now.AddDate(0, 0, -i).Format("2006-01-02"))
		}
		for i := 1; i <= 25; i++ {
			_, _ = d.Exec(`INSERT INTO pesanan(pelanggan_id,total,status,dibuat_at) VALUES(?,?,?,?)`, (i%12)+1, 50000+(i*12500), []string{"baru", "diproses", "selesai", "batal"}[i%4], now.AddDate(0, 0, -i).Format("2006-01-02"))
		}
		d.Close()
	}
	res, err := p.db.Exec(`INSERT INTO databases(engine,name,host,port,username,password_enc,file_path,managed_by_panel,app_id,created_at) VALUES('sqlite','demo-toko','',0,'','',?,1,?,?)`, demoDBPath, appIDs[0], nowStr)
	if dbID, _ := res.LastInsertId(); dbID > 0 {
		for i := 0; i < 3; i++ {
			_, _ = p.db.Exec(`INSERT INTO query_history(database_id,sql_text,status,duration_ms,created_at) VALUES(?,?,?,?,?)`, dbID,
				[]string{"SELECT * FROM pelanggan LIMIT 50", "SELECT status, COUNT(*) FROM pesanan GROUP BY status", "UPDATE pelanggan SET kota='Bandung' WHERE id=2"}[i], "berhasil", 12+i*3, now.Add(-time.Duration(i)*time.Hour).Format(time.RFC3339))
		}
	}
	_ = err
	_ = p.setSetting("demo_seeded", "true")
	// jalankan dua aplikasi demo yang memang punya server Node sederhana
	if p.procMgr != nil {
		for _, idx := range []int{0, 2} {
			if app, err := p.getApp(appIDs[idx]); err == nil {
				if err := p.procMgr.startApp(app); err != nil {
					p.logf("demo %s tidak dapat dijalankan: %v", app.Slug, err)
				}
			}
		}
	}
	return nil
}

func (p *Panel) startDemoApps() {
	if !p.cfg.Demo {
		return
	}
	apps, _ := p.listApps()
	for _, a := range apps {
		if (a.Slug == "toko-satria" || a.Slug == "landing-page") && !p.healthOK(a.Port, a.HealthPath) {
			if err := p.procMgr.startApp(a); err == nil {
				_, _ = p.db.Exec(`UPDATE apps SET status='berjalan' WHERE id=?`, a.ID)
			}
		}
	}
}

func (p *Panel) encryptMust(s string) string {
	v, _ := p.encrypt(s)
	return v
}
