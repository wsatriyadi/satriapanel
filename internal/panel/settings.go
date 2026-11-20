package panel

import (
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func (p *Panel) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{
			"general": map[string]any{
				"serverName":    p.setting("server_name", "home-server"),
				"timezone":      p.setting("timezone", time.Local.String()),
				"baseUrl":       p.setting("base_url", ""),
				"panelHostname": p.setting("panel_hostname", "panel.satriyadi.com"),
				"version":       p.version, "goVersion": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
			},
			"deploy": map[string]any{
				"portPoolStart": p.intSetting("port_pool_start", 3100), "portPoolEnd": p.intSetting("port_pool_end", 3999),
				"panelPort": p.intSetting("panel_port", 8282), "releaseKeep": p.intSetting("release_keep", 5),
				"logRetentionDays": p.intSetting("log_retention_days", 14), "cronRetentionDays": p.intSetting("cron_retention_days", 30),
			},
			"security": map[string]any{
				"sessionIdleMinutes": p.intSetting("session_idle_minutes", 120),
			},
			"monitor": map[string]any{
				"alertDisk": p.intSetting("alert_disk", 90), "alertRam": p.intSetting("alert_ram", 90), "alertRedis": p.intSetting("alert_redis", 85),
			},
			"database": map[string]any{"allowProvision": p.boolSetting("db_allow_provision", false)},
			"notif":    p.notifRules(),
		})
		return
	}
	var in struct {
		ServerName        string  `json:"serverName"`
		Timezone          string  `json:"timezone"`
		BaseURL           *string `json:"baseUrl"`
		PanelHostname     string  `json:"panelHostname"`
		PortPoolStart     int     `json:"portPoolStart"`
		PortPoolEnd       int     `json:"portPoolEnd"`
		PanelPort         int     `json:"panelPort"`
		ReleaseKeep       int     `json:"releaseKeep"`
		LogRetentionDays  int     `json:"logRetentionDays"`
		CronRetentionDays int     `json:"cronRetentionDays"`
		SessionIdle       int     `json:"sessionIdleMinutes"`
		AlertDisk         int     `json:"alertDisk"`
		AlertRam          int     `json:"alertRam"`
		AlertRedis        int     `json:"alertRedis"`
		AllowProvision    *bool   `json:"allowProvision"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.PortPoolStart > 0 && in.PortPoolEnd > 0 {
		if in.PortPoolStart >= in.PortPoolEnd || in.PortPoolStart < 1024 || in.PortPoolEnd > 65535 {
			fail(w, http.StatusBadRequest, "Rentang kolam port tidak valid")
			return
		}
		if in.PanelPort >= in.PortPoolStart && in.PanelPort <= in.PortPoolEnd {
			fail(w, http.StatusBadRequest, "Port panel tidak boleh berada di dalam kolam aplikasi")
			return
		}
		_ = p.setSetting("port_pool_start", strconv.Itoa(in.PortPoolStart))
		_ = p.setSetting("port_pool_end", strconv.Itoa(in.PortPoolEnd))
	}
	if in.Timezone != "" {
		loc, err := time.LoadLocation(in.Timezone)
		if err != nil {
			fail(w, http.StatusBadRequest, "Zona waktu tidak valid (contoh: Asia/Jakarta)")
			return
		}
		_ = p.setSetting("timezone", loc.String())
	}
	if in.PanelHostname != "" && !validHostname(in.PanelHostname) {
		fail(w, http.StatusBadRequest, "Hostname panel tidak valid")
		return
	}
	set := func(key, value string) { _ = p.setSetting(key, value) }
	if strings.TrimSpace(in.ServerName) != "" {
		set("server_name", strings.TrimSpace(in.ServerName))
	}
	if in.PanelHostname != "" {
		set("panel_hostname", in.PanelHostname)
	}
	if in.BaseURL != nil {
		set("base_url", strings.TrimRight(strings.TrimSpace(*in.BaseURL), "/"))
	}
	if in.PanelPort > 0 {
		set("panel_port", strconv.Itoa(in.PanelPort))
	}
	if in.ReleaseKeep > 0 {
		set("release_keep", strconv.Itoa(in.ReleaseKeep))
	}
	if in.LogRetentionDays > 0 {
		set("log_retention_days", strconv.Itoa(in.LogRetentionDays))
	}
	if in.CronRetentionDays > 0 {
		set("cron_retention_days", strconv.Itoa(in.CronRetentionDays))
	}
	if in.SessionIdle > 0 {
		set("session_idle_minutes", strconv.Itoa(in.SessionIdle))
	}
	if in.AlertDisk > 0 {
		set("alert_disk", strconv.Itoa(in.AlertDisk))
	}
	if in.AlertRam > 0 {
		set("alert_ram", strconv.Itoa(in.AlertRam))
	}
	if in.AlertRedis > 0 {
		set("alert_redis", strconv.Itoa(in.AlertRedis))
	}
	if in.AllowProvision != nil {
		set("db_allow_provision", fmt.Sprint(*in.AllowProvision))
	}
	p.audit("settings.update", "settings", "", "Pengaturan panel diperbarui")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		p.listSessions(w, r)
		return
	}
	fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
}

func (p *Panel) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	id := pathID(r)
	var userID int64
	_ = p.db.QueryRow(`SELECT user_id FROM sessions WHERE id=?`, id).Scan(&userID)
	_, _ = p.db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	p.audit("auth.session_revoke", "session", fmt.Sprint(id), "Sesi panel dicabut")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleTZList(w http.ResponseWriter, r *http.Request) {
	zones := []string{"UTC", "Asia/Jakarta", "Asia/Makassar", "Asia/Jayapura", "Asia/Singapore", "Asia/Tokyo", "Europe/London", "America/New_York"}
	writeJSON(w, http.StatusOK, map[string]any{"zones": zones, "current": p.setting("timezone", time.Local.String())})
}
