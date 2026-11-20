package panel

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type AuditLog struct {
	ID         int64  `json:"id"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	ObjectType string `json:"objectType"`
	ObjectID   string `json:"objectId"`
	Summary    string `json:"summary"`
	IP         string `json:"ip"`
	CreatedAt  string `json:"createdAt"`
}

func (p *Panel) queryAudit(r *http.Request) ([]AuditLog, int) {
	limit := qInt(r, "limit", 50)
	if limit < 1 || limit > 500 {
		limit = 50
	}
	offset := qInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	where := []string{"1=1"}
	args := []any{}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		where = append(where, "(summary LIKE ? OR action LIKE ? OR object_id LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if a := strings.TrimSpace(r.URL.Query().Get("action")); a != "" {
		where = append(where, "action=?")
		args = append(args, a)
	}
	if actor := strings.TrimSpace(r.URL.Query().Get("actor")); actor != "" {
		where = append(where, "actor=?")
		args = append(args, actor)
	}
	cond := strings.Join(where, " AND ")
	var total int
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE `+cond, args...).Scan(&total)
	rows, err := p.db.Query(`SELECT id,actor,action,object_type,object_id,summary,ip,created_at FROM audit_logs WHERE `+cond+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return []AuditLog{}, total
	}
	defer rows.Close()
	out := []AuditLog{}
	for rows.Next() {
		var a AuditLog
		if rows.Scan(&a.ID, &a.Actor, &a.Action, &a.ObjectType, &a.ObjectID, &a.Summary, &a.IP, &a.CreatedAt) == nil {
			out = append(out, a)
		}
	}
	return out, total
}

func (p *Panel) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	logs, total := p.queryAudit(r)
	if qBool(r, "export") {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="satriapanel-audit.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "waktu", "aktor", "aksi", "objek", "objek_id", "ringkasan", "ip"})
		for _, a := range logs {
			_ = cw.Write([]string{strconv.FormatInt(a.ID, 10), a.CreatedAt, a.Actor, a.Action, a.ObjectType, a.ObjectID, a.Summary, a.IP})
		}
		cw.Flush()
		p.audit("audit.export", "audit", "", "Audit log diekspor ke CSV")
		return
	}
	rows, _ := p.db.Query(`SELECT action,COUNT(*) FROM audit_logs GROUP BY action ORDER BY COUNT(*) DESC LIMIT 30`)
	actions := []map[string]any{}
	if rows != nil {
		for rows.Next() {
			var action string
			var n int
			if rows.Scan(&action, &n) == nil {
				actions = append(actions, map[string]any{"action": action, "count": n})
			}
		}
		rows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "total": total, "actions": actions})
}

func (p *Panel) handleNotifications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit := qInt(r, "limit", 50)
		if limit < 1 || limit > 300 {
			limit = 50
		}
		q := `SELECT id,kind,title,body,app_id,is_read,created_at FROM notifications`
		args := []any{}
		if qBool(r, "unread") {
			q += ` WHERE is_read=0`
		}
		q += ` ORDER BY id DESC LIMIT ?`
		args = append(args, limit)
		rows, err := p.db.Query(q, args...)
		if err != nil {
			httpError(w, err)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, appID sql.NullInt64
			var kind, title, body, created string
			var read int
			if rows.Scan(&id, &kind, &title, &body, &appID, &read, &created) == nil {
				var app any
				if appID.Valid {
					app = appID.Int64
				}
				out = append(out, map[string]any{"id": id.Int64, "kind": kind, "title": title, "body": body, "appId": app, "isRead": read == 1, "createdAt": created})
			}
		}
		var unread int
		_ = p.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE is_read=0`).Scan(&unread)
		writeJSON(w, http.StatusOK, map[string]any{"notifications": out, "unread": unread})
	case http.MethodDelete:
		var in struct {
			Read bool `json:"read"`
			All  bool `json:"all"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if in.All {
			_, _ = p.db.Exec(`DELETE FROM notifications WHERE is_read=1`)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		_, _ = p.db.Exec(`DELETE FROM notifications WHERE is_read=1`)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
	}
}

func (p *Panel) recentNotifications(limit int) []map[string]any {
	rows, err := p.db.Query(`SELECT id,kind,title,body,is_read,created_at FROM notifications ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var kind, title, body, created string
		var read int
		if rows.Scan(&id, &kind, &title, &body, &read, &created) == nil {
			out = append(out, map[string]any{"id": id, "kind": kind, "title": title, "body": body, "isRead": read == 1, "createdAt": created})
		}
	}
	return out
}

func (p *Panel) handleNotificationRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	id := pathID(r)
	if id > 0 {
		_, _ = p.db.Exec(`UPDATE notifications SET is_read=1 WHERE id=?`, id)
	} else {
		_, _ = p.db.Exec(`UPDATE notifications SET is_read=1 WHERE is_read=0`)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleNotificationDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	id := pathID(r)
	if id == 0 {
		fail(w, http.StatusBadRequest, "ID notifikasi tidak valid")
		return
	}
	_, _ = p.db.Exec(`DELETE FROM notifications WHERE id=?`, id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleNotifRules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"rules": p.notifRules()})
		return
	}
	var in map[string]bool
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := p.setSetting("notif_rules", jsonString(in)); err != nil {
		httpError(w, err)
		return
	}
	p.audit("notif.rules", "notification", "", "Aturan notifikasi diperbarui")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": in})
}

func (p *Panel) handleAuditMeta(w http.ResponseWriter, r *http.Request) {
	var first string
	_ = p.db.QueryRow(`SELECT COALESCE(MIN(created_at),'') FROM audit_logs`).Scan(&first)
	writeJSON(w, http.StatusOK, map[string]any{"firstAt": first, "retentionNote": "Audit log tidak dihapus otomatis."})
}

var _ = fmt.Sprintf
