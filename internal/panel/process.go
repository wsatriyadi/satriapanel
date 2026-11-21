package panel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type processManager struct {
	p     *Panel
	mu    sync.Mutex
	procs map[int64]*procState
	ctx   context.Context
}

type procState struct {
	appID    int64
	pid      int
	driver   string
	logPath  string
	started  time.Time
	stopping bool
	restarts int
	cancel   context.CancelFunc
}

func newProcessManager(p *Panel) *processManager {
	return &processManager{p: p, procs: map[int64]*procState{}}
}

func (m *processManager) start(ctx context.Context) { m.ctx = ctx }

func (m *processManager) isRunning(appID int64) bool {
	m.mu.Lock()
	st := m.procs[appID]
	m.mu.Unlock()
	if st == nil {
		return false
	}
	if st.driver == "direct" {
		return processAlive(st.pid)
	}
	return true
}

func (m *processManager) startApp(app *App) error {
	if app == nil {
		return fmt.Errorf("aplikasi tidak ditemukan")
	}
	if app.Mode == "docker" || app.Mode == "compose" {
		return m.startDockerApp(app)
	}
	dir := p_activeDir(app)
	if dir == "" {
		return fmt.Errorf("belum ada rilis aktif — jalankan Deploy terlebih dahulu")
	}
	if app.StartCmd == "" {
		return fmt.Errorf("perintah start belum diisi")
	}
	driver := app.Driver
	if driver == "" || driver == "auto" {
		driver = "direct"
		if app.Runtime == "node" {
			if _, err := exec.LookPath("pm2"); err == nil && runtime.GOOS != "windows" {
				driver = "pm2"
			}
		}
	}
	switch driver {
	case "pm2":
		if err := m.startPM2(app, dir); err != nil {
			m.p.logf("pm2 gagal untuk %s, memakai driver direct: %v", app.Slug, err)
			driver = "direct"
		} else {
			return nil
		}
		fallthrough
	case "systemd":
		if driver == "systemd" {
			if _, err := exec.LookPath("systemctl"); err == nil && runtime.GOOS == "linux" {
				if err := m.startSystemd(app, dir); err != nil {
					m.p.logf("systemd gagal untuk %s, memakai driver direct: %v", app.Slug, err)
					driver = "direct"
				} else {
					return nil
				}
			} else {
				driver = "direct"
			}
		}
	case "supervisor":
		if runtime.GOOS == "linux" {
			if _, err := exec.LookPath("supervisorctl"); err == nil {
				if err := m.startSupervisor(app, dir); err != nil {
					m.p.logf("supervisor gagal untuk %s, memakai driver direct: %v", app.Slug, err)
					driver = "direct"
				} else {
					return nil
				}
			} else {
				driver = "direct"
			}
		} else {
			driver = "direct"
		}
	}
	return m.startDirect(app, dir, driver)
}

func (m *processManager) startDirect(app *App, dir, driver string) error {
	m.stopApp(app, true)
	// Pastikan port benar-benar bebas. Proses lama yang terlepas dari pohon
	// induk (mis. cmd.exe di Windows) tidak boleh membuat health check baru
	// menilai proses lama sebagai proses baru.
	for i := 0; i < 25; i++ {
		if !m.p.portOpen(app.Port) {
			break
		}
		if i == 0 {
			killPortOwner(app.Port)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if m.p.portOpen(app.Port) {
		return fmt.Errorf("port %d masih dipakai proses lain; deploy dibatalkan agar tidak menilai proses lama", app.Port)
	}
	env, err := m.p.appEnv(app, true)
	if err != nil {
		return err
	}
	if err := writeEnvFile(filepath.Join(dir, ".env"), env); err != nil {
		return fmt.Errorf("menulis .env: %w", err)
	}
	logPath := filepath.Join(m.p.logsDir(), "apps", app.Slug+".log")
	rotateLog(logPath, 12<<20)
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := shellCommand(ctx, app.StartCmd)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), envSlice(env)...)
	cmd.Stdout = lf
	cmd.Stderr = lf
	setProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		cancel()
		lf.Close()
		return err
	}
	st := &procState{appID: app.ID, pid: cmd.Process.Pid, driver: driver, logPath: logPath, started: time.Now(), cancel: cancel}
	m.mu.Lock()
	m.procs[app.ID] = st
	m.mu.Unlock()
	go m.watch(app, cmd, st, lf)
	return nil
}

func (m *processManager) watch(app *App, cmd *exec.Cmd, st *procState, lf *os.File) {
	err := cmd.Wait()
	lf.Close()
	m.mu.Lock()
	cur := m.procs[app.ID]
	stopping := cur == nil || cur != st || st.stopping
	if !stopping {
		st.restarts++
		restarts := st.restarts
		m.mu.Unlock()
		msg := "Proses berhenti tak terduga"
		if err != nil {
			msg += ": " + err.Error()
		}
		m.p.logf("%s: %s (restart %d)", app.Slug, msg, restarts)
		if restarts <= 5 {
			m.p.audit("app.crash", "app", fmt.Sprint(app.ID), fmt.Sprintf("%s berhenti tak terduga; restart %d/5", app.Name, restarts))
			go func() {
				select {
				case <-time.After(time.Duration(restarts) * 2 * time.Second):
				case <-m.ctx.Done():
					return
				}
				fresh, err := m.p.getApp(app.ID)
				if err != nil {
					return
				}
				_ = m.startDirect(fresh, p_activeDir(fresh), st.driver)
			}()
			return
		}
		_, _ = m.p.db.Exec(`UPDATE apps SET status='bermasalah', health_status='gagal', health_checked_at=? WHERE id=?`, nowUTC(), app.ID)
		m.p.notify("alert", "Aplikasi bermasalah: "+app.Name, "Proses berhenti berulang kali dan tidak dapat dipulihkan otomatis.", &app.ID)
		m.p.publish("app.status", map[string]any{"appId": app.ID, "status": "bermasalah"})
		return
	}
	m.mu.Unlock()
}

func (m *processManager) stopApp(app *App, silent bool) {
	m.mu.Lock()
	st := m.procs[app.ID]
	if st != nil {
		st.stopping = true
	}
	m.mu.Unlock()
	switch {
	case st != nil && st.driver == "pm2":
		_ = runCmd(context.Background(), "pm2 stop "+pm2Name(app.Slug), 30*time.Second)
		_, _ = runCmdOutput(context.Background(), "pm2 delete "+pm2Name(app.Slug), 30*time.Second)
	case st != nil && st.driver == "systemd":
		_ = runCmd(context.Background(), "systemctl stop "+systemdUnit(app.Slug), 30*time.Second)
	case st != nil && st.driver == "supervisor":
		_ = runCmd(context.Background(), "supervisorctl stop "+"satriapanel-"+app.Slug, 30*time.Second)
		_ = os.Remove("/etc/supervisor/conf.d/satriapanel-" + app.Slug + ".conf")
		_ = runCmd(context.Background(), "supervisorctl reread", 20*time.Second)
	case st != nil && st.pid > 0:
		killProcessTree(st.pid)
		killPortOwner(app.Port)
	}
	if st == nil && app.Mode != "docker" && app.Mode != "compose" && app.Port > 0 {
		killPortOwner(app.Port)
	}
	if st != nil && st.cancel != nil {
		st.cancel()
	}
	if app.Mode == "docker" || app.Mode == "compose" {
		m.stopDockerApp(app)
	}
	m.mu.Lock()
	delete(m.procs, app.ID)
	m.mu.Unlock()
	if !silent {
		_, _ = m.p.db.Exec(`UPDATE apps SET status='berhenti', health_status='unknown', updated_at=? WHERE id=?`, nowUTC(), app.ID)
		m.p.publish("app.status", map[string]any{"appId": app.ID, "status": "berhenti"})
	}
}

func (m *processManager) restartApp(app *App) error {
	m.stopApp(app, true)
	time.Sleep(400 * time.Millisecond)
	if err := m.startApp(app); err != nil {
		_, _ = m.p.db.Exec(`UPDATE apps SET status='bermasalah' WHERE id=?`, app.ID)
		return err
	}
	_, _ = m.p.db.Exec(`UPDATE apps SET status='berjalan', updated_at=? WHERE id=?`, nowUTC(), app.ID)
	return nil
}

func (m *processManager) shutdown() {
	m.mu.Lock()
	ids := make([]int64, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		if app, err := m.p.getApp(id); err == nil {
			m.stopApp(app, true)
		}
	}
}

// ---------- PM2 ----------

func pm2Name(slug string) string { return "sp-" + slug }

func (m *processManager) startPM2(app *App, dir string) error {
	env, err := m.p.appEnv(app, true)
	if err != nil {
		return err
	}
	if err := writeEnvFile(filepath.Join(dir, ".env"), env); err != nil {
		return err
	}
	script := filepath.Join(m.p.cfg.DataDir, "runtime", "pm2-"+app.Slug+".sh")
	body := "#!/bin/sh\nset -a\n. " + shellQuote(filepath.Join(dir, ".env")) + "\nset +a\ncd " + shellQuote(dir) + "\nexec sh -lc " + shellQuote(app.StartCmd) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		return err
	}
	_ = runCmd(context.Background(), "pm2 delete "+pm2Name(app.Slug), 20*time.Second)
	if _, err := runCmdOutput(context.Background(), "pm2 start "+shellQuote(script)+" --name "+pm2Name(app.Slug)+" --cwd "+shellQuote(dir)+" --time", 60*time.Second); err != nil {
		return err
	}
	_ = runCmd(context.Background(), "pm2 save", 20*time.Second)
	m.mu.Lock()
	m.procs[app.ID] = &procState{appID: app.ID, driver: "pm2", started: time.Now()}
	m.mu.Unlock()
	_, _ = m.p.db.Exec(`UPDATE apps SET status='berjalan', updated_at=? WHERE id=?`, nowUTC(), app.ID)
	return nil
}

func systemdUnit(slug string) string { return "satriapanel-" + slug + ".service" }

func (m *processManager) startSystemd(app *App, dir string) error {
	env, err := m.p.appEnv(app, true)
	if err != nil {
		return err
	}
	if err := writeEnvFile(filepath.Join(dir, ".env"), env); err != nil {
		return err
	}
	unitPath := "/etc/systemd/system/" + systemdUnit(app.Slug)
	user := os.Getenv("USER")
	if user == "" {
		user = "root"
	}
	body := fmt.Sprintf(`[Unit]
Description=SatriaPanel — %s
After=network.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
EnvironmentFile=%s
ExecStart=/bin/sh -lc %s
Restart=always
RestartSec=3
KillMode=mixed

[Install]
WantedBy=multi-user.target
`, app.Name, user, dir, filepath.Join(dir, ".env"), shellQuote(app.StartCmd))
	if err := os.WriteFile(unitPath, []byte(body), 0o644); err != nil {
		return err
	}
	if _, err := runCmdOutput(context.Background(), "systemctl daemon-reload && systemctl enable --now "+systemdUnit(app.Slug), 45*time.Second); err != nil {
		return err
	}
	m.mu.Lock()
	m.procs[app.ID] = &procState{appID: app.ID, driver: "systemd", started: time.Now()}
	m.mu.Unlock()
	_, _ = m.p.db.Exec(`UPDATE apps SET status='berjalan', updated_at=? WHERE id=?`, nowUTC(), app.ID)
	return nil
}

// ---------- Supervisor ----------

func supervisorProgram(slug string) string { return "satriapanel-" + slug }

func (m *processManager) startSupervisor(app *App, dir string) error {
	env, err := m.p.appEnv(app, true)
	if err != nil {
		return err
	}
	if err := writeEnvFile(filepath.Join(dir, ".env"), env); err != nil {
		return err
	}
	envParts := make([]string, 0, len(env))
	for k, v := range env {
		envParts = append(envParts, fmt.Sprintf(`%s="%s"`, k, strings.ReplaceAll(v, `"`, `\"`)))
	}
	user := os.Getenv("USER")
	if user == "" {
		user = "root"
	}
	logPath := filepath.Join(m.p.logsDir(), "apps", app.Slug+".log")
	body := fmt.Sprintf(`[program:%s]
directory=%s
command=/bin/sh -lc %s
environment=%s
user=%s
autostart=true
autorestart=true
startsecs=3
startretries=3
stopasgroup=true
killasgroup=true
stopsignal=TERM
stdout_logfile=%s
stdout_logfile_maxbytes=12MB
stdout_logfile_backups=2
redirect_stderr=true
`, supervisorProgram(app.Slug), dir, shellQuote(app.StartCmd), strings.Join(envParts, ","), user, logPath)
	path := "/etc/supervisor/conf.d/" + supervisorProgram(app.Slug) + ".conf"
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		return err
	}
	if out, err := runCmdOutput(context.Background(), "supervisorctl reread && supervisorctl update", 45*time.Second); err != nil {
		return fmt.Errorf("supervisorctl: %s", truncate(strings.TrimSpace(out), 300))
	}
	m.mu.Lock()
	m.procs[app.ID] = &procState{appID: app.ID, driver: "supervisor", started: time.Now()}
	m.mu.Unlock()
	_, _ = m.p.db.Exec(`UPDATE apps SET status='berjalan', updated_at=? WHERE id=?`, nowUTC(), app.ID)
	return nil
}

func (m *processManager) statusFromDriver(app *App) string {
	if app == nil {
		return "berhenti"
	}
	switch app.Driver {
	case "pm2":
		out, err := runCmdOutput(context.Background(), "pm2 jlist", 10*time.Second)
		if err == nil {
			var list []struct {
				Name   string `json:"name"`
				PM2Env struct {
					Status string `json:"status"`
				} `json:"pm2_env"`
			}
			if json.Unmarshal([]byte(out), &list) == nil {
				for _, it := range list {
					if it.Name == pm2Name(app.Slug) {
						if it.PM2Env.Status == "online" {
							return "berjalan"
						}
						return "berhenti"
					}
				}
			}
		}
	case "systemd":
		out, err := runCmdOutput(context.Background(), "systemctl is-active "+systemdUnit(app.Slug), 10*time.Second)
		if err == nil && strings.TrimSpace(out) == "active" {
			return "berjalan"
		}
	case "supervisor":
		out, err := runCmdOutput(context.Background(), "supervisorctl status "+supervisorProgram(app.Slug), 10*time.Second)
		if err == nil && strings.Contains(out, "RUNNING") {
			return "berjalan"
		}
	}
	return ""
}

// ---------- env & health ----------

func (p *Panel) appEnv(app *App, includeSecrets bool) (map[string]string, error) {
	env := map[string]string{}
	rows, err := p.db.Query(`SELECT key,value,is_secret FROM app_env WHERE app_id=?`, app.ID)
	if err != nil {
		return env, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		var secret int
		if rows.Scan(&k, &v, &secret) == nil {
			env[k] = p.decrypt(v)
		}
	}
	env["PORT"] = strconv.Itoa(app.Port)
	env["APP_PORT"] = strconv.Itoa(app.Port)
	env["APP_NAME"] = app.Slug
	env["APP_ENV"] = "production"

	var engine, name, host string
	var port int
	var user, passEnc, filePath string
	err = p.db.QueryRow(`SELECT engine,name,host,port,username,password_enc,file_path FROM databases WHERE app_id=? ORDER BY id LIMIT 1`, app.ID).
		Scan(&engine, &name, &host, &port, &user, &passEnc, &filePath)
	if err == nil {
		switch engine {
		case "postgresql":
			env["DATABASE_URL"] = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", user, p.decrypt(passEnc), host, port, name)
		case "mysql":
			env["DATABASE_URL"] = fmt.Sprintf("mysql://%s:%s@tcp(%s:%d)/%s", user, p.decrypt(passEnc), host, port, name)
		case "sqlite":
			env["DATABASE_URL"] = "sqlite:" + filePath
			env["DB_DATABASE"] = filePath
		}
	}
	var aclUser, aclPassEnc, prefix string
	if err := p.db.QueryRow(`SELECT acl_user,acl_password_enc,prefix FROM redis_allocations WHERE app_id=?`, app.ID).Scan(&aclUser, &aclPassEnc, &prefix); err == nil {
		host := p.redisHost()
		port := p.redisPort()
		env["REDIS_URL"] = fmt.Sprintf("redis://%s:%s@%s:%d/0", aclUser, p.decrypt(aclPassEnc), host, port)
		env["REDIS_PREFIX"] = prefix
	}
	_ = filePath
	return env, nil
}

func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func writeEnvFile(path string, env map[string]string) error {
	var b strings.Builder
	b.WriteString("# Dibuat otomatis oleh SatriaPanel — jangan disunting manual\n")
	for k, v := range env {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(envQuote(v))
		b.WriteString("\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func envQuote(v string) string {
	if v == "" {
		return `""`
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", "")
	return `"` + r.Replace(v) + `"`
}

func (p *Panel) healthOK(port int, path string) bool {
	if port <= 0 {
		return false
	}
	if path == "" {
		path = "/"
	}
	client := http.Client{Timeout: 2500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode < 500
}

func (p *Panel) healthDetail(port int, path string) (bool, int, string) {
	client := http.Client{Timeout: 2500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
	if err != nil {
		return false, 0, err.Error()
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode < 500, resp.StatusCode, resp.Status
}

func (p *Panel) portOpen(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 700*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func rotateLog(path string, max int64) {
	st, err := os.Stat(path)
	if err == nil && st.Size() > max {
		_ = os.Rename(path, path+".1")
	}
}

func tailFile(path string, maxLines int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1024<<10)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
		}
	}
	return lines
}

func p_activeDir(app *App) string {
	if app.ActiveReleaseDir != "" {
		return app.ActiveReleaseDir
	}
	return app.Workdir
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "sh", "-lc", command)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func runCmd(ctx context.Context, command string, timeout time.Duration) error {
	_, err := runCmdOutput(ctx, command, timeout)
	return err
}

func runCmdOutput(ctx context.Context, command string, timeout time.Duration) (string, error) {
	return runCmdOutputDir(ctx, "", command, timeout)
}

// runCmdOutputDir sama seperti runCmdOutput, tetapi perintah dijalankan dengan
// cmd.Dir = dir bila dir tidak kosong. Wajib untuk perintah yang perilakunya
// bergantung pada direktori kerja — misalnya `docker compose`, yang mencari
// berkas compose dari cwd; tanpa ini compose gagal "no configuration file
// provided" saat panel berjalan dari direktori lain.
func runCmdOutputDir(ctx context.Context, dir, command string, timeout time.Duration) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := shellCommand(cctx, command)
	if dir != "" {
		cmd.Dir = dir
	}
	setProcAttr(cmd)
	out, err := cmd.CombinedOutput()
	if cctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("perintah melewati batas waktu %s", timeout)
	}
	return string(out), err
}
