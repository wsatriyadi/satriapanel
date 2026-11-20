package panel

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Release struct {
	ID         int64  `json:"id"`
	AppID      int64  `json:"appId"`
	CommitHash string `json:"commitHash"`
	DirPath    string `json:"dirPath"`
	ImageTag   string `json:"imageTag"`
	BuiltAt    string `json:"builtAt"`
	Status     string `json:"status"`
}

type Deployment struct {
	ID          int64  `json:"id"`
	AppID       int64  `json:"appId"`
	AppName     string `json:"appName"`
	ReleaseID   *int64 `json:"releaseId"`
	Trigger     string `json:"trigger"`
	Status      string `json:"status"`
	CommitHash  string `json:"commitHash"`
	LogPath     string `json:"-"`
	Error       string `json:"error"`
	StartedAt   string `json:"startedAt"`
	FinishedAt  string `json:"finishedAt"`
	CreatedAt   string `json:"createdAt"`
	DurationSec int    `json:"durationSec"`
}

func (p *Panel) listReleases(appID int64) ([]Release, error) {
	rows, err := p.db.Query(`SELECT id,app_id,commit_hash,dir_path,image_tag,built_at,status FROM releases WHERE app_id=? ORDER BY id DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Release{}
	for rows.Next() {
		var r Release
		if rows.Scan(&r.ID, &r.AppID, &r.CommitHash, &r.DirPath, &r.ImageTag, &r.BuiltAt, &r.Status) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

func (p *Panel) listDeployments(appID int64, limit int) ([]Deployment, error) {
	if limit <= 0 {
		limit = 30
	}
	q := `SELECT d.id,d.app_id,COALESCE(a.name,''),d.release_id,d.trigger,d.status,d.commit_hash,d.log_path,d.error,
		COALESCE(d.started_at,''),COALESCE(d.finished_at,''),d.created_at
		FROM deployments d LEFT JOIN apps a ON a.id=d.app_id`
	args := []any{}
	if appID > 0 {
		q += ` WHERE d.app_id=?`
		args = append(args, appID)
	}
	q += ` ORDER BY d.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := p.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Deployment{}
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err == nil {
			out = append(out, *d)
		}
	}
	return out, nil
}

func scanDeployment(s rowScanner) (*Deployment, error) {
	var d Deployment
	var rel sql.NullInt64
	if err := s.Scan(&d.ID, &d.AppID, &d.AppName, &rel, &d.Trigger, &d.Status, &d.CommitHash, &d.LogPath, &d.Error,
		&d.StartedAt, &d.FinishedAt, &d.CreatedAt); err != nil {
		return nil, err
	}
	if rel.Valid {
		v := rel.Int64
		d.ReleaseID = &v
	}
	if d.StartedAt != "" && d.FinishedAt != "" {
		d.DurationSec = int(parseTime(d.FinishedAt).Sub(parseTime(d.StartedAt)).Seconds())
	}
	return &d, nil
}

func (p *Panel) getDeployment(id int64) (*Deployment, error) {
	d, err := scanDeployment(p.db.QueryRow(`SELECT d.id,d.app_id,COALESCE(a.name,''),d.release_id,d.trigger,d.status,d.commit_hash,d.log_path,d.error,
		COALESCE(d.started_at,''),COALESCE(d.finished_at,''),d.created_at
		FROM deployments d LEFT JOIN apps a ON a.id=d.app_id WHERE d.id=?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errNotFound
		}
		return nil, err
	}
	return d, nil
}

// ---------- deploy manager ----------

type deployManager struct {
	p        *Panel
	queue    chan int64
	mu       sync.Mutex
	cancels  map[int64]context.CancelFunc
	running  int64
	ctx      context.Context
	started  bool
	workerWG sync.WaitGroup
}

func newDeployManager(p *Panel) *deployManager {
	return &deployManager{p: p, queue: make(chan int64, 100), cancels: map[int64]context.CancelFunc{}}
}

func (d *deployManager) start(ctx context.Context) {
	d.ctx = ctx
	d.mu.Lock()
	if d.started {
		d.mu.Unlock()
		return
	}
	d.started = true
	d.mu.Unlock()
	d.workerWG.Add(1)
	go d.worker()
}

func (d *deployManager) stop() {
	if d.ctx != nil {
		d.mu.Lock()
		for _, c := range d.cancels {
			c()
		}
		d.mu.Unlock()
	}
	d.workerWG.Wait()
}

func (d *deployManager) enqueue(appID int64, trigger string) (*Deployment, error) {
	app, err := d.p.getApp(appID)
	if err != nil {
		return nil, err
	}
	var running int
	_ = d.p.db.QueryRow(`SELECT COUNT(*) FROM deployments WHERE app_id=? AND status='berjalan'`, appID).Scan(&running)
	if running > 0 {
		return nil, fmt.Errorf("aplikasi sedang dalam proses deploy")
	}
	logPath := filepath.Join(d.p.logsDir(), "deploy", fmt.Sprintf("%d-%d.log", appID, time.Now().Unix()))
	now := nowUTC()
	res, err := d.p.db.Exec(`INSERT INTO deployments(app_id,trigger,status,log_path,created_at) VALUES(?,?,?,?,?)`,
		appID, trigger, "antre", logPath, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	d.p.audit("deploy.queue", "app", fmt.Sprint(appID), "Deploy "+app.Name+" diantrekan ("+trigger+")")
	select {
	case d.queue <- id:
	default:
		_, _ = d.p.db.Exec(`UPDATE deployments SET status='gagal', error='Antrean deploy penuh', finished_at=? WHERE id=?`, nowUTC(), id)
		return nil, fmt.Errorf("antrean deploy penuh")
	}
	d.p.publish("deploy.queue", map[string]any{"deploymentId": id, "appId": appID})
	return d.p.getDeployment(id)
}

func (d *deployManager) cancel(deploymentID int64) bool {
	d.mu.Lock()
	cancel, running := d.cancels[deploymentID]
	d.mu.Unlock()
	if running {
		cancel()
		return true
	}
	res, err := d.p.db.Exec(`UPDATE deployments SET status='dibatalkan', error='Dibatalkan sebelum berjalan', finished_at=? WHERE id=? AND status='antre'`, nowUTC(), deploymentID)
	if err == nil {
		n, _ := res.RowsAffected()
		return n > 0
	}
	return false
}

func (d *deployManager) worker() {
	defer d.workerWG.Done()
	for {
		select {
		case <-d.ctx.Done():
			return
		case id := <-d.queue:
			d.run(id)
		}
	}
}

func (d *deployManager) run(deploymentID int64) {
	dep, err := d.p.getDeployment(deploymentID)
	if err != nil || dep.Status != "antre" {
		return
	}
	app, err := d.p.getApp(dep.AppID)
	if err != nil {
		_, _ = d.p.db.Exec(`UPDATE deployments SET status='gagal', error='Aplikasi tidak ditemukan', finished_at=? WHERE id=?`, nowUTC(), deploymentID)
		return
	}
	ctx, cancel := context.WithCancel(d.ctx)
	d.mu.Lock()
	d.cancels[deploymentID] = cancel
	d.running = deploymentID
	d.mu.Unlock()
	defer func() {
		cancel()
		d.mu.Lock()
		delete(d.cancels, deploymentID)
		d.running = 0
		d.mu.Unlock()
	}()

	started := nowUTC()
	_, _ = d.p.db.Exec(`UPDATE deployments SET status='berjalan', started_at=?, commit_hash='' WHERE id=?`, started, deploymentID)
	_, _ = d.p.db.Exec(`UPDATE apps SET status='deploy', updated_at=? WHERE id=?`, started, app.ID)
	d.p.publish("deploy.start", map[string]any{"deploymentId": deploymentID, "appId": app.ID})

	logPath := dep.LogPath
	if logPath == "" {
		logPath = filepath.Join(d.p.logsDir(), "deploy", fmt.Sprintf("%d-deploy-%d.log", app.ID, deploymentID))
	}
	_ = os.MkdirAll(filepath.Dir(logPath), 0o750)
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		_, _ = d.p.db.Exec(`UPDATE deployments SET status='gagal', error=?, finished_at=? WHERE id=?`, err.Error(), nowUTC(), deploymentID)
		return
	}
	log := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		line = maskSecrets(line, d.p.secretValues(app))
		_, _ = lf.WriteString(time.Now().Format("15:04:05") + "  " + line + "\n")
		_ = lf.Sync()
	}

	log("Deploy #%d dimulai untuk %s (%s)", deploymentID, app.Name, dep.Trigger)
	log("Runtime: %s %s · mode: %s · port: %d", app.Runtime, app.RuntimeVersion, app.Mode, app.Port)

	prevActive := app.ActiveReleaseID
	newReleaseID, dir, imageTag, err := d.build(ctx, app, log)
	if err != nil {
		log("GAGAL: %v", err)
		l := lf
		defer l.Close()
		if newReleaseID > 0 {
			_, _ = d.p.db.Exec(`UPDATE releases SET status='gagal' WHERE id=?`, newReleaseID)
		}
		_, _ = d.p.db.Exec(`UPDATE deployments SET status='gagal', error=?, finished_at=? WHERE id=?`, truncate(err.Error(), 500), nowUTC(), deploymentID)
		_, _ = d.p.db.Exec(`UPDATE apps SET status=CASE WHEN active_release_id IS NULL THEN 'bermasalah' ELSE 'berjalan' END, updated_at=? WHERE id=?`, nowUTC(), app.ID)
		d.p.notify("deploy", "Deploy gagal: "+app.Name, "Deploy #"+strconv.FormatInt(deploymentID, 10)+" gagal. "+truncate(err.Error(), 180)+". Rilis aktif tidak berubah.", &app.ID)
		d.p.audit("deploy.failed", "app", fmt.Sprint(app.ID), "Deploy "+app.Name+" gagal: "+truncate(err.Error(), 180))
		d.p.publish("deploy.end", map[string]any{"deploymentId": deploymentID, "appId": app.ID, "status": "gagal"})
		return
	}

	if dir != "" {
		log("Rilis baru siap: %s", dir)
	}
	// aktifkan rilis
	_, _ = d.p.db.Exec(`UPDATE releases SET status='arsip' WHERE app_id=? AND status='aktif'`, app.ID)
	_, _ = d.p.db.Exec(`UPDATE releases SET status='aktif' WHERE id=?`, newReleaseID)
	if _, err := d.p.db.Exec(`UPDATE apps SET active_release_id=?, last_deploy_at=?, health_status='unknown', needs_restart=0, updated_at=? WHERE id=?`,
		newReleaseID, nowUTC(), nowUTC(), app.ID); err != nil {
		log("PERINGATAN: gagal menyimpan rilis aktif: %v", err)
	}
	dep.ReleaseID = &newReleaseID
	_, _ = d.p.db.Exec(`UPDATE deployments SET release_id=?, commit_hash=? WHERE id=?`, newReleaseID, lastCommit(app.RepoURL, dir, d.p), deploymentID)

	log("Mengaktifkan rilis dan menjalankan proses…")
	fresh, _ := d.p.getApp(app.ID)
	if fresh != nil && fresh.ActiveReleaseDir == "" && dir != "" {
		fresh.ActiveReleaseDir = dir
	}
	if fresh != nil && fresh.ActiveReleaseID == nil && newReleaseID > 0 {
		rid := newReleaseID
		fresh.ActiveReleaseID = &rid
	}
	if fresh != nil {
		log("Rilis #%d · direktori proses: %s", newReleaseID, fresh.ActiveReleaseDir)
	}
	startErr := d.p.procMgr.startApp(fresh)
	healthOK := false
	if startErr == nil {
		for i := 0; i < 25; i++ {
			select {
			case <-ctx.Done():
				startErr = fmt.Errorf("deploy dibatalkan")
			case <-time.After(700 * time.Millisecond):
			}
			if startErr != nil {
				break
			}
			if d.p.healthOK(app.Port, app.HealthPath) {
				healthOK = true
				break
			}
		}
	} else {
		log("Proses gagal start: %v", startErr)
	}
	if startErr == nil && !healthOK {
		startErr = fmt.Errorf("health check http://127.0.0.1:%d%s gagal setelah 17 detik", app.Port, app.HealthPath)
	}
	if startErr != nil {
		log("GAGAL: %v", startErr)
		log("Mengembalikan rilis sebelumnya secara otomatis…")
		if prevActive != nil {
			_, _ = d.p.db.Exec(`UPDATE releases SET status='arsip' WHERE id=?`, newReleaseID)
			_, _ = d.p.db.Exec(`UPDATE releases SET status='aktif' WHERE id=?`, *prevActive)
			_, _ = d.p.db.Exec(`UPDATE apps SET active_release_id=?, status='berjalan', health_status='unknown', updated_at=? WHERE id=?`, *prevActive, nowUTC(), app.ID)
			_, _ = d.p.db.Exec(`UPDATE releases SET status='gagal' WHERE id=?`, newReleaseID)
			if old, err := d.p.getApp(app.ID); err == nil {
				if old.ActiveReleaseDir == "" {
					_ = d.p.db.QueryRow(`SELECT dir_path FROM releases WHERE id=?`, *prevActive).Scan(&old.ActiveReleaseDir)
				}
				_ = d.p.procMgr.startApp(old)
			}
			log("Rilis aktif dikembalikan ke #%d; versi lama tetap melayani.", *prevActive)
		} else {
			_, _ = d.p.db.Exec(`UPDATE releases SET status='gagal' WHERE id=?`, newReleaseID)
			_, _ = d.p.db.Exec(`UPDATE apps SET status='bermasalah', active_release_id=NULL, updated_at=? WHERE id=?`, nowUTC(), app.ID)
			log("Tidak ada rilis sebelumnya untuk dikembalikan.")
		}
		_, _ = d.p.db.Exec(`UPDATE deployments SET status='gagal-terpulihkan', error=?, finished_at=? WHERE id=?`, truncate(startErr.Error(), 500), nowUTC(), deploymentID)
		d.p.notify("deploy", "Rollback otomatis: "+app.Name, "Deploy #"+strconv.FormatInt(deploymentID, 10)+" gagal di health check; rilis sebelumnya dipulihkan otomatis.", &app.ID)
		d.p.audit("deploy.auto_rollback", "app", fmt.Sprint(app.ID), "Deploy "+app.Name+" gagal; rollback otomatis ke rilis sebelumnya")
		d.p.publish("deploy.end", map[string]any{"deploymentId": deploymentID, "appId": app.ID, "status": "gagal-terpulihkan"})
		lf.Close()
		return
	}
	log("Health check OK — aplikasi melayani di port %d.", app.Port)
	_, _ = d.p.db.Exec(`UPDATE deployments SET status='berhasil', finished_at=? WHERE id=?`, nowUTC(), deploymentID)
	_, _ = d.p.db.Exec(`UPDATE apps SET status='berjalan', health_status='sehat', health_checked_at=?, updated_at=? WHERE id=?`, nowUTC(), nowUTC(), app.ID)
	d.p.audit("deploy.success", "app", fmt.Sprint(app.ID), "Deploy "+app.Name+" berhasil (rilis #"+strconv.FormatInt(newReleaseID, 10)+")")
	d.p.notify("deploy", "Deploy berhasil: "+app.Name, "Rilis #"+strconv.FormatInt(newReleaseID, 10)+" aktif dan health check lulus.", &app.ID)
	d.p.publish("deploy.end", map[string]any{"deploymentId": deploymentID, "appId": app.ID, "status": "berhasil"})
	log("Selesai. Rollback tersedia dari tab Deploy & Rilis.")
	lf.Close()
	_ = imageTag
	go d.p.cleanupReleases(app.ID)
}

func (d *deployManager) build(ctx context.Context, app *App, log func(string, ...any)) (int64, string, string, error) {
	now := time.Now()
	dir := ""
	commit := ""
	imageTag := ""
	var releaseID int64

	if app.SourceType != "image" {
		base := filepath.Join(app.Workdir, "releases")
		if err := os.MkdirAll(base, 0o750); err != nil {
			return 0, "", "", err
		}
		tmp := filepath.Join(base, fmt.Sprintf("%d", now.Unix()))
		_ = os.RemoveAll(tmp)
		log("Mengambil kode dari %s (branch %s)…", maskRepo(app.RepoURL), app.Branch)
		if err := d.gitClone(ctx, app, tmp, log); err != nil {
			return 0, "", "", err
		}
		commit = gitCommit(ctx, tmp)
		short := commit
		if len(short) > 7 {
			short = short[:7]
		}
		if short == "" {
			short = "local"
		}
		final := filepath.Join(base, fmt.Sprintf("%d-%s", now.Unix(), short))
		_ = os.RemoveAll(final)
		if err := os.Rename(tmp, final); err != nil {
			return 0, "", "", err
		}
		dir = final
		log("Commit: %s", short)
	} else {
		log("Sumber image: %s", app.Image)
	}

	res, err := d.p.db.Exec(`INSERT INTO releases(app_id,commit_hash,dir_path,image_tag,built_at,status) VALUES(?,?,?,?,?,?)`,
		app.ID, commit, dir, "", nowUTC(), "membangun")
	if err != nil {
		return 0, "", "", err
	}
	releaseID, _ = res.LastInsertId()
	imageTag = fmt.Sprintf("sp-%s:rel-%d", app.Slug, releaseID)

	if app.Mode == "docker" || app.Mode == "compose" {
		env, err := d.p.appEnv(app, true)
		if err != nil {
			return releaseID, dir, "", err
		}
		if dir != "" {
			if err := writeEnvFile(filepath.Join(dir, ".env"), env); err != nil {
				return releaseID, dir, "", err
			}
		}
		if app.Mode == "docker" {
			if app.SourceType == "image" {
				log("Menarik image %s…", app.Image)
				if err := d.execStream(ctx, fmt.Sprintf("docker pull %s", shellQuote(app.Image)), dir, env, log); err != nil {
					return releaseID, dir, "", err
				}
				imageTag = app.Image
			} else {
				buildCmd := app.BuildCmd
				if buildCmd == "" {
					buildCmd = "docker build -t " + shellQuote(imageTag) + " ."
				}
				log("Membangun image Docker…")
				if err := d.execStream(ctx, buildCmd, dir, env, log); err != nil {
					return releaseID, dir, "", fmt.Errorf("docker build: %w", err)
				}
			}
		} else {
			log("Menyiapkan Compose project…")
			if err := d.execStream(ctx, "docker compose -p "+shellQuote("sp-"+app.Slug)+" up -d --build", dir, env, log); err != nil {
				return releaseID, dir, "", fmt.Errorf("docker compose: %w", err)
			}
			imageTag = "compose:sp-" + app.Slug
		}
		_, _ = d.p.db.Exec(`UPDATE releases SET image_tag=? WHERE id=?`, imageTag, releaseID)
		return releaseID, dir, imageTag, nil
	}

	env, err := d.p.appEnv(app, true)
	if err != nil {
		return releaseID, dir, "", err
	}
	if err := writeEnvFile(filepath.Join(dir, ".env"), env); err != nil {
		return releaseID, dir, "", err
	}
	if app.InstallCmd != "" {
		log("$ %s", app.InstallCmd)
		if err := d.execStream(ctx, app.InstallCmd, dir, env, log); err != nil {
			return releaseID, dir, "", fmt.Errorf("install: %w", err)
		}
	}
	if app.BuildCmd != "" {
		log("$ %s", app.BuildCmd)
		if err := d.execStream(ctx, app.BuildCmd, dir, env, log); err != nil {
			return releaseID, dir, "", fmt.Errorf("build: %w", err)
		}
	}
	_, _ = d.p.db.Exec(`UPDATE releases SET image_tag=? WHERE id=?`, imageTag, releaseID)
	return releaseID, dir, imageTag, nil
}

func (d *deployManager) gitClone(ctx context.Context, app *App, dest string, log func(string, ...any)) error {
	url := normalizeRepoURL(app.RepoURL)
	useToken := app.SourceType == "github" && strings.Contains(url, "github.com") && !strings.HasPrefix(url, "git@")
	token := ""
	pushURL := url
	if useToken {
		token = d.p.githubToken()
		if token != "" {
			pushURL = strings.Replace(url, "https://", "https://x-access-token:"+token+"@", 1)
		}
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", app.Branch, pushURL, dest)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	setProcAttr(cmd)
	out, err := cmd.CombinedOutput()
	text := maskSecrets(string(out), []string{token})
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.TrimSpace(line) != "" {
			log("  " + line)
		}
	}
	if err != nil {
		_ = os.RemoveAll(dest)
		return fmt.Errorf("git clone gagal: %s", truncate(strings.TrimSpace(text), 400))
	}
	if token != "" {
		_ = exec.CommandContext(ctx, "git", "-C", dest, "remote", "set-url", "origin", url).Run()
	}
	return nil
}

func (d *deployManager) execStream(ctx context.Context, command, dir string, env map[string]string, log func(string, ...any)) error {
	cmd := shellCommand(ctx, command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), envSlice(env)...)
	setProcAttr(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 32<<10), 512<<10)
	secrets := []string{}
	for _, v := range env {
		secrets = append(secrets, v)
	}
	for sc.Scan() {
		line := maskSecrets(sc.Text(), secrets)
		log("  " + line)
	}
	if err := cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func lastCommit(repo, dir string, p *Panel) string {
	if dir == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func normalizeRepoURL(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "git@") || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		if strings.HasPrefix(s, "https://github.com/") && !strings.HasSuffix(s, ".git") {
			return s + ".git"
		}
		return s
	}
	parts := strings.Split(s, "/")
	if len(parts) == 2 {
		return "https://github.com/" + s + ".git"
	}
	return s
}

func maskRepo(s string) string {
	if strings.Contains(s, "@") && strings.Contains(s, "://") {
		parts := strings.SplitN(s, "@", 2)
		if len(parts) == 2 {
			return parts[0] + "@…"
		}
	}
	return s
}

func (p *Panel) secretValues(app *App) []string {
	vals := []string{}
	rows, err := p.db.Query(`SELECT value,is_secret FROM app_env WHERE app_id=?`, app.ID)
	if err == nil {
		for rows.Next() {
			var v string
			var secret int
			if rows.Scan(&v, &secret) == nil && secret == 1 {
				d := p.decrypt(v)
				if len(d) >= 4 {
					vals = append(vals, d)
				}
			}
		}
		rows.Close()
	}
	if tok := p.githubToken(); len(tok) > 8 {
		vals = append(vals, tok)
	}
	return vals
}

func maskSecrets(s string, secrets []string) string {
	for _, secret := range secrets {
		if len(secret) >= 4 {
			s = strings.ReplaceAll(s, secret, "••••••••")
		}
	}
	return s
}

func gitCommit(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (p *Panel) handleDeploy(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	trigger := r.URL.Query().Get("trigger")
	if trigger == "" {
		trigger = "manual"
	}
	dep, err := p.deployMgr.enqueue(id, trigger)
	if err != nil {
		failDetail(w, http.StatusConflict, "Deploy tidak dapat dimulai", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"deployment": dep})
}

func (p *Panel) handleDeployments(w http.ResponseWriter, r *http.Request) {
	appID := int64(qInt(r, "appId", 0))
	list, err := p.listDeployments(appID, qInt(r, "limit", 50))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": list})
}

func (p *Panel) handleDeployment(w http.ResponseWriter, r *http.Request) {
	d, err := p.getDeployment(pathID(r))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployment": d})
}

func (p *Panel) handleDeployCancel(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if p.deployMgr.cancel(id) {
		p.audit("deploy.cancel", "deployment", fmt.Sprint(id), "Deploy dibatalkan")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	fail(w, http.StatusConflict, "Deploy tidak sedang berjalan atau sudah selesai")
}

func (p *Panel) handleRollback(w http.ResponseWriter, r *http.Request) {
	appID := pathID(r)
	relID, _ := strconv.ParseInt(r.PathValue("release"), 10, 64)
	app, err := p.getApp(appID)
	if err != nil {
		httpError(w, err)
		return
	}
	var rel Release
	err = p.db.QueryRow(`SELECT id,app_id,commit_hash,dir_path,image_tag,built_at,status FROM releases WHERE id=? AND app_id=?`, relID, appID).
		Scan(&rel.ID, &rel.AppID, &rel.CommitHash, &rel.DirPath, &rel.ImageTag, &rel.BuiltAt, &rel.Status)
	if err != nil {
		fail(w, http.StatusNotFound, "Rilis tidak ditemukan")
		return
	}
	prev := app.ActiveReleaseID
	logPath := filepath.Join(p.logsDir(), "deploy", fmt.Sprintf("%d-rollback-%d.log", appID, time.Now().Unix()))
	res, _ := p.db.Exec(`INSERT INTO deployments(app_id,release_id,trigger,status,commit_hash,log_path,created_at,started_at) VALUES(?,?,?,?,?,?,?,?)`,
		appID, relID, "rollback", "berjalan", rel.CommitHash, logPath, nowUTC(), nowUTC())
	depID, _ := res.LastInsertId()
	logf, _ := os.Create(logPath)
	log := func(format string, args ...any) {
		if logf != nil {
			_, _ = logf.WriteString(time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...) + "\n")
			_ = logf.Sync()
		}
	}
	log("Rollback manual ke rilis #%d (%s)…", relID, rel.CommitHash)
	_, _ = p.db.Exec(`UPDATE releases SET status='arsip' WHERE app_id=?`, appID)
	_, _ = p.db.Exec(`UPDATE releases SET status='aktif' WHERE id=?`, relID)
	_, _ = p.db.Exec(`UPDATE apps SET active_release_id=?, needs_restart=0, updated_at=? WHERE id=?`, relID, nowUTC(), appID)
	fresh, _ := p.getApp(appID)
	if fresh != nil && rel.DirPath != "" {
		fresh.ActiveReleaseDir = rel.DirPath
	}
	var opErr error
	if fresh.Mode == "docker" || fresh.Mode == "compose" {
		opErr = p.procMgr.startApp(fresh)
	} else {
		opErr = p.procMgr.restartApp(fresh)
	}
	ok := false
	if opErr == nil {
		for i := 0; i < 20; i++ {
			time.Sleep(600 * time.Millisecond)
			if p.healthOK(fresh.Port, fresh.HealthPath) {
				ok = true
				break
			}
		}
	}
	if opErr != nil || !ok {
		if opErr == nil {
			opErr = fmt.Errorf("health check gagal")
		}
		log("Rollback gagal: %v", opErr)
		_, _ = p.db.Exec(`UPDATE deployments SET status='gagal', error=?, finished_at=? WHERE id=?`, opErr.Error(), nowUTC(), depID)
		if prev != nil {
			_, _ = p.db.Exec(`UPDATE releases SET status='aktif' WHERE id=?`, *prev)
			_, _ = p.db.Exec(`UPDATE apps SET active_release_id=? WHERE id=?`, *prev, appID)
			if old, err := p.getApp(appID); err == nil {
				_ = p.procMgr.startApp(old)
			}
		}
		p.notify("deploy", "Rollback gagal: "+app.Name, "Rilis #"+strconv.FormatInt(relID, 10)+" tidak lulus health check; rilis lama dipertahankan.", &appID)
		if logf != nil {
			logf.Close()
		}
		failDetail(w, http.StatusConflict, "Rollback gagal", opErr.Error())
		return
	}
	log("Rollback berhasil; aplikasi melayani rilis #%d.", relID)
	_, _ = p.db.Exec(`UPDATE deployments SET status='berhasil', finished_at=? WHERE id=?`, nowUTC(), depID)
	_, _ = p.db.Exec(`UPDATE apps SET status='berjalan', health_status='sehat', health_checked_at=?, updated_at=? WHERE id=?`, nowUTC(), nowUTC(), appID)
	p.audit("deploy.rollback", "app", fmt.Sprint(appID), "Rollback "+app.Name+" ke rilis #"+strconv.FormatInt(relID, 10))
	p.notify("deploy", "Rollback berhasil: "+app.Name, "Aplikasi kembali ke rilis #"+strconv.FormatInt(relID, 10)+".", &appID)
	if logf != nil {
		logf.Close()
	}
	fresh, _ = p.getApp(appID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "app": fresh, "deploymentId": depID})
}

func (p *Panel) cleanupReleases(appID int64) {
	keep := p.intSetting("release_keep", 5)
	if keep < 1 {
		keep = 1
	}
	rows, err := p.db.Query(`SELECT id,dir_path FROM releases WHERE app_id=? AND status IN ('aktif','arsip') ORDER BY id DESC`, appID)
	if err != nil {
		return
	}
	var ids []int64
	var dirs []string
	for rows.Next() {
		var id int64
		var dir string
		if rows.Scan(&id, &dir) == nil {
			ids = append(ids, id)
			dirs = append(dirs, dir)
		}
	}
	rows.Close()
	var active int64
	_ = p.db.QueryRow(`SELECT COALESCE(active_release_id,0) FROM apps WHERE id=?`, appID).Scan(&active)
	kept := 0
	removed := 0
	for i, id := range ids {
		if id == active {
			continue
		}
		kept++
		if kept <= keep {
			continue
		}
		if dirs[i] != "" && strings.HasPrefix(dirs[i], p.appsDir()) {
			_ = os.RemoveAll(dirs[i])
		}
		_, _ = p.db.Exec(`DELETE FROM releases WHERE id=?`, id)
		removed++
	}
	if removed > 0 {
		p.audit("app.release_cleanup", "app", fmt.Sprint(appID), fmt.Sprintf("%d rilis lama dibersihkan (batas simpan %d)", removed, keep))
	}
}

func (p *Panel) handleReleases(w http.ResponseWriter, r *http.Request) {
	rels, err := p.listReleases(pathID(r))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": rels})
}
