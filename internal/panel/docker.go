package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DockerContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Ports   string `json:"ports"`
	Created string `json:"created"`
	Project string `json:"project"`
	Service string `json:"service"`
	Managed bool   `json:"managed"`
}

func (p *Panel) dockerAvailable() bool {
	_, err := runCmdOutput(context.Background(), "docker version --format '{{.Server.Version}}'", 6*time.Second)
	return err == nil
}

func (p *Panel) dockerOverview() map[string]any {
	out := map[string]any{"available": false, "containers": []DockerContainer{}, "images": []map[string]any{}, "volumes": []map[string]any{}, "networks": []map[string]any{}, "disk": map[string]any{}}
	version, err := runCmdOutput(context.Background(), "docker version --format '{{.Server.Version}}'", 6*time.Second)
	if err != nil {
		out["error"] = strings.TrimSpace(version)
		return out
	}
	out["available"] = true
	out["version"] = strings.TrimSpace(version)
	out["containers"] = p.dockerContainers()
	out["images"] = p.dockerImages()
	out["volumes"] = p.dockerVolumes()
	out["networks"] = p.dockerNetworks()
	out["disk"] = p.dockerDisk()
	return out
}

func (p *Panel) dockerContainers() []DockerContainer {
	res := []DockerContainer{}
	raw, err := runCmdOutput(context.Background(), "docker ps -a --no-trunc --format '{{json .}}'", 12*time.Second)
	if err != nil {
		return res
	}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]string
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		id := row["ID"]
		name := strings.TrimPrefix(row["Names"], "/")
		c := DockerContainer{
			ID: id, Name: name, Image: row["Image"], State: row["State"], Status: row["Status"],
			Ports: row["Ports"], Created: row["RunningFor"], Project: row["Project"], Service: row["Service"],
			Managed: strings.HasPrefix(name, "sp-"),
		}
		res = append(res, c)
	}
	return res
}

func (p *Panel) dockerImages() []map[string]any {
	res := []map[string]any{}
	raw, err := runCmdOutput(context.Background(), "docker images --format '{{json .}}'", 12*time.Second)
	if err != nil {
		return res
	}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		var row map[string]string
		if json.Unmarshal([]byte(line), &row) == nil {
			res = append(res, map[string]any{"repository": row["Repository"], "tag": row["Tag"], "id": row["ID"], "size": row["Size"], "created": row["CreatedSince"], "managed": strings.HasPrefix(row["Repository"], "sp-")})
		}
	}
	return res
}

func (p *Panel) dockerVolumes() []map[string]any {
	res := []map[string]any{}
	raw, err := runCmdOutput(context.Background(), "docker volume ls --format '{{json .}}'", 12*time.Second)
	if err != nil {
		return res
	}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		var row map[string]string
		if json.Unmarshal([]byte(line), &row) == nil {
			res = append(res, map[string]any{"name": row["Name"], "driver": row["Driver"], "scope": row["Scope"]})
		}
	}
	return res
}

func (p *Panel) dockerNetworks() []map[string]any {
	res := []map[string]any{}
	raw, err := runCmdOutput(context.Background(), "docker network ls --format '{{json .}}'", 12*time.Second)
	if err != nil {
		return res
	}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		var row map[string]string
		if json.Unmarshal([]byte(line), &row) == nil {
			res = append(res, map[string]any{"id": row["ID"], "name": row["Name"], "driver": row["Driver"], "scope": row["Scope"]})
		}
	}
	return res
}

func (p *Panel) dockerDisk() map[string]any {
	res := map[string]any{}
	raw, err := runCmdOutput(context.Background(), "docker system df -v --format '{{json .}}'", 15*time.Second)
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
			var row map[string]any
			if json.Unmarshal([]byte(line), &row) == nil {
				for k, v := range row {
					res[k] = v
				}
			}
		}
	}
	return res
}

func (p *Panel) handleDocker(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, p.dockerOverview())
		return
	}
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	var in struct {
		Action  string `json:"action"`
		Target  string `json:"target"`
		Force   bool   `json:"force"`
		Confirm string `json:"confirm"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Target == "" {
		in.Target = "images"
	}
	var cmd string
	switch in.Target {
	case "containers":
		cmd = "docker container prune -f"
	case "images":
		cmd = "docker image prune -a -f"
	case "volumes":
		cmd = "docker volume prune -f"
	case "networks":
		cmd = "docker network prune -f"
	case "builder":
		cmd = "docker builder prune -f"
	default:
		fail(w, http.StatusBadRequest, "Target prune tidak dikenal")
		return
	}
	out, err := runCmdOutput(context.Background(), cmd, 90*time.Second)
	if err != nil {
		failDetail(w, http.StatusInternalServerError, "Prune gagal", strings.TrimSpace(out))
		return
	}
	p.audit("docker.prune", "docker", in.Target, "Prune Docker: "+strings.TrimSpace(out))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": strings.TrimSpace(out)})
}

func (p *Panel) handleDockerContainer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	action := r.PathValue("action")
	if id == "" {
		fail(w, http.StatusBadRequest, "Kontainer tidak valid")
		return
	}
	// hanya izinkan karakter aman
	if strings.ContainsAny(id, " ;&|`$()\"'<>") {
		fail(w, http.StatusBadRequest, "Nama kontainer tidak valid")
		return
	}
	q := shellQuote(id)
	var cmd string
	switch action {
	case "start":
		cmd = "docker start " + q
	case "stop":
		cmd = "docker stop " + q
	case "restart":
		cmd = "docker restart " + q
	case "pause":
		cmd = "docker pause " + q
	case "unpause":
		cmd = "docker unpause " + q
	case "remove":
		cmd = "docker rm -f " + q
	default:
		fail(w, http.StatusBadRequest, "Aksi tidak dikenal")
		return
	}
	out, err := runCmdOutput(context.Background(), cmd, 45*time.Second)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Aksi Docker gagal", strings.TrimSpace(out))
		return
	}
	p.audit("docker."+action, "docker", id, "Kontainer Docker "+id+": "+action)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": strings.TrimSpace(out)})
}

func (p *Panel) handleDockerLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || strings.ContainsAny(id, " ;&|`$()\"'<>") {
		fail(w, http.StatusBadRequest, "Kontainer tidak valid")
		return
	}
	lines := qInt(r, "lines", 300)
	if lines < 1 || lines > 5000 {
		lines = 300
	}
	out, err := runCmdOutput(context.Background(), fmt.Sprintf("docker logs --tail %d --timestamps %s 2>&1", lines, shellQuote(id)), 25*time.Second)
	if err != nil && out == "" {
		failDetail(w, http.StatusBadRequest, "Gagal membaca log", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": strings.Split(strings.TrimRight(out, "\r\n"), "\n")})
}

func (p *Panel) handleDockerImages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"images": p.dockerImages()})
		return
	}
	var in struct {
		ID     string `json:"id"`
		Force  bool   `json:"force"`
		Action string `json:"action"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Action != "remove" || in.ID == "" || strings.ContainsAny(in.ID, " ;&|`$()\"'<>") {
		fail(w, http.StatusBadRequest, "Permintaan tidak valid")
		return
	}
	out, err := runCmdOutput(context.Background(), "docker rmi "+func() string {
		if in.Force {
			return "-f "
		}
		return ""
	}()+shellQuote(in.ID), 60*time.Second)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Gagal menghapus image", strings.TrimSpace(out))
		return
	}
	p.audit("docker.image_delete", "docker", in.ID, "Image Docker dihapus")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": strings.TrimSpace(out)})
}

// ---------- runtime aplikasi Docker ----------

func (m *processManager) startDockerApp(app *App) error {
	p := m.p
	m.stopDockerApp(app)
	name := "sp-" + app.Slug
	dir := p_activeDir(app)
	envFile := filepath.Join(dir, ".env")
	if app.Mode == "compose" {
		if dir == "" {
			return fmt.Errorf("belum ada rilis aktif")
		}
		env, err := p.appEnv(app, true)
		if err == nil {
			_ = writeEnvFile(envFile, env)
		}
		out, err := runCmdOutputDir(context.Background(), dir, "docker compose -p "+shellQuote(name)+" up -d", 120*time.Second)
		if err != nil {
			return fmt.Errorf("docker compose: %s", truncate(strings.TrimSpace(out), 300))
		}
		_, _ = p.db.Exec(`UPDATE apps SET status='berjalan', updated_at=? WHERE id=?`, nowUTC(), app.ID)
		return nil
	}
	image := app.Image
	var tag string
	if app.ActiveReleaseID != nil {
		_ = p.db.QueryRow(`SELECT image_tag FROM releases WHERE id=?`, *app.ActiveReleaseID).Scan(&tag)
	}
	if tag != "" {
		image = tag
	}
	if image == "" {
		return fmt.Errorf("image belum tersedia — deploy terlebih dahulu")
	}
	internal := app.InternalPort
	if internal <= 0 {
		internal = 80
	}
	if dir == "" {
		dir = app.Workdir
	}
	args := []string{"run", "-d", "--name", shellQuote(name), "--restart", "unless-stopped",
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", app.Port, internal)}
	if _, err := os.Stat(envFile); err == nil {
		args = append(args, "--env-file", shellQuote(envFile))
	}
	args = append(args, shellQuote(image))
	cmd := "docker " + strings.Join(args, " ")
	out, err := runCmdOutput(context.Background(), cmd, 120*time.Second)
	if err != nil {
		return fmt.Errorf("docker run: %s", truncate(strings.TrimSpace(out), 300))
	}
	_, _ = p.db.Exec(`UPDATE apps SET status='berjalan', updated_at=? WHERE id=?`, nowUTC(), app.ID)
	m.mu.Lock()
	m.procs[app.ID] = &procState{appID: app.ID, driver: "docker", started: time.Now()}
	m.mu.Unlock()
	return nil
}

func (m *processManager) stopDockerApp(app *App) {
	name := "sp-" + app.Slug
	if app.Mode == "compose" {
		_, _ = runCmdOutputDir(context.Background(), p_activeDir(app), "docker compose -p "+shellQuote(name)+" down", 90*time.Second)
	} else {
		_, _ = runCmdOutput(context.Background(), "docker rm -f "+shellQuote(name), 60*time.Second)
	}
	if !m.isRunning(app.ID) {
		_, _ = m.p.db.Exec(`UPDATE apps SET status='berhenti', health_status='unknown', updated_at=? WHERE id=?`, nowUTC(), app.ID)
	}
}
