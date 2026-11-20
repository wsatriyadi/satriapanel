package panel

import (
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookie = "sp_session"

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

func (p *Panel) userCount() int {
	var n int
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM admin_users`).Scan(&n)
	return n
}

func (p *Panel) currentUser(r *http.Request) *User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	var u User
	var lastSeen, expires string
	err = p.db.QueryRow(`SELECT u.id,u.username,u.password_hash,u.created_at,u.updated_at,s.last_seen_at,s.expires_at
		FROM sessions s JOIN admin_users u ON u.id=s.user_id WHERE s.token_hash=?`, tokenHash(c.Value)).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt, &lastSeen, &expires)
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	if parseTime(expires).Before(now) {
		_, _ = p.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash(c.Value))
		return nil
	}
	idle := time.Duration(p.intSetting("session_idle_minutes", 120)) * time.Minute
	if idle > 0 && now.Sub(parseTime(lastSeen)) > idle {
		_, _ = p.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash(c.Value))
		return nil
	}
	if now.Sub(parseTime(lastSeen)) > 45*time.Second {
		_, _ = p.db.Exec(`UPDATE sessions SET last_seen_at=? WHERE token_hash=?`, nowUTC(), tokenHash(c.Value))
	}
	return &u
}

func (p *Panel) createSession(w http.ResponseWriter, r *http.Request, u *User) error {
	token := randomToken(32)
	exp := time.Now().UTC().Add(12 * time.Hour)
	_, err := p.db.Exec(`INSERT INTO sessions(user_id,token_hash,created_at,last_seen_at,expires_at,ip,user_agent) VALUES(?,?,?,?,?,?,?)`,
		u.ID, tokenHash(token), nowUTC(), nowUTC(), exp.Format(time.RFC3339), remoteIP(r), truncate(r.UserAgent(), 240))
	if err != nil {
		return err
	}
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   12 * 3600,
	})
	return nil
}

func (p *Panel) clearSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_, _ = p.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (p *Panel) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	if p.userCount() > 0 {
		fail(w, http.StatusConflict, "Panel sudah dikonfigurasi")
		return
	}
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		ServerName string `json:"serverName"`
		BaseURL    string `json:"baseUrl"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Username) > 32 || !validUsername(req.Username) {
		fail(w, http.StatusBadRequest, "Username 3–32 karakter (huruf, angka, titik, garis bawah, strip)")
		return
	}
	if len(req.Password) < 10 {
		fail(w, http.StatusBadRequest, "Password minimal 10 karakter")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		httpError(w, err)
		return
	}
	now := nowUTC()
	res, err := p.db.Exec(`INSERT INTO admin_users(username,password_hash,created_at,updated_at) VALUES(?,?,?,?)`,
		req.Username, string(hash), now, now)
	if err != nil {
		httpError(w, err)
		return
	}
	id, _ := res.LastInsertId()
	u := &User{ID: id, Username: req.Username, CreatedAt: now, UpdatedAt: now}
	if req.ServerName != "" {
		_ = p.setSetting("server_name", strings.TrimSpace(req.ServerName))
	}
	if req.BaseURL != "" {
		_ = p.setSetting("base_url", strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"))
	}
	if p.cfg.Demo {
		if err := p.seedDemo(); err != nil {
			log.Printf("data contoh saat setup: %v", err)
		}
	}
	_ = p.setSetting("setup_done", "true")
	if err := p.createSession(w, r, u); err != nil {
		httpError(w, err)
		return
	}
	p.auditWithIP("auth.setup", "panel", "", "Panel dikonfigurasi pertama kali oleh "+u.Username, remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (p *Panel) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ip := remoteIP(r)
	now := time.Now()
	p.loginMu.Lock()
	st := p.loginMap[ip]
	if st == nil {
		st = &loginState{}
		p.loginMap[ip] = st
	}
	if now.Before(st.Blocked) {
		left := int(st.Blocked.Sub(now).Seconds())
		p.loginMu.Unlock()
		p.auditWithIP("auth.login_failed", "panel", "", "Percobaan masuk saat diblokir: "+truncate(req.Username, 60), ip)
		failDetail(w, http.StatusTooManyRequests, "Terlalu banyak percobaan", strings.TrimSpace("Coba lagi dalam "+itoa(left)+" detik"))
		return
	}
	p.loginMu.Unlock()

	var u User
	err := p.db.QueryRow(`SELECT id,username,password_hash,created_at,updated_at FROM admin_users WHERE username=?`, strings.TrimSpace(req.Username)).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	ok := err == nil && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) == nil
	p.loginMu.Lock()
	st = p.loginMap[ip]
	if st == nil {
		st = &loginState{}
		p.loginMap[ip] = st
	}
	if !ok {
		st.Fails++
		st.LastFail = now
		if st.Fails >= 5 {
			st.Blocked = now.Add(15 * time.Minute)
			st.Fails = 0
		}
		p.loginMu.Unlock()
		p.auditWithIP("auth.login_failed", "panel", "", "Login gagal untuk "+truncate(req.Username, 60), ip)
		time.Sleep(350 * time.Millisecond)
		fail(w, http.StatusUnauthorized, "Username atau password salah")
		return
	}
	st.Fails = 0
	st.Blocked = time.Time{}
	p.loginMu.Unlock()

	if err := p.createSession(w, r, &u); err != nil {
		httpError(w, err)
		return
	}
	p.auditWithIP("auth.login", "panel", "", "Login berhasil: "+u.Username, ip)
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (p *Panel) handleLogout(w http.ResponseWriter, r *http.Request) {
	p.clearSession(w, r)
	p.audit("auth.logout", "panel", "", "Admin keluar dari sesi")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	if u := userFrom(r); u != nil {
		_, _ = p.db.Exec(`DELETE FROM sessions WHERE user_id=?`, u.ID)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	p.audit("auth.logout_all", "panel", "", "Semua sesi panel diakhiri")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var backups int
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM backups WHERE status='berhasil'`).Scan(&backups)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":       u,
		"serverName": p.setting("server_name", "home-server"),
		"timezone":   p.setting("timezone", time.Local.String()),
		"version":    p.version,
		"panelURL":   p.panelURL(r),
		"setupDone":  p.userCount() > 0,
		"hasBackup":  backups > 0,
		"needsSetup": false,
	})
}

func (p *Panel) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Current)) != nil {
		fail(w, http.StatusBadRequest, "Password saat ini salah")
		return
	}
	if len(req.New) < 10 {
		fail(w, http.StatusBadRequest, "Password baru minimal 10 karakter")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.New), 12)
	if err != nil {
		httpError(w, err)
		return
	}
	if _, err := p.db.Exec(`UPDATE admin_users SET password_hash=?, updated_at=? WHERE id=?`, string(hash), nowUTC(), u.ID); err != nil {
		httpError(w, err)
		return
	}
	cur := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		cur = tokenHash(c.Value)
	}
	_, _ = p.db.Exec(`DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, u.ID, cur)
	p.audit("auth.password_changed", "panel", "", "Password Admin diubah; sesi lain diakhiri")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) auditWithIP(action, objType, objID, summary, ip string) {
	_, _ = p.db.Exec(`INSERT INTO audit_logs(actor,action,object_type,object_id,summary,ip,created_at) VALUES('admin',?,?,?,?,?,?)`,
		action, objType, objID, summary, ip, nowUTC())
}

func (p *Panel) listSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := p.db.Query(`SELECT id,created_at,last_seen_at,expires_at,ip,user_agent FROM sessions ORDER BY last_seen_at DESC`)
	if err != nil {
		httpError(w, err)
		return
	}
	defer rows.Close()
	var out []map[string]any
	cur := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		cur = tokenHash(c.Value)
	}
	for rows.Next() {
		var id int64
		var created, seen, expires, ip, ua string
		var th string
		if rows.Scan(&id, &created, &seen, &expires, &ip, &ua) == nil {
			out = append(out, map[string]any{"id": id, "createdAt": created, "lastSeenAt": seen, "expiresAt": expires, "ip": ip, "userAgent": ua})
		}
		_ = th
	}
	_ = cur
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func validUsername(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
