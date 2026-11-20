package panel

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Addr    string
	DataDir string
	BaseURL string
	Version string
	Demo    bool
}

type Panel struct {
	cfg      Config
	db       *sql.DB
	key      []byte
	webFS    fs.FS
	webRoot  string
	mux      *http.ServeMux
	started  time.Time
	version  string
	broker   *broker
	loginMu  sync.Mutex
	loginMap map[string]*loginState

	deployMgr *deployManager
	procMgr   *processManager
	metrics   *metricStore
	sched     *scheduler

	redisMu sync.Mutex

	alertMu sync.Mutex
	alertAt map[string]time.Time

	backupMu sync.Mutex

	termMu sync.Mutex
	terms  map[string]time.Time

	driveMu sync.Mutex
}

type loginState struct {
	Fails    int
	Blocked  time.Time
	LastFail time.Time
}

func New(cfg Config, assets fs.FS, root string) (*Panel, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	abs, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	cfg.DataDir = abs
	sub, err := fs.Sub(assets, root)
	if err != nil {
		return nil, fmt.Errorf("aset web: %w", err)
	}
	p := &Panel{
		cfg:      cfg,
		webFS:    sub,
		webRoot:  root,
		mux:      http.NewServeMux(),
		started:  time.Now(),
		version:  cfg.Version,
		broker:   newBroker(),
		loginMap: map[string]*loginState{},
		alertAt:  map[string]time.Time{},
		terms:    map[string]time.Time{},
	}
	return p, nil
}

func (p *Panel) DataDir() string { return p.cfg.DataDir }

func (p *Panel) Init(ctx context.Context) error {
	for _, d := range []string{
		p.cfg.DataDir,
		p.appsDir(),
		p.logsDir(),
		filepath.Join(p.logsDir(), "deploy"),
		filepath.Join(p.logsDir(), "apps"),
		filepath.Join(p.cfg.DataDir, "backups"),
		filepath.Join(p.cfg.DataDir, "backups", "db"),
		filepath.Join(p.cfg.DataDir, "runtime"),
		filepath.Join(p.cfg.DataDir, "uploads"),
		filepath.Join(p.cfg.DataDir, "tmp"),
	} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("membuat folder %s: %w", d, err)
		}
	}
	if err := p.openDB(); err != nil {
		return err
	}
	if err := p.loadKey(); err != nil {
		return err
	}
	p.defaults()
	p.recoverInterrupted()
	p.procMgr = newProcessManager(p)
	p.deployMgr = newDeployManager(p)
	p.metrics = newMetricStore(p)
	p.sched = newScheduler(p)
	p.sched.seedBuiltins()
	if p.cfg.Demo {
		if err := p.seedDemo(); err != nil {
			log.Printf("data contoh: %v", err)
		}
	}
	p.registerRoutes()
	return nil
}

func (p *Panel) Run(ctx context.Context) error {
	p.deployMgr.start(ctx)
	p.procMgr.start(ctx)
	p.metrics.start(ctx)
	p.sched.start(ctx)
	p.startDemoApps()
	go p.verifyRunningApps(ctx)

	srv := &http.Server{
		Addr:              p.cfg.Addr,
		Handler:           p.middleware(p.mux),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("melayani panel di http://%s", p.cfg.Addr)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		p.cleanup()
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (p *Panel) cleanup() {
	p.sched.stop()
	p.deployMgr.stop()
	p.procMgr.shutdown()
	if p.db != nil {
		_, _ = p.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, nowUTC())
		_ = p.db.Close()
	}
}

func (p *Panel) recoverInterrupted() {
	now := nowUTC()
	_, _ = p.db.Exec(`UPDATE deployments SET status='gagal', error='Panel berhenti saat deploy berjalan', finished_at=? WHERE status IN ('antre','berjalan')`, now)
	_, _ = p.db.Exec(`UPDATE apps SET status='berhenti' WHERE status IN ('deploy','bermasalah')`)
}

func (p *Panel) verifyRunningApps(ctx context.Context) {
	time.Sleep(1500 * time.Millisecond)
	rows, err := p.db.Query(`SELECT id, name, port, status FROM apps WHERE status='berjalan'`)
	if err != nil {
		return
	}
	type item struct {
		id   int64
		name string
		port int
	}
	var items []item
	for rows.Next() {
		var it item
		var st string
		if rows.Scan(&it.id, &it.name, &it.port, &st) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	for _, it := range items {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !p.healthOK(it.port, "/") {
			if p.procMgr == nil || !p.procMgr.isRunning(it.id) {
				_, _ = p.db.Exec(`UPDATE apps SET status='berhenti', health_status='gagal', health_checked_at=? WHERE id=?`, nowUTC(), it.id)
				p.audit("app.state", "app", fmt.Sprint(it.id), "Aplikasi "+it.name+" ditandai berhenti setelah panel restart (health check gagal)")
			}
		}
	}
}

func (p *Panel) appsDir() string               { return filepath.Join(p.cfg.DataDir, "apps") }
func (p *Panel) logsDir() string               { return filepath.Join(p.cfg.DataDir, "logs") }
func (p *Panel) backupDir() string             { return filepath.Join(p.cfg.DataDir, "backups") }
func (p *Panel) appWorkdir(slug string) string { return filepath.Join(p.appsDir(), slug) }

func (p *Panel) baseURL(r *http.Request) string {
	if v := p.setting("base_url", ""); v != "" {
		return strings.TrimRight(v, "/")
	}
	scheme := "http"
	if r != nil {
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		if host := r.Host; host != "" && !strings.HasPrefix(host, "127.0.0.1") && !strings.HasPrefix(host, "localhost") {
			return scheme + "://" + host
		}
	}
	host := p.setting("panel_hostname", "panel.satriyadi.com")
	return "https://" + host
}

func (p *Panel) panelURL(r *http.Request) string {
	host := p.setting("panel_hostname", "panel.satriyadi.com")
	return "https://" + host
}

// ---------- broker SSE ----------

type brokerEvent struct {
	Kind string `json:"kind"`
	Data any    `json:"data"`
}

type broker struct {
	mu   sync.Mutex
	subs map[chan brokerEvent]struct{}
}

func newBroker() *broker {
	return &broker{subs: map[chan brokerEvent]struct{}{}}
}

func (b *broker) subscribe() chan brokerEvent {
	ch := make(chan brokerEvent, 32)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *broker) unsubscribe(ch chan brokerEvent) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *broker) publish(ev brokerEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (p *Panel) publish(kind string, data any) {
	p.broker.publish(brokerEvent{Kind: kind, Data: data})
}

func (p *Panel) logf(format string, args ...any) {
	log.Printf(format, args...)
}
