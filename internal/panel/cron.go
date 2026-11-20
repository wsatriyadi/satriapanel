package panel

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type CronJob struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	AppID      *int64 `json:"appId"`
	AppName    string `json:"appName"`
	Command    string `json:"command"`
	Workdir    string `json:"workdir"`
	Schedule   string `json:"schedule"`
	Enabled    bool   `json:"enabled"`
	Timeout    int    `json:"timeoutSeconds"`
	BuiltinKey string `json:"builtinKey"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
	NextRun    string `json:"nextRun"`
	LastStatus string `json:"lastStatus"`
	LastRun    string `json:"lastRun"`
	Running    bool   `json:"running"`
	Human      string `json:"human"`
}

type CronRun struct {
	ID         int64  `json:"id"`
	JobID      int64  `json:"jobId"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exitCode"`
	Output     string `json:"output"`
	DurationMs int64  `json:"durationMs"`
}

type scheduler struct {
	p       *Panel
	mu      sync.Mutex
	c       *cron.Cron
	entries map[int64]cron.EntryID
	running sync.Map
	ctx     context.Context
	started bool
}

func newScheduler(p *Panel) *scheduler {
	return &scheduler{p: p, c: cron.New(), entries: map[int64]cron.EntryID{}}
}

func (s *scheduler) start(ctx context.Context) {
	s.ctx = ctx
	s.seedBuiltins()
	s.reload()
	s.c.Start()
	s.started = true
}

func (s *scheduler) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		_ = s.c.Stop()
	}
}

func (s *scheduler) seedBuiltins() {
	now := nowUTC()
	jobs := []struct {
		key, name, schedule string
	}{
		{"backup", "Backup otomatis", s.p.setting("backup_schedule", "0 2 * * *")},
		{"health_check", "Health check aplikasi", "*/2 * * * *"},
		{"metrics_cleanup", "Pembersihan metrik lama", "0 3 * * *"},
		{"logs_cleanup", "Pembersihan log lama", "30 3 * * *"},
		{"cron_cleanup", "Pembersihan riwayat cron", "0 4 * * *"},
		{"release_cleanup", "Pembersihan rilis lama", "15 4 * * *"},
	}
	for _, j := range jobs {
		_, _ = s.p.db.Exec(`INSERT OR IGNORE INTO cron_jobs(name,kind,command,workdir,schedule_expr,enabled,timeout_seconds,builtin_key,created_at,updated_at)
			VALUES(?,?,?,?,?,1,?,?,?,?)`,
			j.name, "system", "", "", j.schedule, 900, j.key, now, now)
	}
}

func (s *scheduler) reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c == nil {
		s.c = cron.New()
	}
	for _, id := range s.entries {
		s.c.Remove(id)
	}
	s.entries = map[int64]cron.EntryID{}
	rows, err := s.p.db.Query(`SELECT id,schedule_expr,enabled,builtin_key FROM cron_jobs`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var expr string
		var enabled int
		var builtin sql.NullString
		if rows.Scan(&id, &expr, &enabled, &builtin) != nil {
			continue
		}
		if enabled == 0 {
			continue
		}
		if builtin.Valid && builtin.String == "backup" {
			expr = s.p.setting("backup_schedule", "0 2 * * *")
		}
		if _, err := cron.ParseStandard(expr); err != nil {
			s.p.logf("cron %d: jadwal tidak valid: %v", id, err)
			continue
		}
		jobID := id
		entry, err := s.c.AddFunc(expr, func() { s.runJob(jobID, "jadwal") })
		if err == nil {
			s.entries[id] = entry
		}
	}
}

func (s *scheduler) nextRun(id int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok {
		n := s.c.Entry(e).Next
		if !n.IsZero() {
			return n.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func (s *scheduler) runJob(jobID int64, trigger string) (*CronRun, error) {
	if _, loaded := s.running.LoadOrStore(jobID, true); loaded {
		return nil, fmt.Errorf("job masih berjalan (dilewati agar tidak tumpang tindih)")
	}
	defer s.running.Delete(jobID)

	var job CronJob
	var appID sql.NullInt64
	var builtin sql.NullString
	var enabled int
	err := s.p.db.QueryRow(`SELECT id,name,kind,app_id,command,workdir,schedule_expr,enabled,timeout_seconds,COALESCE(builtin_key,''),created_at,updated_at
		FROM cron_jobs WHERE id=?`, jobID).Scan(&job.ID, &job.Name, &job.Kind, &appID, &job.Command, &job.Workdir, &job.Schedule, &enabled, &job.Timeout, &builtin, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if appID.Valid {
		v := appID.Int64
		job.AppID = &v
	}
	job.BuiltinKey = builtin.String
	started := time.Now().UTC()
	res, _ := s.p.db.Exec(`INSERT INTO cron_runs(job_id,started_at,status) VALUES(?,?,?)`, jobID, started.Format(time.RFC3339), "berjalan")
	runID, _ := res.LastInsertId()
	s.p.publish("cron.start", map[string]any{"jobId": jobID, "runId": runID, "name": job.Name, "trigger": trigger})
	s.p.audit("cron.start", "cron", fmt.Sprint(jobID), "Cron "+job.Name+" dimulai ("+trigger+")")

	timeout := time.Duration(job.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()

	output, exitCode, runErr := s.execute(ctx, &job)
	status := "berhasil"
	if runErr != nil {
		status = "gagal"
	}
	if len(output) > 120000 {
		output = output[:120000] + "\n… output dipotong"
	}
	finished := time.Now().UTC()
	var code any
	if exitCode >= 0 {
		code = exitCode
	}
	_, _ = s.p.db.Exec(`UPDATE cron_runs SET finished_at=?,status=?,exit_code=?,output=? WHERE id=?`,
		finished.Format(time.RFC3339), status, code, output, runID)
	s.p.publish("cron.end", map[string]any{"jobId": jobID, "runId": runID, "status": status, "name": job.Name})
	if runErr != nil {
		s.p.audit("cron.failed", "cron", fmt.Sprint(jobID), "Cron "+job.Name+" gagal: "+truncate(runErr.Error(), 200))
		s.p.notify("cron", "Cron gagal: "+job.Name, truncate(output+" "+runErr.Error(), 300), job.AppID)
	} else {
		s.p.audit("cron.success", "cron", fmt.Sprint(jobID), "Cron "+job.Name+" selesai")
	}
	run := &CronRun{ID: runID, JobID: jobID, StartedAt: started.Format(time.RFC3339), FinishedAt: finished.Format(time.RFC3339), Status: status, Output: output, DurationMs: finished.Sub(started).Milliseconds()}
	if exitCode >= 0 {
		run.ExitCode = &exitCode
	}
	if runErr != nil && output == "" {
		run.Output = runErr.Error()
	}
	return run, runErr
}

func (s *scheduler) execute(ctx context.Context, job *CronJob) (string, int, error) {
	if job.BuiltinKey != "" {
		out, err := s.executeBuiltin(ctx, job.BuiltinKey)
		code := 0
		if err != nil {
			code = 1
		}
		return out, code, err
	}
	cmdText := job.Command
	dir := job.Workdir
	if job.AppID != nil {
		app, err := s.p.getApp(*job.AppID)
		if err != nil {
			return "", 1, fmt.Errorf("aplikasi tidak ditemukan")
		}
		dir = p_activeDir(app)
		env, err := s.p.appEnv(app, true)
		if err != nil {
			return "", 1, err
		}
		cmd := shellCommand(ctx, cmdText)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), envSlice(env)...)
		setProcAttr(cmd)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			code = -1
		}
		return string(out), code, err
	}
	cmd := shellCommand(ctx, cmdText)
	if dir != "" {
		cmd.Dir = dir
	}
	setProcAttr(cmd)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		code = -1
	}
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("perintah melewati batas waktu %d detik", job.Timeout)
	}
	return string(out), code, err
}

func (s *scheduler) executeBuiltin(ctx context.Context, key string) (string, error) {
	p := s.p
	switch key {
	case "backup":
		if !p.boolSetting("backup_enabled", false) {
			return "Backup otomatis dinonaktifkan; dilewati.\n", nil
		}
		b, err := p.runBackup(ctx, "terjadwal")
		if err != nil {
			return fmt.Sprintf("Backup gagal: %v\n", err), err
		}
		return fmt.Sprintf("Backup #%d selesai, %s item, ukuran %s.\n", b.ID, b.ManifestSummary, fmtBytes(b.TotalSize)), nil
	case "health_check":
		apps, err := p.listApps()
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, a := range apps {
			if a.Status != "berjalan" && a.Status != "bermasalah" {
				continue
			}
			ok, code, detail := p.healthDetail(a.Port, a.HealthPath)
			status := "sehat"
			if !ok {
				status = "gagal"
			}
			_, _ = p.db.Exec(`UPDATE apps SET health_status=?,health_checked_at=? WHERE id=?`, status, nowUTC(), a.ID)
			fmt.Fprintf(&b, "%s: %s (HTTP %d) %s\n", a.Slug, status, code, detail)
		}
		if b.Len() == 0 {
			b.WriteString("Tidak ada aplikasi berjalan untuk diperiksa.\n")
		}
		return b.String(), nil
	case "metrics_cleanup":
		cut := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
		res, err := p.db.Exec(`DELETE FROM metric_samples WHERE sampled_at<?`, cut)
		n, _ := res.RowsAffected()
		return fmt.Sprintf("%d sampel metrik lama dihapus.\n", n), err
	case "logs_cleanup":
		days := p.intSetting("log_retention_days", 14)
		cut := time.Now().AddDate(0, 0, -days)
		removed := 0
		for _, d := range []string{filepath.Join(p.logsDir(), "deploy"), p.logsDir()} {
			entries, _ := os.ReadDir(d)
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				info, err := e.Info()
				if err == nil && info.ModTime().Before(cut) {
					if os.Remove(filepath.Join(d, e.Name())) == nil {
						removed++
					}
				}
			}
		}
		return fmt.Sprintf("%d berkas log lebih tua dari %d hari dihapus.\n", removed, days), nil
	case "cron_cleanup":
		days := p.intSetting("cron_retention_days", 30)
		cut := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
		res, err := p.db.Exec(`DELETE FROM cron_runs WHERE started_at<?`, cut)
		n, _ := res.RowsAffected()
		return fmt.Sprintf("%d riwayat cron lama dihapus.\n", n), err
	case "release_cleanup":
		apps, err := p.listApps()
		if err != nil {
			return "", err
		}
		for _, a := range apps {
			p.cleanupReleases(a.ID)
		}
		return fmt.Sprintf("Pembersihan rilis selesai untuk %d aplikasi (menyimpan %d rilis terakhir).\n", len(apps), p.intSetting("release_keep", 5)), nil
	}
	return "", fmt.Errorf("builtin tidak dikenal: %s", key)
}

func (p *Panel) listCronJobs() ([]CronJob, error) {
	rows, err := p.db.Query(`SELECT j.id,j.name,j.kind,j.app_id,COALESCE(a.name,''),j.command,j.workdir,j.schedule_expr,j.enabled,j.timeout_seconds,
		COALESCE(j.builtin_key,''),j.created_at,j.updated_at,
		COALESCE((SELECT status FROM cron_runs r WHERE r.job_id=j.id ORDER BY r.id DESC LIMIT 1),''),
		COALESCE((SELECT started_at FROM cron_runs r WHERE r.job_id=j.id ORDER BY r.id DESC LIMIT 1),'')
		FROM cron_jobs j LEFT JOIN apps a ON a.id=j.app_id ORDER BY j.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CronJob{}
	for rows.Next() {
		var j CronJob
		var appID sql.NullInt64
		var enabled int
		if rows.Scan(&j.ID, &j.Name, &j.Kind, &appID, &j.AppName, &j.Command, &j.Workdir, &j.Schedule, &enabled, &j.Timeout, &j.BuiltinKey, &j.CreatedAt, &j.UpdatedAt, &j.LastStatus, &j.LastRun) != nil {
			continue
		}
		if appID.Valid {
			v := appID.Int64
			j.AppID = &v
		}
		j.Enabled = enabled == 1
		j.Human = humanSchedule(j.Schedule)
		if p.sched != nil {
			j.NextRun = p.sched.nextRun(j.ID)
			_, running := p.sched.running.Load(j.ID)
			j.Running = running
		}
		out = append(out, j)
	}
	return out, nil
}

func cronParse(expr string) (cron.Schedule, error) {
	return cron.ParseStandard(expr)
}

func humanSchedule(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return expr
	}
	if expr == "* * * * *" {
		return "Setiap menit"
	}
	if fields[0] == "*/2" && fields[1] == "*" {
		return "Setiap 2 menit"
	}
	if fields[2] == "*" && fields[3] == "*" && fields[4] == "*" {
		if fields[1] == "*" {
			return "Setiap jam pada menit " + fields[0]
		}
		return fmt.Sprintf("Setiap hari pukul %02s:%02s", fields[1], fields[0])
	}
	return expr + " (UTC)"
}

func (p *Panel) handleCron(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jobs, err := p.listCronJobs()
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "timezone": "UTC"})
	case http.MethodPost:
		var in struct {
			Name     string `json:"name"`
			AppID    int64  `json:"appId"`
			Command  string `json:"command"`
			Workdir  string `json:"workdir"`
			Schedule string `json:"schedule"`
			Enabled  bool   `json:"enabled"`
			Timeout  int    `json:"timeoutSeconds"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if strings.TrimSpace(in.Name) == "" {
			fail(w, http.StatusBadRequest, "Nama cron wajib diisi")
			return
		}
		if _, err := cron.ParseStandard(in.Schedule); err != nil {
			fail(w, http.StatusBadRequest, "Jadwal cron tidak valid (format 5 kolom, contoh: 0 2 * * *)")
			return
		}
		if strings.TrimSpace(in.Command) == "" && in.AppID == 0 {
			fail(w, http.StatusBadRequest, "Perintah wajib diisi")
			return
		}
		if in.Timeout <= 0 {
			in.Timeout = 300
		}
		kind := "system"
		if in.AppID > 0 {
			if _, err := p.getApp(in.AppID); err != nil {
				fail(w, http.StatusBadRequest, "Aplikasi tidak ditemukan")
				return
			}
			kind = "app"
		}
		now := nowUTC()
		res, err := p.db.Exec(`INSERT INTO cron_jobs(name,kind,app_id,command,workdir,schedule_expr,enabled,timeout_seconds,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, strings.TrimSpace(in.Name), kind, nullableID(in.AppID), in.Command, in.Workdir, in.Schedule, in.Enabled, in.Timeout, now, now)
		if err != nil {
			httpError(w, err)
			return
		}
		id, _ := res.LastInsertId()
		p.sched.reload()
		p.audit("cron.create", "cron", fmt.Sprint(id), "Cron "+in.Name+" dibuat ("+in.Schedule+")")
		jobs, _ := p.listCronJobs()
		for _, j := range jobs {
			if j.ID == id {
				writeJSON(w, http.StatusCreated, map[string]any{"job": j})
				return
			}
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	default:
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
	}
}

func (p *Panel) handleCronJob(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var job CronJob
	var appID sql.NullInt64
	var enabled int
	var builtin sql.NullString
	err := p.db.QueryRow(`SELECT id,name,kind,app_id,command,workdir,schedule_expr,enabled,timeout_seconds,COALESCE(builtin_key,''),created_at,updated_at FROM cron_jobs WHERE id=?`, id).
		Scan(&job.ID, &job.Name, &job.Kind, &appID, &job.Command, &job.Workdir, &job.Schedule, &enabled, &job.Timeout, &builtin, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		httpError(w, errNotFound)
		return
	}
	if appID.Valid {
		v := appID.Int64
		job.AppID = &v
	}
	job.Enabled = enabled == 1
	job.BuiltinKey = builtin.String
	switch r.Method {
	case http.MethodGet:
		runs := p.listCronRuns(id, qInt(r, "limit", 30))
		writeJSON(w, http.StatusOK, map[string]any{"job": job, "runs": runs})
	case http.MethodPut:
		var in struct {
			Name     string `json:"name"`
			Command  string `json:"command"`
			Schedule string `json:"schedule"`
			Enabled  bool   `json:"enabled"`
			Timeout  int    `json:"timeoutSeconds"`
			Workdir  string `json:"workdir"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if _, err := cron.ParseStandard(in.Schedule); err != nil {
			fail(w, http.StatusBadRequest, "Jadwal cron tidak valid")
			return
		}
		name := in.Name
		if name == "" {
			name = job.Name
		}
		timeout := in.Timeout
		if timeout <= 0 {
			timeout = job.Timeout
		}
		command := in.Command
		if job.BuiltinKey != "" {
			command = job.Command
		}
		if _, err := p.db.Exec(`UPDATE cron_jobs SET name=?,command=?,workdir=?,schedule_expr=?,enabled=?,timeout_seconds=?,updated_at=? WHERE id=?`,
			name, command, in.Workdir, in.Schedule, in.Enabled, timeout, nowUTC(), id); err != nil {
			httpError(w, err)
			return
		}
		if job.BuiltinKey == "backup" {
			_ = p.setSetting("backup_schedule", in.Schedule)
		}
		p.sched.reload()
		p.audit("cron.update", "cron", fmt.Sprint(id), "Cron "+name+" diubah")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case http.MethodDelete:
		if job.BuiltinKey != "" {
			fail(w, http.StatusBadRequest, "Cron bawaan panel tidak dapat dihapus; nonaktifkan saja")
			return
		}
		if _, err := p.db.Exec(`DELETE FROM cron_jobs WHERE id=?`, id); err != nil {
			httpError(w, err)
			return
		}
		p.sched.reload()
		p.audit("cron.delete", "cron", fmt.Sprint(id), "Cron "+job.Name+" dihapus")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
	}
}

func (p *Panel) handleCronRun(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	run, err := p.sched.runJob(id, "manual")
	if err != nil && run == nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run, "error": errString(err)})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (p *Panel) handleCronRuns(w http.ResponseWriter, r *http.Request) {
	id := int64(qInt(r, "jobId", 0))
	runs := p.listCronRuns(id, qInt(r, "limit", 50))
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (p *Panel) listCronRuns(jobID int64, limit int) []CronRun {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := `SELECT id,job_id,started_at,COALESCE(finished_at,''),status,exit_code,output FROM cron_runs`
	args := []any{}
	if jobID > 0 {
		q += ` WHERE job_id=?`
		args = append(args, jobID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := p.db.Query(q, args...)
	if err != nil {
		return []CronRun{}
	}
	defer rows.Close()
	out := []CronRun{}
	for rows.Next() {
		var run CronRun
		var code sql.NullInt64
		if rows.Scan(&run.ID, &run.JobID, &run.StartedAt, &run.FinishedAt, &run.Status, &code, &run.Output) == nil {
			if code.Valid {
				v := int(code.Int64)
				run.ExitCode = &v
			}
			if run.FinishedAt != "" {
				run.DurationMs = parseTime(run.FinishedAt).Sub(parseTime(run.StartedAt)).Milliseconds()
			}
			out = append(out, run)
		}
	}
	return out
}

func (p *Panel) handleCronPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Schedule string `json:"schedule"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	sched, err := cron.ParseStandard(in.Schedule)
	if err != nil {
		fail(w, http.StatusBadRequest, "Jadwal tidak valid")
		return
	}
	next := []string{}
	t := time.Now().UTC()
	for i := 0; i < 5; i++ {
		t = sched.Next(t)
		next = append(next, t.Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, map[string]any{"next": next, "human": humanSchedule(in.Schedule)})
}

func (p *Panel) handleCronMeta(w http.ResponseWriter, r *http.Request) {
	apps, _ := p.listApps()
	presets := []map[string]string{
		{"label": "Setiap menit", "value": "* * * * *"},
		{"label": "Setiap 5 menit", "value": "*/5 * * * *"},
		{"label": "Setiap jam", "value": "0 * * * *"},
		{"label": "Setiap hari 02:00", "value": "0 2 * * *"},
		{"label": "Setiap hari 03:30", "value": "30 3 * * *"},
		{"label": "Setiap Senin 04:00", "value": "0 4 * * 1"},
		{"label": "Tanggal 1 tiap bulan", "value": "0 5 1 * *"},
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps, "presets": presets, "timezone": "UTC"})
}
