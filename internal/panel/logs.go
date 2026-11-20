package panel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func jsonBytes(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (p *Panel) appLogPath(appID int64) (string, string, error) {
	app, err := p.getApp(appID)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(p.logsDir(), "apps", app.Slug+".log"), app.Name, nil
}

func (p *Panel) handleAppLogs(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	path, name, err := p.appLogPath(id)
	if err != nil {
		httpError(w, err)
		return
	}
	if qBool(r, "download") {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+".log"))
		http.ServeFile(w, r, path)
		return
	}
	lines := tailFile(path, qInt(r, "lines", 200))
	info, _ := os.Stat(path)
	writeJSON(w, http.StatusOK, map[string]any{
		"lines": lines, "name": name, "live": true,
		"path": path, "updatedAt": fileTime(info),
	})
}

func fileTime(info os.FileInfo) string {
	if info == nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339)
}

func (p *Panel) handleAppLogStream(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	path, _, err := p.appLogPath(id)
	if err != nil {
		httpError(w, err)
		return
	}
	streamLogFile(w, r, path, qInt(r, "lines", 200))
}

func (p *Panel) handleDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	dep, err := p.getDeployment(id)
	if err != nil {
		httpError(w, err)
		return
	}
	if qBool(r, "download") {
		if dep.LogPath == "" {
			fail(w, http.StatusNotFound, "Log deploy belum tersedia")
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("deploy-%d.log", id)))
		http.ServeFile(w, r, dep.LogPath)
		return
	}
	if dep.LogPath == "" {
		writeJSON(w, http.StatusOK, map[string]any{"lines": []string{}, "live": dep.Status == "antre" || dep.Status == "berjalan", "deployment": dep})
		return
	}
	info, _ := os.Stat(dep.LogPath)
	writeJSON(w, http.StatusOK, map[string]any{
		"lines": tailFile(dep.LogPath, qInt(r, "lines", 300)), "live": dep.Status == "antre" || dep.Status == "berjalan",
		"updatedAt": fileTime(info), "deployment": dep,
	})
}

func (p *Panel) handleDeploymentLogStream(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	dep, err := p.getDeployment(id)
	if err != nil {
		httpError(w, err)
		return
	}
	if dep.LogPath == "" {
		streamLogFile(w, r, "", 0)
		return
	}
	streamLogFile(w, r, dep.LogPath, qInt(r, "lines", 300))
}

func streamLogFile(w http.ResponseWriter, r *http.Request, path string, initial int) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "Streaming tidak didukung")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func(event string, data any) bool {
		_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonBytes(data))
		if err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if initial <= 0 {
		initial = 200
	}
	if initial > 2000 {
		initial = 2000
	}
	var offset int64
	if path != "" {
		if lines := tailFile(path, initial); len(lines) > 0 {
			if !send("history", map[string]any{"lines": lines}) {
				return
			}
		}
		if info, err := os.Stat(path); err == nil {
			offset = info.Size()
		}
	} else {
		_ = send("history", map[string]any{"lines": []string{}})
	}
	_ = send("status", map[string]any{"live": true, "at": nowUTC()})
	ticker := time.NewTicker(450 * time.Millisecond)
	defer ticker.Stop()
	idle := time.NewTicker(20 * time.Second)
	defer idle.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-idle.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if path == "" {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if info.Size() < offset {
				offset = 0 // rotasi log
			}
			if info.Size() == offset {
				continue
			}
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			_, _ = f.Seek(offset, io.SeekStart)
			buf := make([]byte, 256<<10)
			n, _ := f.Read(buf)
			f.Close()
			if n <= 0 {
				continue
			}
			offset += int64(n)
			chunk := strings.TrimRight(string(buf[:n]), "\r\n")
			lines := strings.Split(chunk, "\n")
			for i := range lines {
				lines[i] = strings.TrimSuffix(lines[i], "\r")
			}
			if !send("lines", map[string]any{"lines": lines}) {
				return
			}
		}
	}
}

func (p *Panel) handleLogsMeta(w http.ResponseWriter, r *http.Request) {
	apps, _ := p.listApps()
	type logInfo struct {
		AppID int64  `json:"appId"`
		Name  string `json:"name"`
		Slug  string `json:"slug"`
		Size  int64  `json:"size"`
		At    string `json:"updatedAt"`
	}
	out := []logInfo{}
	for _, a := range apps {
		path := filepath.Join(p.logsDir(), "apps", a.Slug+".log")
		info, _ := os.Stat(path)
		li := logInfo{AppID: a.ID, Name: a.Name, Slug: a.Slug}
		if info != nil {
			li.Size = info.Size()
			li.At = fileTime(info)
		}
		out = append(out, li)
	}
	deps, _ := p.listDeployments(0, 40)
	writeJSON(w, http.StatusOK, map[string]any{"apps": out, "deployments": deps, "deployLogDir": filepath.Join(p.logsDir(), "deploy")})
}
