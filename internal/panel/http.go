package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type apiErr struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiErr{Error: msg})
}

func failDetail(w http.ResponseWriter, status int, msg, detail string) {
	writeJSON(w, status, apiErr{Error: msg, Detail: detail})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		failDetail(w, http.StatusBadRequest, "Data tidak valid", err.Error())
		return false
	}
	return true
}

func qInt(r *http.Request, key string, def int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	return v
}

func qBool(r *http.Request, key string) bool {
	v := r.URL.Query().Get(key)
	return v == "1" || v == "true"
}

func pathID(r *http.Request) int64 {
	v, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return v
}

func remoteIP(r *http.Request) string {
	if v := r.Header.Get("CF-Connecting-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		parts := strings.Split(v, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (p *Panel) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic %s %s: %v", r.Method, r.URL.Path, rec)
				fail(w, http.StatusInternalServerError, "Terjadi kesalahan internal panel")
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", "max-age=15552000; includeSubDomains")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}

		path := r.URL.Path
		if strings.HasPrefix(path, "/api/") {
			if p.isPublicAPI(r) {
				next.ServeHTTP(w, r)
				return
			}
			u := p.currentUser(r)
			if u == nil {
				fail(w, http.StatusUnauthorized, "Sesi tidak sah atau sudah berakhir")
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
				if r.Header.Get("X-Satria") != "1" {
					fail(w, http.StatusForbidden, "Permintaan ditolak (pelindung CSRF)")
					return
				}
			}
			r = r.WithContext(withUser(r.Context(), u))
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *Panel) isPublicAPI(r *http.Request) bool {
	path := r.URL.Path
	switch {
	case path == "/api/health", path == "/api/setup", path == "/api/auth/login":
		return true
	case strings.HasPrefix(path, "/api/webhooks/github/"):
		return true
	case strings.HasPrefix(path, "/api/integrations/google/callback"):
		return true
	}
	return false
}

func (p *Panel) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	// hash-router SPA: semua rute non-aset memakai index
	if path != "index.html" && !strings.HasPrefix(path, "assets/") && !strings.HasPrefix(path, "vendor/") &&
		path != "favicon.svg" && path != "manifest.webmanifest" {
		path = "index.html"
	}
	if strings.Contains(path, "..") {
		http.NotFound(w, r)
		return
	}
	f, err := p.webFS.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		http.NotFound(w, r)
		return
	}
	data, err := io.ReadAll(f)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(path, "assets/") || strings.HasPrefix(path, "vendor/") {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, path, stat.ModTime(), bytes.NewReader(data))
}

func (p *Panel) handleHealth(w http.ResponseWriter, r *http.Request) {
	var n int
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM admin_users`).Scan(&n)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"version":    p.version,
		"needsSetup": n == 0,
		"serverName": p.setting("server_name", "home-server"),
		"time":       nowUTC(),
		"timezone":   p.setting("timezone", time.Local.String()),
		"uptimeSec":  int(time.Since(p.started).Seconds()),
	})
}

func (p *Panel) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	info := p.collectServerInfo()
	writeJSON(w, http.StatusOK, info)
}

type userCtxKey struct{}

func withUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userCtxKey{}, u)
}

func userFrom(r *http.Request) *User {
	u, _ := r.Context().Value(userCtxKey{}).(*User)
	return u
}

var errNotFound = errors.New("tidak ditemukan")

func httpError(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotFound) {
		fail(w, http.StatusNotFound, "Data tidak ditemukan")
		return
	}
	failDetail(w, http.StatusInternalServerError, "Operasi gagal", err.Error())
}

func isAjax(r *http.Request) bool { return r.Header.Get("X-Satria") == "1" }

func fmtBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
