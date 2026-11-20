package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type App struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	Slug             string   `json:"slug"`
	SourceType       string   `json:"sourceType"`
	RepoURL          string   `json:"repoUrl"`
	Branch           string   `json:"branch"`
	Runtime          string   `json:"runtime"`
	RuntimeVersion   string   `json:"runtimeVersion"`
	Mode             string   `json:"mode"`
	Driver           string   `json:"driver"`
	InstallCmd       string   `json:"installCmd"`
	BuildCmd         string   `json:"buildCmd"`
	StartCmd         string   `json:"startCmd"`
	Port             int      `json:"port"`
	Hostname         string   `json:"hostname"`
	AutoDeploy       bool     `json:"autoDeploy"`
	WebhookID        string   `json:"webhookId"`
	WebhookSecretEnc string   `json:"-"`
	Workdir          string   `json:"workdir"`
	PersistentDirs   []string `json:"persistentDirs"`
	Status           string   `json:"status"`
	ActiveReleaseID  *int64   `json:"activeReleaseId"`
	ActiveReleaseDir string   `json:"-"`
	ActiveCommit     string   `json:"activeCommit"`
	HealthStatus     string   `json:"healthStatus"`
	HealthPath       string   `json:"healthPath"`
	HealthCheckedAt  string   `json:"healthCheckedAt"`
	LastDeployAt     string   `json:"lastDeployAt"`
	NeedsRestart     bool     `json:"needsRestart"`
	Image            string   `json:"image"`
	InternalPort     int      `json:"internalPort"`
	ComposeService   string   `json:"composeService"`
	DNSRecordID      string   `json:"dnsRecordId"`
	CreatedAt        string   `json:"createdAt"`
	UpdatedAt        string   `json:"updatedAt"`
	EnvCount         int      `json:"envCount"`
	LastDeployState  string   `json:"lastDeployState"`
	Running          bool     `json:"running"`
	RuntimeReady     bool     `json:"runtimeReady"`
}

const appSelect = `SELECT a.id,a.name,a.slug,a.source_type,a.repo_url,a.branch,a.runtime,a.runtime_version,a.mode,a.driver,
a.install_cmd,a.build_cmd,a.start_cmd,a.port,a.hostname,a.auto_deploy,a.webhook_id,a.webhook_secret,a.workdir,a.persistent_dirs,
a.status,a.active_release_id,COALESCE(r.dir_path,''),COALESCE(r.commit_hash,''),a.health_status,a.health_path,
COALESCE(a.health_checked_at,''),COALESCE(a.last_deploy_at,''),a.needs_restart,a.image,a.internal_port,a.compose_service,
a.dns_record_id,a.created_at,a.updated_at,
(SELECT COUNT(*) FROM app_env e WHERE e.app_id=a.id),
COALESCE((SELECT d.status FROM deployments d WHERE d.app_id=a.id ORDER BY d.id DESC LIMIT 1),'')
FROM apps a LEFT JOIN releases r ON r.id=a.active_release_id`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanApp(s rowScanner) (*App, error) {
	var a App
	var relID sql.NullInt64
	var dirs string
	if err := s.Scan(&a.ID, &a.Name, &a.Slug, &a.SourceType, &a.RepoURL, &a.Branch, &a.Runtime, &a.RuntimeVersion,
		&a.Mode, &a.Driver, &a.InstallCmd, &a.BuildCmd, &a.StartCmd, &a.Port, &a.Hostname, &a.AutoDeploy,
		&a.WebhookID, &a.WebhookSecretEnc, &a.Workdir, &dirs, &a.Status, &relID, &a.ActiveReleaseDir, &a.ActiveCommit,
		&a.HealthStatus, &a.HealthPath, &a.HealthCheckedAt, &a.LastDeployAt, &a.NeedsRestart, &a.Image,
		&a.InternalPort, &a.ComposeService, &a.DNSRecordID, &a.CreatedAt, &a.UpdatedAt, &a.EnvCount, &a.LastDeployState); err != nil {
		return nil, err
	}
	if relID.Valid {
		v := relID.Int64
		a.ActiveReleaseID = &v
	}
	_ = json.Unmarshal([]byte(dirs), &a.PersistentDirs)
	if a.PersistentDirs == nil {
		a.PersistentDirs = []string{}
	}
	return &a, nil
}

func (p *Panel) getApp(id int64) (*App, error) {
	a, err := scanApp(p.db.QueryRow(appSelect+` WHERE a.id=?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errNotFound
		}
		return nil, err
	}
	if p.procMgr != nil {
		a.Running = p.procMgr.isRunning(id)
		if !a.Running && p.healthOK(a.Port, a.HealthPath) {
			a.Running = true
		}
	} else {
		a.Running = a.Status == "berjalan"
	}
	if a.Running {
		a.Status = "berjalan"
	}
	return a, nil
}

func (p *Panel) listApps() ([]*App, error) {
	rows, err := p.db.Query(appSelect + ` ORDER BY a.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*App
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		if p.procMgr != nil {
			a.Running = p.procMgr.isRunning(a.ID) || a.Status == "berjalan"
			if a.Running {
				a.Status = "berjalan"
			}
		}
		out = append(out, a)
	}
	if out == nil {
		out = []*App{}
	}
	return out, nil
}

func (p *Panel) handleApps(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		apps, err := p.listApps()
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"apps": apps, "count": len(apps)})
		return
	}
	var req appInput
	if !decodeJSON(w, r, &req) {
		return
	}
	a, err := p.createApp(req)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Gagal membuat aplikasi", err.Error())
		return
	}
	p.audit("app.create", "app", fmt.Sprint(a.ID), "Aplikasi "+a.Name+" dibuat ("+a.Runtime+", port "+strconv.Itoa(a.Port)+")")
	writeJSON(w, http.StatusCreated, map[string]any{"app": a})
}

type appInput struct {
	Name           string   `json:"name"`
	Slug           string   `json:"slug"`
	SourceType     string   `json:"sourceType"`
	RepoURL        string   `json:"repoUrl"`
	Branch         string   `json:"branch"`
	Runtime        string   `json:"runtime"`
	RuntimeVersion string   `json:"runtimeVersion"`
	Mode           string   `json:"mode"`
	Driver         string   `json:"driver"`
	InstallCmd     string   `json:"installCmd"`
	BuildCmd       string   `json:"buildCmd"`
	StartCmd       string   `json:"startCmd"`
	Port           int      `json:"port"`
	Hostname       string   `json:"hostname"`
	AutoDeploy     bool     `json:"autoDeploy"`
	PersistentDirs []string `json:"persistentDirs"`
	HealthPath     string   `json:"healthPath"`
	Image          string   `json:"image"`
	InternalPort   int      `json:"internalPort"`
	ComposeService string   `json:"composeService"`
}

func (p *Panel) createApp(in appInput) (*App, error) {
	name := strings.TrimSpace(in.Name)
	if len(name) < 2 || len(name) > 64 {
		return nil, fmt.Errorf("nama aplikasi 2–64 karakter")
	}
	slug := slugify(in.Slug)
	if slug == "" {
		slug = slugify(name)
	}
	base := slug
	for i := 2; ; i++ {
		var n int
		_ = p.db.QueryRow(`SELECT COUNT(*) FROM apps WHERE slug=?`, slug).Scan(&n)
		if n == 0 {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	src := in.SourceType
	if src == "" {
		src = "github"
	}
	switch src {
	case "github", "git_https", "git_ssh", "image":
	default:
		return nil, fmt.Errorf("sumber tidak dikenal")
	}
	mode := in.Mode
	if mode == "" {
		mode = "native"
	}
	switch mode {
	case "native", "docker", "compose":
	default:
		return nil, fmt.Errorf("mode jalan tidak dikenal")
	}
	if src != "image" && strings.TrimSpace(in.RepoURL) == "" {
		return nil, fmt.Errorf("URL repo wajib diisi")
	}
	port := in.Port
	if port == 0 {
		var err error
		port, err = p.allocatePort(0)
		if err != nil {
			return nil, err
		}
	} else if err := p.validatePort(port, 0); err != nil {
		return nil, err
	}
	dirs := sanitizeDirs(in.PersistentDirs)
	healthPath := in.HealthPath
	if healthPath == "" {
		healthPath = "/"
	}
	install, build, start := in.InstallCmd, in.BuildCmd, in.StartCmd
	if start == "" && mode == "native" {
		d, b, s := runtimeDefaults(in.Runtime)
		if install == "" {
			install = d
		}
		if build == "" {
			build = b
		}
		start = s
	}
	now := nowUTC()
	workdir := p.appWorkdir(slug)
	webhookSecret := ""
	if in.AutoDeploy {
		webhookSecret, _ = p.encrypt(randomToken(32))
	}
	internalPort := in.InternalPort
	if internalPort == 0 && mode == "docker" {
		internalPort = 80
	}
	res, err := p.db.Exec(`INSERT INTO apps(name,slug,source_type,repo_url,branch,runtime,runtime_version,mode,driver,install_cmd,build_cmd,start_cmd,port,hostname,auto_deploy,webhook_secret,workdir,persistent_dirs,health_path,image,internal_port,compose_service,status,health_status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		name, slug, src, strings.TrimSpace(in.RepoURL), defaultStr(in.Branch, "main"), defaultStr(in.Runtime, "node"), in.RuntimeVersion,
		mode, defaultStr(in.Driver, "auto"), install, build, start, port, strings.TrimSpace(in.Hostname), in.AutoDeploy,
		webhookSecret, workdir, jsonString(dirs), healthPath, in.Image, internalPort, in.ComposeService, "berhenti", "unknown", now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	for _, d := range dirs {
		_ = os.MkdirAll(filepath.Join(workdir, "persistent", filepath.FromSlash(d)), 0o750)
	}
	_ = os.MkdirAll(filepath.Join(workdir, "releases"), 0o750)
	a, err := p.getApp(id)
	if err != nil {
		return nil, err
	}
	if a.Hostname != "" {
		go p.syncAppIngress(a.ID, "")
	}
	if a.AutoDeploy && p.githubToken() != "" && a.SourceType != "image" {
		if err := p.ensureGitHubWebhook(a); err != nil {
			p.logf("webhook GitHub: %v", err)
		}
	}
	return a, nil
}

func runtimeDefaults(runtimeName string) (install, build, start string) {
	switch runtimeName {
	case "php":
		return "composer install --no-dev --optimize-autoloader --no-interaction", "", "php -S 127.0.0.1:$PORT -t public"
	case "python":
		return "python3 -m venv .venv && .venv/bin/pip install -r requirements.txt", "", ".venv/bin/gunicorn -b 127.0.0.1:$PORT app:app"
	case "go":
		return "go mod download", "go build -o app .", "./app"
	case "static":
		return "", "", "python3 -m http.server $PORT --bind 127.0.0.1"
	default:
		return "npm ci", "npm run build --if-present", "npm start"
	}
}

func defaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func sanitizeDirs(dirs []string) []string {
	out := []string{}
	for _, d := range dirs {
		d = strings.TrimSpace(strings.ReplaceAll(d, "\\", "/"))
		d = strings.Trim(d, "/")
		if d == "" || strings.Contains(d, "..") {
			continue
		}
		out = append(out, d)
	}
	return out
}

func (p *Panel) allocatePort(preferred int) (int, error) {
	if preferred > 0 {
		if err := p.validatePort(preferred, 0); err != nil {
			return 0, err
		}
		return preferred, nil
	}
	start := p.intSetting("port_pool_start", 3100)
	end := p.intSetting("port_pool_end", 3999)
	panelPort := p.intSetting("panel_port", 8282)
	for port := start; port <= end; port++ {
		if port == panelPort {
			continue
		}
		var n int
		_ = p.db.QueryRow(`SELECT COUNT(*) FROM apps WHERE port=?`, port).Scan(&n)
		if n > 0 {
			continue
		}
		if p.portOpen(port) {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("kolam port %d–%d penuh", start, end)
}

func (p *Panel) validatePort(port int, appID int64) error {
	start := p.intSetting("port_pool_start", 3100)
	end := p.intSetting("port_pool_end", 3999)
	panelPort := p.intSetting("panel_port", 8282)
	if port < 1 || port > 65535 {
		return fmt.Errorf("port tidak valid")
	}
	if port == panelPort {
		return fmt.Errorf("port %d dipakai panel", port)
	}
	if port < start || port > end {
		return fmt.Errorf("port harus di dalam kolam %d–%d", start, end)
	}
	var n int
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM apps WHERE port=? AND id<>?`, port, appID).Scan(&n)
	if n > 0 {
		return fmt.Errorf("port %d sudah dipakai aplikasi lain", port)
	}
	if p.portOpen(port) {
		return fmt.Errorf("port %d sedang didengarkan layanan lain", port)
	}
	return nil
}

func (p *Panel) handleApp(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	a, err := p.getApp(id)
	if err != nil {
		httpError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		var releases, deployments any
		rels, _ := p.listReleases(id)
		deps, _ := p.listDeployments(id, 30)
		releases, deployments = rels, deps
		env, _ := p.listEnv(id)
		var dbLink any
		var engine, name, host, user string
		var port int
		var dbID int64
		var filePath string
		err := p.db.QueryRow(`SELECT id,engine,name,host,port,username,file_path FROM databases WHERE app_id=? ORDER BY id LIMIT 1`, id).
			Scan(&dbID, &engine, &name, &host, &port, &user, &filePath)
		if err == nil {
			dbLink = map[string]any{"id": dbID, "engine": engine, "name": name, "host": host, "port": port, "username": user, "filePath": filePath}
		}
		redisAlloc, _ := p.redisAllocation(id)
		driver := a.Driver
		injected := map[string]string{"PORT": strconv.Itoa(a.Port), "APP_PORT": strconv.Itoa(a.Port), "APP_NAME": a.Slug, "APP_ENV": "production"}
		if dbLink != nil {
			injected["DATABASE_URL"] = "(dari database terikat)"
		}
		if redisAlloc != nil {
			injected["REDIS_URL"] = "(dari alokasi Redis)"
			injected["REDIS_PREFIX"] = redisAlloc.Prefix
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"app": a, "releases": releases, "deployments": deployments, "env": env,
			"database": dbLink, "redis": redisAlloc, "driver": driver, "injected": injected,
			"webhookUrl": p.baseURL(r) + "/api/webhooks/github/" + strconv.FormatInt(id, 10),
		})
	case http.MethodPut:
		var in appInput
		if !decodeJSON(w, r, &in) {
			return
		}
		if err := p.updateApp(a, in); err != nil {
			failDetail(w, http.StatusBadRequest, "Gagal menyimpan aplikasi", err.Error())
			return
		}
		p.audit("app.update", "app", fmt.Sprint(id), "Aplikasi "+a.Name+" diubah")
		updated, _ := p.getApp(id)
		if updated != nil && updated.AutoDeploy && updated.WebhookID == "" && p.githubToken() != "" {
			if err := p.ensureGitHubWebhook(updated); err != nil {
				p.logf("webhook GitHub: %v", err)
			}
		}
		if updated != nil && updated.Hostname != a.Hostname {
			go p.syncAppIngress(id, a.Hostname)
		}
		writeJSON(w, http.StatusOK, map[string]any{"app": updated})
	case http.MethodDelete:
		delFiles := qBool(r, "deleteFiles")
		p.deleteApp(a, delFiles)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
	}
}

func (p *Panel) updateApp(a *App, in appInput) error {
	if in.Name != "" && in.Name != a.Name {
		if len(strings.TrimSpace(in.Name)) < 2 {
			return fmt.Errorf("nama aplikasi terlalu pendek")
		}
		a.Name = strings.TrimSpace(in.Name)
	}
	if in.Port != 0 && in.Port != a.Port {
		if err := p.validatePort(in.Port, a.ID); err != nil {
			return err
		}
		a.Port = in.Port
	}
	if in.Hostname != a.Hostname {
		a.Hostname = strings.TrimSpace(in.Hostname)
		if a.Hostname != "" && !validHostname(a.Hostname) {
			return fmt.Errorf("hostname tidak valid")
		}
	}
	if in.AutoDeploy && !a.AutoDeploy {
		secret, _ := p.encrypt(randomToken(32))
		_, _ = p.db.Exec(`UPDATE apps SET webhook_secret=? WHERE id=?`, secret, a.ID)
	}
	dirs := sanitizeDirs(in.PersistentDirs)
	driver := in.Driver
	if driver == "" {
		driver = a.Driver
	}
	healthPath := in.HealthPath
	if healthPath == "" {
		healthPath = a.HealthPath
	}
	mode := defaultStr(in.Mode, a.Mode)
	internalPort := in.InternalPort
	if internalPort == 0 {
		internalPort = a.InternalPort
	}
	sourceType := defaultStr(in.SourceType, a.SourceType)
	switch sourceType {
	case "github", "git_https", "git_ssh", "image":
	default:
		return fmt.Errorf("sumber tidak dikenal")
	}
	repoURL := in.RepoURL
	if repoURL == "" {
		repoURL = a.RepoURL
	}
	if sourceType != "image" && strings.TrimSpace(repoURL) == "" {
		return fmt.Errorf("URL repo wajib diisi")
	}
	_, err := p.db.Exec(`UPDATE apps SET name=?, source_type=?, repo_url=?, branch=?, runtime=?, runtime_version=?, mode=?, driver=?, install_cmd=?, build_cmd=?, start_cmd=?, port=?, hostname=?, auto_deploy=?, persistent_dirs=?, health_path=?, image=?, internal_port=?, compose_service=?, updated_at=? WHERE id=?`,
		a.Name, sourceType, repoURL, defaultStr(in.Branch, a.Branch), defaultStr(in.Runtime, a.Runtime), in.RuntimeVersion, mode, driver,
		in.InstallCmd, in.BuildCmd, in.StartCmd, a.Port, a.Hostname, in.AutoDeploy, jsonString(dirs), healthPath,
		in.Image, internalPort, in.ComposeService, nowUTC(), a.ID)
	if err != nil {
		return err
	}
	for _, d := range dirs {
		_ = os.MkdirAll(filepath.Join(a.Workdir, "persistent", filepath.FromSlash(d)), 0o750)
	}
	a.Mode, a.Driver = mode, driver
	a.SourceType, a.RepoURL = sourceType, repoURL
	a.PersistentDirs = dirs
	a.HealthPath = healthPath
	return nil
}

func (p *Panel) deleteApp(a *App, deleteFiles bool) {
	// hentikan proses lebih dulu
	if p.procMgr != nil {
		p.procMgr.stopApp(a, true)
	}
	if a.WebhookID != "" {
		_ = p.deleteGitHubWebhook(a)
	}
	if alloc, err := p.redisAllocation(a.ID); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		c := p.redisAdminClient()
		_ = c.Do(ctx, "ACL", "DELUSER", alloc.ACLUser).Err()
		_ = c.Do(ctx, "ACL", "SAVE").Err()
		c.Close()
		cancel()
	}
	p.removeAppIngress(*a)
	if deleteFiles && a.Workdir != "" && strings.HasPrefix(a.Workdir, p.appsDir()) {
		_ = os.RemoveAll(a.Workdir)
	}
	if a.ActiveReleaseID != nil {
		_, _ = p.db.Exec(`UPDATE apps SET active_release_id=NULL WHERE id=?`, a.ID)
	}
	_, _ = p.db.Exec(`UPDATE databases SET app_id=NULL WHERE app_id=?`, a.ID)
	_, _ = p.db.Exec(`DELETE FROM cron_jobs WHERE app_id=?`, a.ID)
	if _, err := p.db.Exec(`DELETE FROM apps WHERE id=?`, a.ID); err != nil {
		p.logf("hapus aplikasi: %v", err)
	}
	p.audit("app.delete", "app", fmt.Sprint(a.ID), "Aplikasi "+a.Name+" dihapus"+map[bool]string{true: " beserta folder kerjanya", false: ""}[deleteFiles])
}

func validHostname(h string) bool {
	if len(h) > 253 {
		return false
	}
	re := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`)
	return re.MatchString(h)
}

func (p *Panel) handleAppAction(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	action := r.PathValue("action")
	a, err := p.getApp(id)
	if err != nil {
		httpError(w, err)
		return
	}
	var opErr error
	switch action {
	case "start":
		opErr = p.procMgr.startApp(a)
		if opErr == nil {
			_, _ = p.db.Exec(`UPDATE apps SET status='berjalan', needs_restart=0, updated_at=? WHERE id=?`, nowUTC(), id)
		}
	case "stop":
		p.procMgr.stopApp(a, false)
	case "restart":
		opErr = p.procMgr.restartApp(a)
		if opErr == nil {
			_, _ = p.db.Exec(`UPDATE apps SET needs_restart=0, status='berjalan' WHERE id=?`, id)
		}
	default:
		fail(w, http.StatusBadRequest, "Aksi tidak dikenal")
		return
	}
	if opErr != nil {
		_, _ = p.db.Exec(`UPDATE apps SET status='bermasalah', updated_at=? WHERE id=?`, nowUTC(), id)
		failDetail(w, http.StatusBadRequest, "Aksi gagal", opErr.Error())
		return
	}
	go p.checkAppHealth(id)
	p.audit("app."+action, "app", fmt.Sprint(id), "Aksi "+action+" pada "+a.Name)
	fresh, _ := p.getApp(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "app": fresh})
}

func (p *Panel) checkAppHealth(id int64) {
	a, err := p.getApp(id)
	if err != nil {
		return
	}
	ok, code, detail := p.healthDetail(a.Port, a.HealthPath)
	if a.Mode == "docker" || a.Mode == "compose" {
		code = 0
		ok = p.healthOK(a.Port, a.HealthPath)
	}
	status := "sehat"
	if !ok {
		status = "gagal"
	}
	appStatus := a.Status
	if ok {
		appStatus = "berjalan"
	}
	_, _ = p.db.Exec(`UPDATE apps SET health_status=?, health_checked_at=?, status=?, updated_at=? WHERE id=?`,
		status, nowUTC(), appStatus, nowUTC(), id)
	if !ok && detail != "" {
		_, _ = p.db.Exec(`INSERT INTO audit_logs(actor,action,object_type,object_id,summary,created_at) VALUES('panel','app.health','app',?,'Health check gagal: '+?,?)`,
			fmt.Sprint(id), truncate(detail, 200), nowUTC())
	}
	_ = code
	p.publish("app.health", map[string]any{"appId": id, "health": status})
}

func (p *Panel) handleAppEnv(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	a, err := p.getApp(id)
	if err != nil {
		httpError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		env, err := p.listEnv(id)
		if err != nil {
			httpError(w, err)
			return
		}
		injected := map[string]string{"PORT": strconv.Itoa(a.Port), "APP_PORT": strconv.Itoa(a.Port), "APP_NAME": a.Slug, "APP_ENV": "production"}
		if err := p.db.QueryRow(`SELECT COUNT(*) FROM databases WHERE app_id=?`, id).Scan(new(int)); err == nil {
			injected["DATABASE_URL"] = "(dari database terikat)"
		}
		if _, err := p.redisAllocation(id); err == nil {
			injected["REDIS_URL"] = "(dari alokasi Redis)"
			injected["REDIS_PREFIX"] = "app_" + a.Slug + ":"
		}
		writeJSON(w, http.StatusOK, map[string]any{"env": env, "injected": injected})
		return
	}
	var req struct {
		Vars []struct {
			Key      string `json:"key"`
			Value    string `json:"value"`
			IsSecret bool   `json:"isSecret"`
			Keep     bool   `json:"keep"`
		} `json:"vars"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	type oldVal struct{ val string }
	old := map[string]string{}
	rows, _ := p.db.Query(`SELECT key,value FROM app_env WHERE app_id=?`, id)
	if rows != nil {
		for rows.Next() {
			var k, v string
			if rows.Scan(&k, &v) == nil {
				old[k] = v
			}
		}
		rows.Close()
	}
	seen := map[string]bool{}
	tx, err := p.db.Begin()
	if err != nil {
		httpError(w, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM app_env WHERE app_id=?`, id); err != nil {
		tx.Rollback()
		httpError(w, err)
		return
	}
	for _, v := range req.Vars {
		key := strings.TrimSpace(v.Key)
		if key == "" {
			continue
		}
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(key) {
			tx.Rollback()
			fail(w, http.StatusBadRequest, "Nama variabel tidak valid: "+key)
			return
		}
		if seen[key] {
			tx.Rollback()
			fail(w, http.StatusBadRequest, "Variabel ganda: "+key)
			return
		}
		seen[key] = true
		val := v.Value
		if v.Keep && v.IsSecret {
			if prev, ok := old[key]; ok {
				val = prev
			}
		} else {
			enc, err := p.encrypt(val)
			if err != nil {
				tx.Rollback()
				httpError(w, err)
				return
			}
			val = enc
		}
		if v.IsSecret && val != "" && !strings.HasPrefix(val, "enc:v1:") {
			enc, _ := p.encrypt(val)
			val = enc
		}
		if _, err := tx.Exec(`INSERT INTO app_env(app_id,key,value,is_secret) VALUES(?,?,?,?)`, id, key, val, v.IsSecret); err != nil {
			tx.Rollback()
			httpError(w, err)
			return
		}
	}
	if _, err := tx.Exec(`UPDATE apps SET needs_restart=1, updated_at=? WHERE id=?`, nowUTC(), id); err != nil {
		tx.Rollback()
		httpError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpError(w, err)
		return
	}
	keys := make([]string, 0, len(req.Vars))
	for _, v := range req.Vars {
		keys = append(keys, v.Key)
	}
	p.audit("app.env_update", "app", fmt.Sprint(id), "Variabel lingkungan "+a.Name+" diubah ("+strings.Join(keys, ", ")+")")
	p.publish("app.env", map[string]any{"appId": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "needsRestart": true})
}

func (p *Panel) listEnv(appID int64) ([]map[string]any, error) {
	rows, err := p.db.Query(`SELECT key,value,is_secret FROM app_env WHERE app_id=? ORDER BY key`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var k, v string
		var secret int
		if rows.Scan(&k, &v, &secret) == nil {
			item := map[string]any{"key": k, "isSecret": secret == 1}
			if secret == 1 {
				item["value"] = ""
				item["hasValue"] = v != ""
			} else {
				item["value"] = p.decrypt(v)
			}
			out = append(out, item)
		}
	}
	return out, nil
}

func (p *Panel) handlePorts(w http.ResponseWriter, r *http.Request) {
	apps, err := p.listApps()
	if err != nil {
		httpError(w, err)
		return
	}
	type mapping struct {
		AppID    int64  `json:"appId"`
		App      string `json:"app"`
		Slug     string `json:"slug"`
		Port     int    `json:"port"`
		Hostname string `json:"hostname"`
		Status   string `json:"status"`
		Health   string `json:"health"`
		Checked  string `json:"checkedAt"`
	}
	out := []mapping{}
	for _, a := range apps {
		out = append(out, mapping{a.ID, a.Name, a.Slug, a.Port, a.Hostname, a.Status, a.HealthStatus, a.HealthCheckedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"apps":      out,
		"pool":      map[string]int{"start": p.intSetting("port_pool_start", 3100), "end": p.intSetting("port_pool_end", 3999)},
		"panelPort": p.intSetting("panel_port", 8282),
	})
}

func (p *Panel) handleNextPort(w http.ResponseWriter, r *http.Request) {
	port, err := p.allocatePort(0)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"port": port})
}

func (p *Panel) refreshAppStatus(id int64) string {
	a, err := p.getApp(id)
	if err != nil {
		return "berhenti"
	}
	if p.procMgr.isRunning(id) {
		return "berjalan"
	}
	return a.Status
}
