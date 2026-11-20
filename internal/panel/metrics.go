package panel

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

type ServerInfo struct {
	Hostname    string  `json:"hostname"`
	OS          string  `json:"os"`
	Platform    string  `json:"platform"`
	Kernel      string  `json:"kernel"`
	Arch        string  `json:"arch"`
	CPUModel    string  `json:"cpuModel"`
	CPUCores    int     `json:"cpuCores"`
	CPUPercent  float64 `json:"cpuPercent"`
	MemTotal    uint64  `json:"memTotal"`
	MemUsed     uint64  `json:"memUsed"`
	MemPercent  float64 `json:"memPercent"`
	SwapTotal   uint64  `json:"swapTotal"`
	SwapUsed    uint64  `json:"swapUsed"`
	DiskTotal   uint64  `json:"diskTotal"`
	DiskUsed    uint64  `json:"diskUsed"`
	DiskPercent float64 `json:"diskPercent"`
	DiskPath    string  `json:"diskPath"`
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	UptimeSec   uint64  `json:"uptimeSec"`
	BootTime    uint64  `json:"bootTime"`
	GoVersion   string  `json:"goVersion"`
	Timezone    string  `json:"timezone"`
	PanelUptime int64   `json:"panelUptimeSec"`
	PanelRAM    uint64  `json:"panelRamBytes"`
	ServerName  string  `json:"serverName"`
}

type MetricPoint struct {
	At       string  `json:"at"`
	CPU      float64 `json:"cpu"`
	RAM      float64 `json:"ram"`
	Disk     float64 `json:"disk"`
	MemBytes uint64  `json:"memBytes"`
}

type metricStore struct {
	p       *Panel
	mu      sync.Mutex
	latest  MetricPoint
	history []MetricPoint
	lastDB  time.Time
}

func newMetricStore(p *Panel) *metricStore {
	return &metricStore{p: p}
}

func (m *metricStore) start(ctx context.Context) {
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		m.collect(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.collect(ctx)
			}
		}
	}()
}

func (m *metricStore) collect(ctx context.Context) {
	info := m.p.collectServerInfo()
	pt := MetricPoint{
		At:       nowUTC(),
		CPU:      round2(info.CPUPercent),
		RAM:      round2(info.MemPercent),
		Disk:     round2(info.DiskPercent),
		MemBytes: info.MemUsed,
	}
	m.mu.Lock()
	m.latest = pt
	m.history = append(m.history, pt)
	if len(m.history) > 720 {
		m.history = m.history[len(m.history)-720:]
	}
	shouldSave := time.Since(m.lastDB) >= 55*time.Second
	if shouldSave {
		m.lastDB = time.Now()
	}
	m.mu.Unlock()

	if shouldSave {
		_, _ = m.p.db.Exec(`INSERT INTO metric_samples(scope,app_id,cpu_pct,mem_bytes,disk_pct,sampled_at) VALUES('server',NULL,?,?,?,?)`,
			pt.CPU, int64(info.MemUsed), pt.Disk, pt.At)
	}
	m.p.checkAlerts(info)
	m.p.publish("metrics", map[string]any{"server": info, "point": pt})
}

func (m *metricStore) snapshot() (MetricPoint, []MetricPoint, []MetricPoint) {
	info := m.p.collectServerInfo()
	m.mu.Lock()
	latest := m.latest
	hist := append([]MetricPoint(nil), m.history...)
	m.mu.Unlock()
	rows, err := m.p.db.Query(`SELECT sampled_at,cpu_pct,mem_bytes,disk_pct FROM metric_samples WHERE scope='server' ORDER BY id DESC LIMIT 288`)
	persisted := []MetricPoint{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var pt MetricPoint
			var memBytes int64
			if rows.Scan(&pt.At, &pt.CPU, &memBytes, &pt.Disk) == nil {
				pt.MemBytes = uint64(memBytes)
				if info.MemTotal > 0 {
					pt.RAM = round2(float64(memBytes) / float64(info.MemTotal) * 100)
				}
				persisted = append(persisted, pt)
			}
		}
	}
	// urutkan naik
	for i, j := 0, len(persisted)-1; i < j; i, j = i+1, j-1 {
		persisted[i], persisted[j] = persisted[j], persisted[i]
	}
	if len(hist) < len(persisted) {
		merged := append([]MetricPoint{}, persisted...)
		merged = append(merged, hist...)
		sort.Slice(merged, func(i, j int) bool { return merged[i].At < merged[j].At })
		seen := map[string]bool{}
		unique := merged[:0]
		for _, pt := range merged {
			if seen[pt.At] {
				continue
			}
			seen[pt.At] = true
			unique = append(unique, pt)
		}
		if len(unique) > 288 {
			unique = unique[len(unique)-288:]
		}
		hist = unique
	}
	return latest, hist, persisted
}

func (p *Panel) collectServerInfo() ServerInfo {
	info := ServerInfo{
		Hostname:    hostnameOr("server"),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		CPUCores:    runtime.NumCPU(),
		GoVersion:   runtime.Version(),
		Timezone:    p.setting("timezone", time.Local.String()),
		PanelUptime: int64(time.Since(p.started).Seconds()),
		ServerName:  p.setting("server_name", "home-server"),
	}
	if hi, err := host.Info(); err == nil {
		info.Hostname = hi.Hostname
		info.Platform = strings.TrimSpace(hi.Platform + " " + hi.PlatformVersion)
		info.Kernel = hi.KernelVersion
		info.UptimeSec = hi.Uptime
		info.BootTime = hi.BootTime
	}
	if cpus, err := cpu.Info(); err == nil && len(cpus) > 0 {
		info.CPUModel = strings.TrimSpace(cpus[0].ModelName)
	}
	if pct, err := cpu.Percent(120*time.Millisecond, false); err == nil && len(pct) > 0 {
		info.CPUPercent = round2(pct[0])
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		info.MemTotal, info.MemUsed, info.MemPercent = vm.Total, vm.Used, round2(vm.UsedPercent)
	}
	if sw, err := mem.SwapMemory(); err == nil {
		info.SwapTotal, info.SwapUsed = sw.Total, sw.Used
	}
	path := "/"
	if runtime.GOOS == "windows" {
		path = os.Getenv("SystemDrive") + `\`
		if path == `\` {
			path = "C:\\"
		}
	}
	if du, err := disk.Usage(path); err == nil {
		info.DiskTotal, info.DiskUsed, info.DiskPercent, info.DiskPath = du.Total, du.Used, round2(du.UsedPercent), path
	}
	if la, err := load.Avg(); err == nil {
		info.Load1, info.Load5, info.Load15 = round2(la.Load1), round2(la.Load5), round2(la.Load15)
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	info.PanelRAM = ms.Alloc
	return info
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func (p *Panel) checkAlerts(info ServerInfo) {
	thresholds := map[string]struct {
		value float64
		label string
	}{
		"disk":  {float64(p.intSetting("alert_disk", 90)), "Penggunaan disk"},
		"ram":   {float64(p.intSetting("alert_ram", 90)), "Penggunaan RAM"},
		"redis": {float64(p.intSetting("alert_redis", 85)), "Penggunaan memori Redis"},
	}
	values := map[string]float64{"disk": info.DiskPercent, "ram": info.MemPercent}
	if p.boolSetting("redis_enabled", false) {
		if v := p.redisMemoryPercent(); v > 0 {
			values["redis"] = v
		}
	}
	for key, th := range thresholds {
		v, ok := values[key]
		if !ok {
			continue
		}
		if v < th.value {
			continue
		}
		p.alertMu.Lock()
		last := p.alertAt[key]
		can := time.Since(last) > 30*time.Minute
		if can {
			p.alertAt[key] = time.Now()
		}
		p.alertMu.Unlock()
		if !can {
			continue
		}
		title := fmt.Sprintf("%s tinggi (%.0f%%)", th.label, v)
		body := fmt.Sprintf("Ambang batas %.0f%% terlampaui pada %s.", th.value, info.Hostname)
		p.notify("alert", title, body, nil)
		p.audit("monitor.alert", "server", "", title)
	}
}

func (p *Panel) handleDashboard(w http.ResponseWriter, r *http.Request) {
	apps, _ := p.listApps()
	running := 0
	unhealthy := 0
	for _, a := range apps {
		if a.Running {
			running++
		}
		if a.HealthStatus == "gagal" {
			unhealthy++
		}
	}
	deps, _ := p.listDeployments(0, 8)
	var backups int
	var lastBackup string
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM backups WHERE status='berhasil'`).Scan(&backups)
	_ = p.db.QueryRow(`SELECT COALESCE(MAX(finished_at),'') FROM backups WHERE status='berhasil'`).Scan(&lastBackup)
	var unread int
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE is_read=0`).Scan(&unread)
	var cronCount int
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM cron_jobs WHERE enabled=1`).Scan(&cronCount)
	latest, hist, _ := p.metrics.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"server":        p.collectServerInfo(),
		"point":         latest,
		"series":        hist,
		"apps":          apps,
		"stats":         map[string]any{"apps": len(apps), "running": running, "unhealthy": unhealthy, "deployments": len(deps), "backups": backups, "lastBackup": lastBackup, "unread": unread, "cron": cronCount},
		"deployments":   deps,
		"notifications": p.recentNotifications(6),
		"redis":         p.redisSummary(),
	})
}

func (p *Panel) handleMetrics(w http.ResponseWriter, r *http.Request) {
	latest, hist, persisted := p.metrics.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"latest": latest, "series": hist, "persisted": persisted})
}

func (p *Panel) handleMonitorStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "Streaming tidak didukung")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := p.broker.subscribe()
	defer p.broker.unsubscribe(ch)
	writeSSE := func(kind string, data any) bool {
		b := jsonBytes(map[string]any{"kind": kind, "data": data})
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !writeSSE("snapshot", map[string]any{"server": p.collectServerInfo(), "apps": p.appsSummary()}) {
		return
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			if ev.Kind == "metrics" || ev.Kind == "app.status" || ev.Kind == "app.health" || ev.Kind == "deploy.start" || ev.Kind == "deploy.end" || ev.Kind == "notification" {
				if !writeSSE(ev.Kind, ev.Data) {
					return
				}
			}
		}
	}
}

func (p *Panel) appsSummary() []map[string]any {
	apps, _ := p.listApps()
	out := make([]map[string]any, 0, len(apps))
	for _, a := range apps {
		out = append(out, map[string]any{"id": a.ID, "name": a.Name, "slug": a.Slug, "status": a.Status, "health": a.HealthStatus, "port": a.Port, "runtime": a.Runtime, "lastDeployState": a.LastDeployState})
	}
	return out
}
