package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type driveCredentials struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	RefreshToken string `json:"refreshToken"`
}

func (p *Panel) driveCreds() driveCredentials {
	var c driveCredentials
	_ = p.getCredJSON("google_drive", &c)
	return c
}

func (p *Panel) driveConfigured() bool {
	c := p.driveCreds()
	return c.ClientID != "" && c.ClientSecret != "" && c.RefreshToken != ""
}

func (p *Panel) driveStatus() map[string]any {
	c := p.driveCreds()
	return map[string]any{
		"configured": c.ClientID != "" && c.ClientSecret != "",
		"connected":  c.RefreshToken != "",
		"clientId":   c.ClientID,
		"scope":      "https://www.googleapis.com/auth/drive.file",
		"rootFolder": p.setting("drive_root_folder", "SatriaPanel-Backup"),
	}
}

func (p *Panel) handleGoogleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, p.driveStatus())
		return
	}
	var in struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		Disconnect   bool   `json:"disconnect"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Disconnect {
		_, _ = p.db.Exec(`DELETE FROM credentials WHERE kind='google_drive'`)
		p.audit("drive.disconnect", "integration", "google", "Google Drive diputus")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	old := p.driveCreds()
	c := driveCredentials{ClientID: strings.TrimSpace(in.ClientID), ClientSecret: in.ClientSecret, RefreshToken: old.RefreshToken}
	if c.ClientID == "" || in.ClientSecret == "" {
		if old.ClientID == "" || old.ClientSecret == "" {
			fail(w, http.StatusBadRequest, "Client ID dan Client Secret wajib diisi")
			return
		}
		c.ClientID, c.ClientSecret = old.ClientID, old.ClientSecret
	}
	if c.ClientID != old.ClientID {
		c.RefreshToken = ""
	}
	_ = p.saveCredJSON("google_drive", "Google Drive", c)
	p.audit("drive.config", "integration", "google", "Kredensial OAuth Google Drive disimpan")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": p.driveStatus()})
}

func (p *Panel) handleGoogleAuth(w http.ResponseWriter, r *http.Request) {
	c := p.driveCreds()
	if c.ClientID == "" || c.ClientSecret == "" {
		fail(w, http.StatusBadRequest, "Isi Client ID dan Client Secret terlebih dahulu")
		return
	}
	state := randomToken(24)
	_ = p.setSetting("google_oauth_state", state)
	_ = p.setSetting("google_oauth_state_at", nowUTC())
	redirect := p.baseURL(r) + "/api/integrations/google/callback"
	q := url.Values{}
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("response_type", "code")
	q.Set("scope", "https://www.googleapis.com/auth/drive.file")
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	q.Set("state", state)
	writeJSON(w, http.StatusOK, map[string]any{"url": "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode(), "redirectUri": redirect})
}

func (p *Panel) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		http.Redirect(w, r, "/#/backup?google=error&reason="+url.QueryEscape(errMsg), http.StatusFound)
		return
	}
	state := r.URL.Query().Get("state")
	expected := p.setting("google_oauth_state", "")
	saved := p.setting("google_oauth_state_at", "")
	if state == "" || expected == "" || state != expected || (saved != "" && time.Since(parseTime(saved)) > 15*time.Minute) {
		http.Redirect(w, r, "/#/backup?google=error&reason=state", http.StatusFound)
		return
	}
	_ = p.setSetting("google_oauth_state", "")
	code := r.URL.Query().Get("code")
	c := p.driveCreds()
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("redirect_uri", p.baseURL(r)+"/api/integrations/google/callback")
	form.Set("grant_type", "authorization_code")
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		http.Redirect(w, r, "/#/backup?google=error&reason="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	defer resp.Body.Close()
	var tok struct {
		RefreshToken string `json:"refresh_token"`
		AccessToken  string `json:"access_token"`
		Error        string `json:"error_description"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	if resp.StatusCode >= 300 || tok.RefreshToken == "" {
		http.Redirect(w, r, "/#/backup?google=error&reason="+url.QueryEscape(defaultStr(tok.Error, "token exchange gagal")), http.StatusFound)
		return
	}
	c.RefreshToken = tok.RefreshToken
	_ = p.saveCredJSON("google_drive", "Google Drive", c)
	p.audit("drive.connect", "integration", "google", "Google Drive terhubung dengan scope drive.file")
	http.Redirect(w, r, "/#/backup?google=connected", http.StatusFound)
}

func (p *Panel) driveAccessToken(ctx context.Context) (string, error) {
	c := p.driveCreds()
	if c.ClientID == "" || c.ClientSecret == "" || c.RefreshToken == "" {
		return "", fmt.Errorf("Google Drive belum terhubung")
	}
	form := url.Values{}
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("refresh_token", c.RefreshToken)
	form.Set("grant_type", "refresh_token")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error_description"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	if resp.StatusCode >= 300 || tok.AccessToken == "" {
		return "", fmt.Errorf("refresh token Google gagal: %s", defaultStr(tok.Error, resp.Status))
	}
	return tok.AccessToken, nil
}

func (p *Panel) driveFindOrCreateFolder(ctx context.Context, token, name, parent string) (string, error) {
	q := fmt.Sprintf("name='%s' and mimeType='application/vnd.google-apps.folder' and trashed=false", strings.ReplaceAll(name, "'", `\'`))
	if parent != "" {
		q += fmt.Sprintf(" and '%s' in parents", parent)
	}
	u := "https://www.googleapis.com/drive/v3/files?fields=files(id,name)&pageSize=5&q=" + url.QueryEscape(q)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var found struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&found)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("pencarian folder Drive gagal: %s", resp.Status)
	}
	if len(found.Files) > 0 {
		return found.Files[0].ID, nil
	}
	meta := map[string]any{"name": name, "mimeType": "application/vnd.google-apps.folder"}
	if parent != "" {
		meta["parents"] = []string{parent}
	}
	b, _ := json.Marshal(meta)
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, "https://www.googleapis.com/drive/v3/files?fields=id", strings.NewReader(string(b)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var created struct {
		ID    string `json:"id"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	if resp.StatusCode >= 300 || created.ID == "" {
		return "", fmt.Errorf("membuat folder Drive gagal: %s", created.Error.Message)
	}
	return created.ID, nil
}

func (p *Panel) uploadDriveBackup(localPath, server string, started time.Time) (string, string, error) {
	if !p.driveConfigured() {
		return "", "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	token, err := p.driveAccessToken(ctx)
	if err != nil {
		return "", "", err
	}
	root, err := p.driveFindOrCreateFolder(ctx, token, p.setting("drive_root_folder", "SatriaPanel-Backup"), "")
	if err != nil {
		return "", "", err
	}
	serverFolder, err := p.driveFindOrCreateFolder(ctx, token, server, root)
	if err != nil {
		return "", "", err
	}
	dateFolder, err := p.driveFindOrCreateFolder(ctx, token, started.Format("2006-01-02"), serverFolder)
	if err != nil {
		return "", "", err
	}
	fileID, link, err := p.driveUploadFile(ctx, token, localPath, dateFolder)
	if err != nil {
		return "", "", err
	}
	_ = p.setSetting("drive_server_folder", serverFolder)
	_ = p.setSetting("drive_last_file", fileID)
	go p.driveRetention(serverFolder)
	return dateFolder, link, nil
}

func (p *Panel) driveUploadFile(ctx context.Context, token, localPath, parent string) (string, string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", "", err
	}
	meta := map[string]any{"name": filepath.Base(localPath), "parents": []string{parent}}
	b, _ := json.Marshal(meta)
	u := "https://www.googleapis.com/upload/drive/v3/files?uploadType=resumable&fields=id,name,webViewLink"
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(b)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", "application/zip")
	req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(info.Size(), 10))
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("inisiasi unggah Drive gagal: %s", resp.Status)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", "", fmt.Errorf("Drive tidak memberi URL unggah")
	}
	f, err := os.Open(localPath)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	req, _ = http.NewRequestWithContext(ctx, http.MethodPut, location, f)
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", "application/zip")
	resp, err = (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var out struct {
		ID          string `json:"id"`
		WebViewLink string `json:"webViewLink"`
		Error       struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 || out.ID == "" {
		return "", "", fmt.Errorf("unggah Drive gagal: %s", defaultStr(out.Error.Message, resp.Status))
	}
	return out.ID, out.WebViewLink, nil
}

func (p *Panel) driveRetention(serverFolder string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	token, err := p.driveAccessToken(ctx)
	if err != nil {
		return
	}
	q := fmt.Sprintf("'%s' in parents and trashed=false and name contains 'satriapanel-'", serverFolder)
	u := "https://www.googleapis.com/drive/v3/files?fields=files(id,name,createdTime)&pageSize=1000&q=" + url.QueryEscape(q)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var list struct {
		Files []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			CreatedTime string `json:"createdTime"`
		} `json:"files"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	sort.Slice(list.Files, func(i, j int) bool { return list.Files[i].CreatedTime > list.Files[j].CreatedTime })
	keepDaily := p.intSetting("backup_retention_d", 7)
	keepWeekly := p.intSetting("backup_retention_w", 4)
	daySeen := map[string]bool{}
	weekSeen := map[string]bool{}
	keep := map[string]bool{}
	for _, f := range list.Files {
		t, err := time.Parse(time.RFC3339, f.CreatedTime)
		if err != nil {
			continue
		}
		day := t.Format("2006-01-02")
		year, week := t.ISOWeek()
		wk := fmt.Sprintf("%d-%02d", year, week)
		if !daySeen[day] && len(daySeen) < keepDaily {
			daySeen[day] = true
			keep[f.ID] = true
			continue
		}
		if !weekSeen[wk] && len(weekSeen) < keepWeekly {
			weekSeen[wk] = true
			keep[f.ID] = true
		}
	}
	for _, f := range list.Files {
		if keep[f.ID] {
			continue
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, "https://www.googleapis.com/drive/v3/files/"+url.PathEscape(f.ID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}
}
