package panel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------- kredensial terenkripsi ----------

func (p *Panel) getCredJSON(kind string, dst any) error {
	var secret string
	if err := p.db.QueryRow(`SELECT secret FROM credentials WHERE kind=?`, kind).Scan(&secret); err != nil {
		return err
	}
	plain := p.decrypt(secret)
	if plain == "" {
		return fmt.Errorf("kredensial kosong")
	}
	return json.Unmarshal([]byte(plain), dst)
}

func (p *Panel) saveCredJSON(kind, label string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	enc, err := p.encrypt(string(b))
	if err != nil {
		return err
	}
	_, err = p.db.Exec(`INSERT INTO credentials(kind,label,secret,meta,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(kind) DO UPDATE SET secret=excluded.secret,label=excluded.label,updated_at=excluded.updated_at`,
		kind, label, enc, "{}", nowUTC())
	return err
}

func (p *Panel) credentialExists(kind string) bool {
	var n int
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM credentials WHERE kind=?`, kind).Scan(&n)
	return n > 0
}

// ---------- GitHub ----------

type githubCredentials struct {
	Token string `json:"token"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

func (p *Panel) githubCreds() githubCredentials {
	var c githubCredentials
	_ = p.getCredJSON("github", &c)
	return c
}

func (p *Panel) githubToken() string { return p.githubCreds().Token }

func githubAPI(ctx context.Context, token, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "SatriaPanel")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode >= 300 {
		var ghErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &ghErr)
		return fmt.Errorf("GitHub %s: %s", resp.Status, defaultStr(ghErr.Message, truncate(string(data), 200)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (p *Panel) handleGitHubConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		c := p.githubCreds()
		writeJSON(w, http.StatusOK, map[string]any{"configured": c.Token != "", "login": c.Login, "name": c.Name})
		return
	}
	var in struct {
		Token      string `json:"token"`
		Disconnect bool   `json:"disconnect"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Disconnect {
		_, _ = p.db.Exec(`DELETE FROM credentials WHERE kind='github'`)
		p.audit("github.disconnect", "integration", "github", "Kredensial GitHub dihapus")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	token := strings.TrimSpace(in.Token)
	if token == "" {
		fail(w, http.StatusBadRequest, "Personal Access Token wajib diisi")
		return
	}
	var user struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := githubAPI(ctx, token, http.MethodGet, "/user", nil, &user); err != nil {
		failDetail(w, http.StatusBadRequest, "Token GitHub tidak valid", err.Error())
		return
	}
	c := githubCredentials{Token: token, Login: user.Login, Name: user.Name}
	if err := p.saveCredJSON("github", user.Login, c); err != nil {
		httpError(w, err)
		return
	}
	p.audit("github.config", "integration", "github", "Token GitHub disimpan untuk @"+user.Login)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "login": user.Login, "name": user.Name})
}

func (p *Panel) handleGitHubRepos(w http.ResponseWriter, r *http.Request) {
	token := p.githubToken()
	if token == "" {
		fail(w, http.StatusBadRequest, "GitHub belum terhubung")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	var repos []struct {
		FullName      string `json:"full_name"`
		Name          string `json:"name"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
		UpdatedAt     string `json:"updated_at"`
		HTMLURL       string `json:"html_url"`
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	path := "/user/repos?per_page=100&sort=updated"
	if search != "" {
		path = "/search/repositories?q=" + url.QueryEscape(search+" user:@"+p.githubCreds().Login) + "&per_page=50"
	}
	var wrap struct {
		Items []struct {
			FullName      string `json:"full_name"`
			Name          string `json:"name"`
			Private       bool   `json:"private"`
			DefaultBranch string `json:"default_branch"`
			UpdatedAt     string `json:"updated_at"`
			HTMLURL       string `json:"html_url"`
		} `json:"items"`
	}
	if search != "" {
		if err := githubAPI(ctx, token, http.MethodGet, path, nil, &wrap); err != nil {
			failDetail(w, http.StatusBadGateway, "Gagal mengambil repositori", err.Error())
			return
		}
		for _, it := range wrap.Items {
			repos = append(repos, struct {
				FullName      string `json:"full_name"`
				Name          string `json:"name"`
				Private       bool   `json:"private"`
				DefaultBranch string `json:"default_branch"`
				UpdatedAt     string `json:"updated_at"`
				HTMLURL       string `json:"html_url"`
			}{it.FullName, it.Name, it.Private, it.DefaultBranch, it.UpdatedAt, it.HTMLURL})
		}
	} else if err := githubAPI(ctx, token, http.MethodGet, path, nil, &repos); err != nil {
		failDetail(w, http.StatusBadGateway, "Gagal mengambil repositori", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos, "login": p.githubCreds().Login})
}

func (p *Panel) handleGitHubBranches(w http.ResponseWriter, r *http.Request) {
	token := p.githubToken()
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if token == "" || repo == "" {
		fail(w, http.StatusBadRequest, "Repo atau token GitHub belum tersedia")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var branches []struct {
		Name string `json:"name"`
	}
	if err := githubAPI(ctx, token, http.MethodGet, "/repos/"+url.PathEscape(repo)+"/branches?per_page=100", nil, &branches); err != nil {
		failDetail(w, http.StatusBadGateway, "Gagal mengambil branch", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": branches})
}

func (p *Panel) handleGitHubDetect(w http.ResponseWriter, r *http.Request) {
	token := p.githubToken()
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	branch := defaultStr(r.URL.Query().Get("branch"), "main")
	if token == "" || repo == "" {
		fail(w, http.StatusBadRequest, "Repo atau token GitHub belum tersedia")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var listing []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := githubAPI(ctx, token, http.MethodGet, "/repos/"+url.PathEscape(repo)+"/contents?ref="+url.QueryEscape(branch), nil, &listing); err != nil {
		failDetail(w, http.StatusBadGateway, "Gagal memindai repo", err.Error())
		return
	}
	files := map[string]bool{}
	for _, f := range listing {
		files[f.Name] = true
	}
	runtimeName := "node"
	install, build, start := runtimeDefaults("node")
	entry := ""
	port := 3000
	mode := "native"
	healthPath := "/"
	if files["composer.json"] {
		runtimeName = "php"
		install, build, start = runtimeDefaults("php")
	} else if files["requirements.txt"] || files["pyproject.toml"] {
		runtimeName = "python"
		install, build, start = runtimeDefaults("python")
	} else if files["go.mod"] {
		runtimeName = "go"
		install, build, start = runtimeDefaults("go")
	} else if files["index.html"] && !files["package.json"] {
		runtimeName = "static"
		install, build, start = runtimeDefaults("static")
	}
	if files["Dockerfile"] {
		mode = "docker"
	}
	if files["docker-compose.yml"] || files["compose.yml"] {
		mode = "compose"
	}
	_ = entry
	writeJSON(w, http.StatusOK, map[string]any{
		"runtime": runtimeName, "mode": mode, "installCmd": install, "buildCmd": build, "startCmd": start,
		"port": port, "healthPath": healthPath, "files": listing,
		"hint": "Periksa kembali perintah build/start dan port internal sebelum deploy.",
	})
}

func (p *Panel) ensureGitHubWebhook(app *App) error {
	token := p.githubToken()
	if token == "" {
		return fmt.Errorf("GitHub belum terhubung")
	}
	ownerRepo, err := githubOwnerRepo(app.RepoURL)
	if err != nil {
		return err
	}
	secret := p.decrypt(app.WebhookSecretEnc)
	if secret == "" {
		return fmt.Errorf("secret webhook belum ada")
	}
	body, _ := json.Marshal(map[string]any{
		"name": "web", "active": true,
		"events": []string{"push"},
		"config": map[string]string{
			"url":          p.baseURL(nil) + "/api/webhooks/github/" + strconv.FormatInt(app.ID, 10),
			"content_type": "json", "secret": secret, "insecure_ssl": "0",
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var out struct {
		ID int64 `json:"id"`
	}
	if err := githubAPI(ctx, token, http.MethodPost, "/repos/"+url.PathEscape(ownerRepo)+"/hooks", strings.NewReader(string(body)), &out); err != nil {
		return err
	}
	_, err = p.db.Exec(`UPDATE apps SET webhook_id=? WHERE id=?`, strconv.FormatInt(out.ID, 10), app.ID)
	p.audit("github.webhook_create", "app", fmt.Sprint(app.ID), "Webhook GitHub dibuat untuk "+app.Name)
	return err
}

func (p *Panel) deleteGitHubWebhook(app *App) error {
	token := p.githubToken()
	if token == "" || app.WebhookID == "" {
		return nil
	}
	ownerRepo, err := githubOwnerRepo(app.RepoURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = githubAPI(ctx, token, http.MethodDelete, "/repos/"+url.PathEscape(ownerRepo)+"/hooks/"+app.WebhookID, nil, nil)
	if err == nil {
		_, _ = p.db.Exec(`UPDATE apps SET webhook_id='' WHERE id=?`, app.ID)
		p.audit("github.webhook_delete", "app", fmt.Sprint(app.ID), "Webhook GitHub dihapus untuk "+app.Name)
	}
	return err
}

func githubOwnerRepo(repoURL string) (string, error) {
	s := normalizeRepoURL(repoURL)
	s = strings.TrimSuffix(strings.TrimSuffix(s, ".git"), "/")
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("URL repo tidak valid")
	}
	owner := parts[len(parts)-2]
	repo := parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", fmt.Errorf("URL repo tidak valid")
	}
	return owner + "/" + repo, nil
}

func (p *Panel) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	if len(r.URL.Path) > 0 && strings.HasSuffix(r.URL.Path, "/") {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	app, err := p.getApp(id)
	if err != nil {
		fail(w, http.StatusNotFound, "Aplikasi tidak ditemukan")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 5<<20))
	if err != nil {
		fail(w, http.StatusBadRequest, "Body terlalu besar")
		return
	}
	secret := p.decrypt(app.WebhookSecretEnc)
	if secret == "" {
		fail(w, http.StatusBadRequest, "Webhook belum dikonfigurasi")
		return
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig == "" || !hmac.Equal([]byte(sig), []byte(want)) {
		p.auditWithIP("github.webhook_invalid", "app", fmt.Sprint(id), "Tanda tangan webhook tidak sah untuk "+app.Name, remoteIP(r))
		fail(w, http.StatusUnauthorized, "Tanda tangan webhook tidak sah")
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	if event == "ping" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pong": true})
		return
	}
	if event != "push" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ignored": event})
		return
	}
	var push struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	_ = json.Unmarshal(body, &push)
	expectedRef := "refs/heads/" + app.Branch
	if push.Ref != expectedRef {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ignored": "branch", "ref": push.Ref})
		return
	}
	if !app.AutoDeploy {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ignored": "auto_deploy_nonaktif"})
		return
	}
	dep, err := p.deployMgr.enqueue(app.ID, "webhook")
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	p.auditWithIP("github.webhook_deploy", "app", fmt.Sprint(app.ID), "Push "+truncate(push.After, 8)+" memicu deploy "+app.Name, remoteIP(r))
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "deploymentId": dep.ID})
}

func (p *Panel) handleAppWebhook(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	app, err := p.getApp(id)
	if err != nil {
		httpError(w, err)
		return
	}
	if r.Method == http.MethodDelete {
		if app.WebhookID == "" {
			fail(w, http.StatusBadRequest, "Webhook belum dibuat")
			return
		}
		if err := p.deleteGitHubWebhook(app); err != nil {
			failDetail(w, http.StatusBadGateway, "Gagal menghapus webhook", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if p.githubToken() == "" {
		fail(w, http.StatusBadRequest, "Hubungkan GitHub Personal Access Token terlebih dahulu di Pengaturan Integrasi")
		return
	}
	if app.WebhookSecretEnc == "" {
		secret, _ := p.encrypt(randomToken(32))
		_, _ = p.db.Exec(`UPDATE apps SET webhook_secret=?, auto_deploy=1 WHERE id=?`, secret, id)
		app, _ = p.getApp(id)
	}
	if app.WebhookID != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "webhookId": app.WebhookID, "message": "Webhook sudah ada"})
		return
	}
	if err := p.ensureGitHubWebhook(app); err != nil {
		failDetail(w, http.StatusBadGateway, "Gagal membuat webhook GitHub", err.Error())
		return
	}
	fresh, _ := p.getApp(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "app": fresh, "webhookId": fresh.WebhookID})
}

// ---------- Cloudflare ----------

type cloudflareCredentials struct {
	APIToken  string `json:"apiToken"`
	ZoneID    string `json:"zoneId"`
	ZoneName  string `json:"zoneName"`
	TunnelID  string `json:"tunnelId"`
	AccountID string `json:"accountId"`
}

func (p *Panel) cfCreds() cloudflareCredentials {
	var c cloudflareCredentials
	_ = p.getCredJSON("cloudflare", &c)
	return c
}

func cfAPI(ctx context.Context, token, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4"+path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var wrap struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return fmt.Errorf("respons Cloudflare tidak valid")
	}
	if !wrap.Success {
		msgs := []string{}
		for _, e := range wrap.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("Cloudflare: %s", strings.Join(msgs, "; "))
	}
	if out != nil && len(wrap.Result) > 0 {
		return json.Unmarshal(wrap.Result, out)
	}
	return nil
}

func (p *Panel) handleCloudflare(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		c := p.cfCreds()
		apps, _ := p.listApps()
		m := []map[string]any{}
		for _, a := range apps {
			m = append(m, map[string]any{"appId": a.ID, "name": a.Name, "hostname": a.Hostname, "port": a.Port, "dnsRecordId": a.DNSRecordID, "status": a.Status})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"configured": c.APIToken != "", "zoneId": c.ZoneID, "zoneName": c.ZoneName, "tunnelId": c.TunnelID,
			"accountId": c.AccountID, "panelHostname": p.setting("panel_hostname", "panel.satriyadi.com"),
			"configPath": p.setting("cloudflared_config", "/etc/cloudflared/config.yml"), "service": p.setting("cloudflared_service", "cloudflared"),
			"mappings": m, "autoSync": p.boolSetting("cloudflared_auto_sync", false),
			"ports":        p.portMappings(),
			"configExists": fileExists(p.setting("cloudflared_config", "/etc/cloudflared/config.yml")),
		})
		return
	}
	var in struct {
		APIToken      string `json:"apiToken"`
		ZoneID        string `json:"zoneId"`
		ZoneName      string `json:"zoneName"`
		TunnelID      string `json:"tunnelId"`
		AccountID     string `json:"accountId"`
		PanelHostname string `json:"panelHostname"`
		ConfigPath    string `json:"configPath"`
		Service       string `json:"service"`
		AutoSync      bool   `json:"autoSync"`
		KeepToken     bool   `json:"keepToken"`
		Disconnect    bool   `json:"disconnect"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Disconnect {
		_, _ = p.db.Exec(`DELETE FROM credentials WHERE kind='cloudflare'`)
		_ = p.setSetting("cloudflared_auto_sync", "false")
		p.audit("cloudflare.disconnect", "integration", "cloudflare", "Integrasi Cloudflare diputus")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	old := p.cfCreds()
	c := cloudflareCredentials{APIToken: old.APIToken, ZoneID: defaultStr(in.ZoneID, old.ZoneID), ZoneName: defaultStr(in.ZoneName, old.ZoneName), TunnelID: defaultStr(in.TunnelID, old.TunnelID), AccountID: defaultStr(in.AccountID, old.AccountID)}
	if !in.KeepToken && in.APIToken != "" {
		c.APIToken = strings.TrimSpace(in.APIToken)
	}
	if c.APIToken == "" {
		fail(w, http.StatusBadRequest, "API Token Cloudflare wajib diisi")
		return
	}
	if in.PanelHostname != "" && !validHostname(in.PanelHostname) {
		fail(w, http.StatusBadRequest, "Hostname panel tidak valid")
		return
	}
	_ = p.saveCredJSON("cloudflare", "Cloudflare", c)
	if in.PanelHostname != "" {
		_ = p.setSetting("panel_hostname", in.PanelHostname)
	}
	if in.ConfigPath != "" {
		_ = p.setSetting("cloudflared_config", in.ConfigPath)
	}
	if in.Service != "" {
		_ = p.setSetting("cloudflared_service", in.Service)
	}
	_ = p.setSetting("cloudflared_auto_sync", fmt.Sprint(in.AutoSync))
	p.audit("cloudflare.config", "integration", "cloudflare", "Konfigurasi Cloudflare disimpan")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleCloudflareTest(w http.ResponseWriter, r *http.Request) {
	c := p.cfCreds()
	if c.APIToken == "" {
		fail(w, http.StatusBadRequest, "Cloudflare belum dikonfigurasi")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var verify struct {
		Status string `json:"status"`
	}
	if err := cfAPI(ctx, c.APIToken, http.MethodGet, "/user/tokens/verify", nil, &verify); err != nil {
		failDetail(w, http.StatusBadGateway, "Token Cloudflare tidak valid", err.Error())
		return
	}
	tunnel := ""
	if c.TunnelID != "" {
		var t struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		path := "/accounts/" + c.AccountID + "/cfd_tunnel/" + c.TunnelID
		if c.AccountID != "" {
			if err := cfAPI(ctx, c.APIToken, http.MethodGet, path, nil, &t); err == nil {
				tunnel = t.Name + " (" + t.Status + ")"
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": verify.Status, "tunnel": tunnel})
}

func (p *Panel) handleCloudflareApply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm string `json:"confirm"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Confirm != "TERAPKAN" {
		fail(w, http.StatusBadRequest, "Ketik TERAPKAN untuk konfirmasi penulisan konfigurasi")
		return
	}
	c := p.cfCreds()
	if c.TunnelID == "" {
		fail(w, http.StatusBadRequest, "Tunnel ID belum diisi")
		return
	}
	path := p.setting("cloudflared_config", "/etc/cloudflared/config.yml")
	apps, _ := p.listApps()
	var b strings.Builder
	credFile := ""
	if old, err := os.ReadFile(path); err == nil {
		re := regexp.MustCompile(`(?m)^credentials-file:\s*(.+)$`)
		if m := re.FindStringSubmatch(string(old)); len(m) > 1 {
			credFile = strings.TrimSpace(m[1])
		}
	}
	b.WriteString("# Dikelola SatriaPanel — perubahan manual dapat tertimpa saat Terapkan\n")
	b.WriteString("tunnel: " + c.TunnelID + "\n")
	if credFile != "" {
		b.WriteString("credentials-file: " + credFile + "\n")
	}
	b.WriteString("ingress:\n")
	b.WriteString("  - hostname: " + p.setting("panel_hostname", "panel.satriyadi.com") + "\n")
	b.WriteString(fmt.Sprintf("    service: http://127.0.0.1:%d\n", p.intSetting("panel_port", 8282)))
	seen := map[string]bool{}
	for _, a := range apps {
		if a.Hostname == "" || seen[a.Hostname] {
			continue
		}
		seen[a.Hostname] = true
		b.WriteString("  - hostname: " + a.Hostname + "\n")
		b.WriteString(fmt.Sprintf("    service: http://127.0.0.1:%d\n", a.Port))
	}
	b.WriteString("  - service: http_status:404\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		failDetail(w, http.StatusInternalServerError, "Tidak dapat menulis konfigurasi", err.Error())
		return
	}
	if fileExists(path) {
		backup := path + ".satriapanel-" + time.Now().Format("20060102-150405") + ".bak"
		if bts, err := os.ReadFile(path); err == nil {
			if err := os.WriteFile(backup, bts, 0o600); err != nil {
				failDetail(w, http.StatusInternalServerError, "Tidak dapat membuat cadangan konfigurasi", err.Error())
				return
			}
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		failDetail(w, http.StatusInternalServerError, "Tidak dapat menulis konfigurasi", err.Error())
		return
	}
	validation := "cloudflared tidak terpasang; konfigurasi ditulis tanpa validasi"
	if _, err := exec.LookPath("cloudflared"); err == nil {
		out, err := exec.Command("cloudflared", "tunnel", "--config", path, "ingress", "validate").CombinedOutput()
		if err != nil {
			failDetail(w, http.StatusBadRequest, "Konfigurasi tidak valid", strings.TrimSpace(string(out)))
			return
		}
		validation = "validasi cloudflared lulus"
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = runCmdOutput(ctx, "systemctl reload "+p.setting("cloudflared_service", "cloudflared"), 20*time.Second)
		cancel()
	}
	// sinkronkan DNS jika API tersedia
	dnsCount := 0
	if c.APIToken != "" && c.ZoneID != "" {
		for _, a := range apps {
			if a.Hostname == "" {
				continue
			}
			if err := p.cfEnsureDNS(a, c); err == nil {
				dnsCount++
			}
		}
	}
	p.audit("cloudflare.apply", "integration", "cloudflare", "Konfigurasi ingress diterapkan; "+validation)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "validation": validation, "path": path, "dnsSynced": dnsCount, "config": b.String()})
}

func (p *Panel) cfEnsureDNS(app *App, c cloudflareCredentials) error {
	if c.ZoneID == "" || c.TunnelID == "" || app.Hostname == "" {
		return fmt.Errorf("konfigurasi Cloudflare belum lengkap")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	target := c.TunnelID + ".cfargotunnel.com"
	if app.DNSRecordID != "" {
		var out struct {
			ID string `json:"id"`
		}
		err := cfAPI(ctx, c.APIToken, http.MethodPut, "/zones/"+c.ZoneID+"/dns_records/"+app.DNSRecordID,
			map[string]any{"type": "CNAME", "name": app.Hostname, "content": target, "proxied": true, "ttl": 1}, &out)
		if err == nil {
			return nil
		}
	}
	var out struct {
		ID string `json:"id"`
	}
	err := cfAPI(ctx, c.APIToken, http.MethodPost, "/zones/"+c.ZoneID+"/dns_records",
		map[string]any{"type": "CNAME", "name": app.Hostname, "content": target, "proxied": true, "ttl": 1}, &out)
	if err != nil {
		return err
	}
	_, _ = p.db.Exec(`UPDATE apps SET dns_record_id=? WHERE id=?`, out.ID, app.ID)
	return nil
}

func (p *Panel) syncAppIngress(appID int64, oldHostname string) {
	app, err := p.getApp(appID)
	if err != nil {
		return
	}
	c := p.cfCreds()
	if c.APIToken != "" && c.ZoneID != "" && app.Hostname != "" {
		if err := p.cfEnsureDNS(app, c); err != nil {
			p.logf("sinkronisasi DNS %s: %v", app.Hostname, err)
		}
	}
	if oldHostname != "" && oldHostname != app.Hostname && app.DNSRecordID != "" && c.APIToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := cfAPI(ctx, c.APIToken, http.MethodDelete, "/zones/"+c.ZoneID+"/dns_records/"+app.DNSRecordID, nil, nil); err == nil {
			_, _ = p.db.Exec(`UPDATE apps SET dns_record_id='' WHERE id=?`, app.ID)
		}
		cancel()
	}
	if p.boolSetting("cloudflared_auto_sync", false) && c.TunnelID != "" {
		path := p.setting("cloudflared_config", "/etc/cloudflared/config.yml")
		if fileExists(path) {
			// jangan menulis diam-diam; cukup catat agar admin menerapkan dari halaman Cloudflared
			p.notify("cloudflare", "Konfigurasi ingress perlu diterapkan", "Hostname "+app.Hostname+" ditambahkan. Buka Cloudflared & Port lalu klik Terapkan.", &app.ID)
		}
	}
}

func (p *Panel) removeAppIngress(app App) {
	c := p.cfCreds()
	if app.DNSRecordID != "" && c.APIToken != "" && c.ZoneID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = cfAPI(ctx, c.APIToken, http.MethodDelete, "/zones/"+c.ZoneID+"/dns_records/"+app.DNSRecordID, nil, nil)
	}
	if app.Hostname != "" {
		p.notify("cloudflare", "Hostname aplikasi dihapus", "Hapus/ubah ingress "+app.Hostname+" dari konfigurasi cloudflared jika masih ada.", nil)
	}
}

func (p *Panel) portMappings() []map[string]any {
	apps, _ := p.listApps()
	out := []map[string]any{}
	for _, a := range apps {
		out = append(out, map[string]any{"appId": a.ID, "name": a.Name, "slug": a.Slug, "port": a.Port, "hostname": a.Hostname, "status": a.Status})
	}
	return out
}

// ringkasan integrasi
func (p *Panel) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	gh := p.githubCreds()
	cf := p.cfCreds()
	drive := p.driveCreds()
	var tg telegramCredentials
	_ = p.getCredJSON("telegram", &tg)
	var wa whatsappCredentials
	_ = p.getCredJSON("whatsapp", &wa)
	writeJSON(w, http.StatusOK, map[string]any{
		"github":      map[string]any{"configured": gh.Token != "", "login": gh.Login},
		"cloudflare":  map[string]any{"configured": cf.APIToken != "", "zone": cf.ZoneName, "tunnelId": cf.TunnelID},
		"googleDrive": map[string]any{"configured": drive.ClientID != "", "connected": drive.RefreshToken != ""},
		"telegram":    map[string]any{"configured": tg.Token != "" && tg.ChatID != ""},
		"whatsapp":    map[string]any{"configured": wa.URL != "" && wa.Token != ""},
	})
}
