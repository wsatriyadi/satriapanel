package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MCPToken adalah token akses MCP. Nilai token asli tidak pernah disimpan.
type MCPToken struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"createdAt"`
	LastUsedAt string   `json:"lastUsedAt,omitempty"`
	RevokedAt  string   `json:"revokedAt,omitempty"`
	Active     bool     `json:"active"`
}

var mcpAllScopes = []string{"read", "deploy", "cron", "backup", "data-write", "admin"}

var mcpScopeLabels = map[string]string{
	"read":       "Baca status, log, riwayat, berkas, skema, dan SELECT",
	"deploy":     "Deploy, rollback, start/stop/restart aplikasi dan container",
	"cron":       "Buat, ubah, jalankan, dan hapus cron",
	"backup":     "Mulai dan pantau backup (bukan restore)",
	"data-write": "INSERT, UPDATE, DELETE pada database",
	"admin":      "Seluruh aksi destruktif, termasuk berkas, shell, restore, DDL",
}

type mcpRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

const mcpLatestProtocol = "2025-06-18"

var mcpProtocolVersions = map[string]bool{
	"2025-06-18": true,
	"2025-03-26": true,
	"2024-11-05": true,
}

func (p *Panel) listMCPTokens() []MCPToken {
	rows, err := p.db.Query(`SELECT id,name,prefix,scopes_json,created_at,COALESCE(last_used_at,''),COALESCE(revoked_at,'') FROM mcp_tokens ORDER BY id DESC`)
	if err != nil {
		return []MCPToken{}
	}
	defer rows.Close()
	out := []MCPToken{}
	for rows.Next() {
		var t MCPToken
		var scopes string
		if rows.Scan(&t.ID, &t.Name, &t.Prefix, &scopes, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt) != nil {
			continue
		}
		t.Scopes = parseMCPScopes(scopes)
		t.Active = t.RevokedAt == ""
		out = append(out, t)
	}
	return out
}

func parseMCPScopes(raw string) []string {
	var scopes []string
	if json.Unmarshal([]byte(raw), &scopes) != nil || len(scopes) == 0 {
		return []string{"read"}
	}
	out := []string{}
	for _, s := range scopes {
		if mcpKnownScope(s) && !containsString(out, s) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		out = []string{"read"}
	}
	return out
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func mcpKnownScope(s string) bool {
	for _, x := range mcpAllScopes {
		if x == s {
			return true
		}
	}
	return false
}

func (p *Panel) getMCPTokenByHash(hash string) (*MCPToken, error) {
	if hash == "" {
		return nil, errNotFound
	}
	var t MCPToken
	var scopes string
	err := p.db.QueryRow(`SELECT id,name,prefix,scopes_json,created_at,COALESCE(last_used_at,''),COALESCE(revoked_at,'')
		FROM mcp_tokens WHERE token_hash=?`, hash).
		Scan(&t.ID, &t.Name, &t.Prefix, &scopes, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
	if err != nil {
		return nil, err
	}
	if t.RevokedAt != "" {
		return nil, errNotFound
	}
	t.Scopes = parseMCPScopes(scopes)
	t.Active = true
	return &t, nil
}

func (t *MCPToken) hasScope(scope string) bool {
	if scope == "" {
		return true
	}
	if containsString(t.Scopes, scope) {
		return true
	}
	// Token admin adalah token kepercayaan penuh untuk seluruh katalog.
	return containsString(t.Scopes, "admin")
}

func (p *Panel) handleMCPTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	var in struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) < 2 || len(in.Name) > 80 {
		fail(w, http.StatusBadRequest, "Nama token harus 2-80 karakter")
		return
	}
	scopes := []string{}
	for _, s := range in.Scopes {
		if !mcpKnownScope(s) {
			fail(w, http.StatusBadRequest, "Scope tidak dikenal: "+s)
			return
		}
		if !containsString(scopes, s) {
			scopes = append(scopes, s)
		}
	}
	if len(scopes) == 0 {
		scopes = []string{"read"}
	}
	plain := "spmcp_" + randomToken(32)
	prefix := plain
	if len(prefix) > 14 {
		prefix = prefix[:14] + "…"
	}
	res, err := p.db.Exec(`INSERT INTO mcp_tokens(name,prefix,token_hash,scopes_json,created_at) VALUES(?,?,?,?,?)`,
		in.Name, prefix, tokenHash(plain), jsonString(scopes), nowUTC())
	if err != nil {
		httpError(w, err)
		return
	}
	id, _ := res.LastInsertId()
	p.audit("mcp.token_create", "mcp_token", strconv.FormatInt(id, 10), "Token MCP dibuat: "+in.Name+" ("+strings.Join(scopes, ", ")+")")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"token":     plain,
		"id":        id,
		"name":      in.Name,
		"scopes":    scopes,
		"createdAt": nowUTC(),
		"notice":    "Salin token sekarang. Token tidak akan ditampilkan lagi.",
	})
}

func (p *Panel) handleMCPTokenItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	id := pathID(r)
	if id == 0 {
		fail(w, http.StatusBadRequest, "ID token tidak valid")
		return
	}
	var name string
	if err := p.db.QueryRow(`SELECT name FROM mcp_tokens WHERE id=?`, id).Scan(&name); err != nil {
		fail(w, http.StatusNotFound, "Token MCP tidak ditemukan")
		return
	}
	if _, err := p.db.Exec(`UPDATE mcp_tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, nowUTC(), id); err != nil {
		httpError(w, err)
		return
	}
	p.audit("mcp.token_revoke", "mcp_token", strconv.FormatInt(id, 10), "Token MCP dicabut: "+name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleMCPSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	var in struct {
		Enabled   *bool `json:"enabled"`
		RateLimit *int  `json:"rateLimit"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Enabled != nil {
		if err := p.setSetting("mcp_enabled", strconv.FormatBool(*in.Enabled)); err != nil {
			httpError(w, err)
			return
		}
	}
	if in.RateLimit != nil {
		if *in.RateLimit < 1 || *in.RateLimit > 600 {
			fail(w, http.StatusBadRequest, "Batas laju harus 1-600 permintaan per menit")
			return
		}
		if err := p.setSetting("mcp_rate_limit", strconv.Itoa(*in.RateLimit)); err != nil {
			httpError(w, err)
			return
		}
	}
	p.audit("mcp.settings", "mcp", "", "Pengaturan MCP diperbarui")
	p.writeMCPConfig(w)
}

func (p *Panel) writeMCPConfig(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":   p.boolSetting("mcp_enabled", true),
		"rateLimit": p.intSetting("mcp_rate_limit", 60),
		"endpoint":  p.baseURL(nil) + "/mcp",
		"protocol":  mcpLatestProtocol,
		"scopes":    mcpAllScopes,
		"scopeInfo": mcpScopeLabels,
		"tokens":    p.listMCPTokens(),
	})
}

func (p *Panel) handleMCPConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	p.writeMCPConfig(w)
}

func (p *Panel) mcpRateAllowed(t *MCPToken) (bool, int) {
	limit := p.intSetting("mcp_rate_limit", 60)
	if limit < 1 {
		limit = 60
	}
	now := time.Now()
	p.mcpMu.Lock()
	defer p.mcpMu.Unlock()
	st := p.mcpRate[t.ID]
	if st == nil || now.Sub(st.window) >= time.Minute {
		st = &mcpRateState{window: now}
		p.mcpRate[t.ID] = st
	}
	if st.count >= limit {
		return false, limit
	}
	st.count++
	return true, limit
}

func mcpBearer(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (p *Panel) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": mcpRPCError{Code: -32600, Message: "Gunakan POST untuk endpoint MCP"},
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if !p.boolSetting("mcp_enabled", true) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": mcpRPCError{Code: -32003, Message: "Endpoint MCP dinonaktifkan di Pengaturan"},
		})
		return
	}
	token := mcpBearer(r)
	if token == "" || !strings.HasPrefix(token, "spmcp_") {
		w.Header().Set("WWW-Authenticate", `Bearer realm="SatriaPanel MCP"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": mcpRPCError{Code: -32001, Message: "Token MCP tidak valid atau tidak aktif"},
		})
		return
	}
	t, err := p.getMCPTokenByHash(tokenHash(token))
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="SatriaPanel MCP"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": mcpRPCError{Code: -32001, Message: "Token MCP tidak valid atau sudah dicabut"},
		})
		return
	}
	ip := remoteIP(r)
	if ok, limit := p.mcpRateAllowed(t); !ok {
		p.auditMCP(t.Name, "rate_limit", "", fmt.Sprintf("Ditolak: batas %d permintaan/menit terlampaui", limit), ip)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": mcpRPCError{Code: -32002, Message: fmt.Sprintf("Batas laju token terlampaui (%d/menit)", limit)},
		})
		return
	}
	_, _ = p.db.Exec(`UPDATE mcp_tokens SET last_used_at=? WHERE id=?`, nowUTC(), t.ID)

	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"jsonrpc": "2.0", "id": nil, "error": mcpRPCError{Code: -32700, Message: "Gagal membaca permintaan"}})
		return
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"jsonrpc": "2.0", "id": nil, "error": mcpRPCError{Code: -32700, Message: "Body JSON kosong"}})
		return
	}
	batch := strings.HasPrefix(trimmed, "[")
	if !batch {
		var req mcpRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"jsonrpc": "2.0", "id": nil, "error": mcpRPCError{Code: -32700, Message: "JSON tidak valid"}})
			return
		}
		p.answerMCP(w, r, t, req, ip)
		return
	}
	var reqs []mcpRPCRequest
	if err := json.Unmarshal(body, &reqs); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"jsonrpc": "2.0", "id": nil, "error": mcpRPCError{Code: -32700, Message: "Batch JSON tidak valid"}})
		return
	}
	if len(reqs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"jsonrpc": "2.0", "id": nil, "error": mcpRPCError{Code: -32600, Message: "Batch kosong"}})
		return
	}
	out := []mcpRPCResponse{}
	for _, req := range reqs {
		resp, notification := p.processMCP(r.Context(), t, req, ip)
		if notification || len(resp.ID) == 0 {
			continue
		}
		out = append(out, resp)
	}
	if len(out) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (p *Panel) answerMCP(w http.ResponseWriter, r *http.Request, t *MCPToken, req mcpRPCRequest, ip string) {
	resp, notification := p.processMCP(r.Context(), t, req, ip)
	if notification || len(resp.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (p *Panel) processMCP(ctx context.Context, t *MCPToken, req mcpRPCRequest, ip string) (mcpRPCResponse, bool) {
	resp := mcpRPCResponse{JSONRPC: "2.0", ID: req.ID}
	if strings.TrimSpace(req.Method) == "" {
		resp.Error = &mcpRPCError{Code: -32600, Message: "Method wajib diisi"}
		return resp, false
	}
	notification := isMCPNotification(req.ID)
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		version := params.ProtocolVersion
		if !mcpProtocolVersions[version] {
			version = mcpLatestProtocol
		}
		resp.Result = map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo":   map[string]any{"name": "SatriaPanel", "version": p.version},
			"instructions": "SatriaPanel MCP. Token memiliki scope terbatas. Tool destruktif memerlukan confirm=true dan target yang diulang persis. Deploy dan backup berjalan asinkron; pantau lewat deploy_status atau backup_status.",
		}
		return resp, notification
	case "ping":
		resp.Result = map[string]any{}
		return resp, notification
	case "notifications/initialized", "notifications/cancelled":
		return resp, true
	case "tools/list":
		scopes := toolListForToken(t)
		resp.Result = map[string]any{"tools": scopes}
		return resp, notification
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || strings.TrimSpace(params.Name) == "" {
			resp.Result = mcpToolError("Parameter tools/call tidak valid: name dan arguments wajib diisi")
			return resp, notification
		}
		result := p.callMCPTool(ctx, t, params.Name, params.Arguments, ip)
		resp.Result = result
		return resp, notification
	default:
		resp.Error = &mcpRPCError{Code: -32601, Message: "Method tidak dikenal: " + req.Method}
		return resp, false
	}
}

func isMCPNotification(id json.RawMessage) bool {
	s := strings.TrimSpace(string(id))
	return s == "" || s == "null"
}

func mcpToolError(message string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": message}},
		"isError": true,
	}
}

func mcpToolOK(v any) map[string]any {
	raw, _ := json.MarshalIndent(v, "", "  ")
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(raw)}},
		"structuredContent": v,
		"isError":           false,
	}
}

func (p *Panel) callMCPTool(ctx context.Context, t *MCPToken, name string, args map[string]any, ip string) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	tool, ok := mcpToolRegistry()[name]
	if !ok {
		p.auditMCP(t.Name, name, mcpAuditTarget(name, args), "Ditolak: tool tidak tersedia", ip)
		return mcpToolError("Tool tidak tersedia: " + name)
	}
	required, err := requiredMCPScope(name, args)
	if err != nil {
		p.auditMCP(t.Name, name, mcpAuditTarget(name, args), "Ditolak: "+err.Error(), ip)
		return mcpToolError(err.Error())
	}
	if !t.hasScope(required) {
		p.auditMCP(t.Name, name, mcpAuditTarget(name, args), "Ditolak: token tidak memiliki scope "+required+" | args: "+mcpSafeArgs(args), ip)
		return mcpToolError("Akses ditolak: token MCP tidak memiliki scope " + required)
	}
	_ = tool
	result, err := p.executeMCPTool(ctx, t, name, args)
	if err != nil {
		p.auditMCP(t.Name, name, mcpAuditTarget(name, args), "Gagal: "+err.Error()+" | args: "+mcpSafeArgs(args), ip)
		return mcpToolError(err.Error())
	}
	p.auditMCP(t.Name, name, mcpAuditTarget(name, args), "Berhasil | args: "+mcpSafeArgs(args), ip)
	return mcpToolOK(result)
}

func mcpAuditTarget(name string, args map[string]any) string {
	for _, key := range []string{"target", "app", "appId", "database", "databaseId", "backupId", "runId", "id", "containerId", "path", "command"} {
		if v, ok := args[key]; ok {
			s := fmt.Sprint(v)
			if s != "" && s != "<nil>" {
				return truncate(s, 300)
			}
		}
	}
	return name
}

func requiredMCPScope(name string, args map[string]any) (string, error) {
	switch name {
	case "db_query":
		q := strings.TrimSpace(mArgString(args, "query"))
		if q == "" {
			return "read", errors.New("query SQL wajib diisi")
		}
		scope, _ := classifyMCPSQL(q)
		return scope, nil
	case "docker_action":
		switch mArgString(args, "action") {
		case "start", "stop", "restart", "pause", "unpause":
			return "deploy", nil
		case "remove", "prune":
			return "admin", nil
		default:
			return "deploy", errors.New("aksi docker tidak dikenal")
		}
	}
	return mcpToolRegistry()[name].Scope, nil
}

// classifyMCPSQL menentukan scope minimum dan apakah pernyataan berisiko.
// Klasifikasi sengaja konservatif: WITH/EXPLAIN yang memuat DML tidak boleh
// dianggap baca, karena EXPLAIN ANALYZE dan CTE dapat mengeksekusi perubahan.
func classifyMCPSQL(query string) (scope string, risky bool) {
	words := sqlWords(query)
	if len(words) == 0 {
		return "admin", true
	}
	first := words[0]
	has := func(set map[string]bool) bool {
		for _, w := range words {
			if set[w] {
				return true
			}
		}
		return false
	}
	dml := map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "REPLACE": true, "MERGE": true, "UPSERT": true}
	ddl := map[string]bool{"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true, "GRANT": true, "REVOKE": true,
		"VACUUM": true, "ATTACH": true, "DETACH": true, "REINDEX": true, "CLUSTER": true, "COMMENT": true,
		"COPY": true, "DO": true, "CALL": true, "USE": true, "BEGIN": true, "COMMIT": true, "ROLLBACK": true}

	switch first {
	case "SELECT", "VALUES":
		scope = "read"
		if has(ddl) || has(map[string]bool{"INTO": true}) {
			scope = "admin"
		}
	case "PRAGMA":
		// Beberapa PRAGMA mengubah perilaku basis data; hanya admin.
		scope = "admin"
	case "SHOW", "DESCRIBE", "DESC":
		scope = "read"
	case "WITH":
		switch {
		case has(ddl):
			scope = "admin"
		case has(dml):
			scope = "data-write"
		default:
			scope = "read"
		}
	case "EXPLAIN":
		switch {
		case has(ddl):
			scope = "admin"
		case has(dml):
			scope = "data-write"
		default:
			scope = "read"
		}
	case "INSERT", "UPDATE", "DELETE", "REPLACE", "MERGE":
		scope = "data-write"
	default:
		scope = "admin"
	}
	if scope == "admin" {
		return scope, true
	}
	if has(map[string]bool{"DELETE": true, "UPDATE": true}) {
		where := false
		for _, w := range words {
			if w == "WHERE" {
				where = true
				break
			}
		}
		risky = !where
	}
	return scope, risky
}

// sqlWords mengabaikan komentar dan literal agar kata kunci di dalam string
// tidak mengubah klasifikasi. Identifier ber-quote dilewati.
func sqlWords(q string) []string {
	out := []string{}
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, strings.ToUpper(b.String()))
		b.Reset()
	}
	isWord := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
	}
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			flush()
			for i < len(q) && q[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			flush()
			i += 2
			for i+1 < len(q) && !(q[i] == '*' && q[i+1] == '/') {
				i++
			}
			if i+1 < len(q) {
				i += 2
			}
		case c == '\'' || c == '"' || c == '`':
			flush()
			quote := c
			i++
			for i < len(q) {
				if q[i] == '\\' && i+1 < len(q) {
					i += 2
					continue
				}
				if q[i] == quote {
					if i+1 < len(q) && q[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case isWord(c):
			b.WriteByte(c)
			i++
		default:
			flush()
			i++
		}
	}
	flush()
	return out
}

func mcpConfirm(args map[string]any, expected string) error {
	if !mArgBool(args, "confirm") {
		return errors.New("Aksi destruktif memerlukan confirm=true")
	}
	target := strings.TrimSpace(mArgString(args, "target"))
	if target == "" || target != expected {
		return fmt.Errorf("Target konfirmasi tidak cocok; ulangi target persis: %s", expected)
	}
	return nil
}

func mArgString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

func mArgInt(args map[string]any, key string) int64 {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case int:
		return int64(x)
	case int64:
		return x
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	}
	return 0
}

func mArgBool(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "1"
	}
	return false
}

func mArgStringSlice(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	out := []string{}
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			out = append(out, fmt.Sprint(item))
		}
	case []string:
		out = append(out, x...)
	case string:
		for _, item := range strings.Split(x, ",") {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func mcpTokenRecord(t *MCPToken) MCPToken {
	return *t
}

var _ = sql.ErrNoRows
