package panel

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisAllocation struct {
	ID        int64  `json:"id"`
	AppID     int64  `json:"appId"`
	AppName   string `json:"appName"`
	Slug      string `json:"slug"`
	ACLUser   string `json:"aclUser"`
	Prefix    string `json:"prefix"`
	CreatedAt string `json:"createdAt"`
}

func (p *Panel) redisHost() string {
	return p.setting("redis_host", "127.0.0.1")
}

func (p *Panel) redisPort() int {
	return p.intSetting("redis_port", 6379)
}

func (p *Panel) redisPassword() string {
	return p.decrypt(p.setting("redis_password", ""))
}

func (p *Panel) redisClient(db int) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", p.redisHost(), p.redisPort()),
		Password:     p.redisPassword(),
		DB:           db,
		DialTimeout:  900 * time.Millisecond,
		ReadTimeout:  4 * time.Second,
		WriteTimeout: 4 * time.Second,
		MaxRetries:   -1,
	})
}

func (p *Panel) redisAllocation(appID int64) (*RedisAllocation, error) {
	var a RedisAllocation
	err := p.db.QueryRow(`SELECT r.id,r.app_id,COALESCE(a.name,''),COALESCE(a.slug,''),r.acl_user,r.prefix,r.created_at
		FROM redis_allocations r LEFT JOIN apps a ON a.id=r.app_id WHERE r.app_id=?`, appID).
		Scan(&a.ID, &a.AppID, &a.AppName, &a.Slug, &a.ACLUser, &a.Prefix, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (p *Panel) redisSummary() map[string]any {
	out := map[string]any{"configured": p.boolSetting("redis_enabled", false), "host": p.redisHost(), "port": p.redisPort(), "online": false, "service": p.redisServiceStatus()}
	if !p.boolSetting("redis_enabled", false) {
		out["error"] = "Redis belum diaktifkan. Atur koneksi di halaman Redis."
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	c := p.redisClient(0)
	defer c.Close()
	info, err := c.Info(ctx, "server").Result()
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	out["online"] = true
	out["configured"] = true
	out["version"] = redisInfoValue(info, "redis_version")
	out["mode"] = redisInfoValue(info, "redis_mode")
	out["uptimeSec"], _ = strconv.ParseInt(redisInfoValue(info, "uptime_in_seconds"), 10, 64)
	mem, err := c.Info(ctx, "memory").Result()
	if err == nil {
		used, _ := strconv.ParseUint(redisInfoValue(mem, "used_memory"), 10, 64)
		max, _ := strconv.ParseUint(redisInfoValue(mem, "maxmemory"), 10, 64)
		out["memoryUsed"] = used
		out["memoryMax"] = max
		if max > 0 {
			out["memoryPercent"] = round2(float64(used) / float64(max) * 100)
		}
	}
	keys, _ := c.DBSize(ctx).Result()
	out["keys"] = keys
	clients, err := c.Info(ctx, "clients").Result()
	if err == nil {
		out["clients"], _ = strconv.ParseInt(redisInfoValue(clients, "connected_clients"), 10, 64)
	}
	stats, err := c.Info(ctx, "stats").Result()
	if err == nil {
		out["ops"] = redisInfoValue(stats, "instantaneous_ops_per_sec")
		out["hits"], _ = strconv.ParseInt(redisInfoValue(stats, "keyspace_hits"), 10, 64)
		out["misses"], _ = strconv.ParseInt(redisInfoValue(stats, "keyspace_misses"), 10, 64)
	}
	return out
}

func redisInfoValue(info, key string) string {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, key+":"))
		}
	}
	return ""
}

func (p *Panel) redisMemoryPercent() float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	c := p.redisClient(0)
	defer c.Close()
	info, err := c.Info(ctx, "memory").Result()
	if err != nil {
		return 0
	}
	used, _ := strconv.ParseFloat(redisInfoValue(info, "used_memory"), 64)
	max, _ := strconv.ParseFloat(redisInfoValue(info, "maxmemory"), 64)
	if max <= 0 {
		return 0
	}
	return round2(used / max * 100)
}

func (p *Panel) redisServiceStatus() string {
	if runtime.GOOS != "linux" {
		return "tidak tersedia di sistem ini"
	}
	for _, svc := range []string{"redis-server", "redis"} {
		out, err := runCmdOutput(context.Background(), "systemctl is-active "+svc, 5*time.Second)
		if err == nil {
			s := strings.TrimSpace(out)
			if s != "" {
				return s
			}
		}
		if strings.TrimSpace(out) == "active" {
			return out
		}
	}
	return "tidak aktif"
}

func (p *Panel) redisAdminClient() *redis.Client {
	return p.redisClient(0)
}

func (p *Panel) redisPing() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c := p.redisAdminClient()
	defer c.Close()
	return c.Ping(ctx).Result()
}

func (p *Panel) handleRedis(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		p.redisMu.Lock()
		defer p.redisMu.Unlock()
		summary := p.redisSummary()
		rows, _ := p.db.Query(`SELECT r.id,r.app_id,COALESCE(a.name,''),COALESCE(a.slug,''),r.acl_user,r.prefix,r.created_at
			FROM redis_allocations r LEFT JOIN apps a ON a.id=r.app_id ORDER BY r.id`)
		alloc := []RedisAllocation{}
		if rows != nil {
			for rows.Next() {
				var a RedisAllocation
				if rows.Scan(&a.ID, &a.AppID, &a.AppName, &a.Slug, &a.ACLUser, &a.Prefix, &a.CreatedAt) == nil {
					alloc = append(alloc, a)
				}
			}
			rows.Close()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": summary, "allocations": alloc,
			"config": map[string]any{"host": p.redisHost(), "port": p.redisPort(), "hasPassword": p.redisPassword() != "", "prefixPattern": "app_<slug>:*"},
		})
		return
	}
	fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
}

func (p *Panel) handleRedisConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"host": p.redisHost(), "port": p.redisPort(), "hasPassword": p.redisPassword() != ""})
		return
	}
	var in struct {
		Host         string `json:"host"`
		Port         int    `json:"port"`
		Password     string `json:"password"`
		KeepPassword bool   `json:"keepPassword"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Port <= 0 || in.Port > 65535 {
		fail(w, http.StatusBadRequest, "Port Redis tidak valid")
		return
	}
	_ = p.setSetting("redis_host", defaultStr(in.Host, "127.0.0.1"))
	_ = p.setSetting("redis_port", strconv.Itoa(in.Port))
	if !in.KeepPassword {
		if in.Password == "" {
			_ = p.setSetting("redis_password", "")
		} else {
			enc, _ := p.encrypt(in.Password)
			_ = p.setSetting("redis_password", enc)
		}
	}
	p.audit("redis.config", "redis", "", "Konfigurasi koneksi Redis diubah")
	ping, err := p.redisPing()
	if err != nil {
		_ = p.setSetting("redis_enabled", "false")
		failDetail(w, http.StatusBadGateway, "Koneksi Redis gagal", err.Error())
		return
	}
	_ = p.setSetting("redis_enabled", "true")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ping": ping})
}

func (p *Panel) handleRedisAllocations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	var in struct {
		AppID int64 `json:"appId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	app, err := p.getApp(in.AppID)
	if err != nil {
		fail(w, http.StatusNotFound, "Aplikasi tidak ditemukan")
		return
	}
	username := "app_" + strings.ReplaceAll(app.Slug, "-", "_")
	if len(username) > 60 {
		username = username[:60]
	}
	prefix := "app_" + app.Slug + ":"
	password := randomToken(24)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := p.redisAdminClient()
	defer c.Close()
	if err := c.Do(ctx, "ACL", "SETUSER", username, "reset", "on", ">"+password, "~"+prefix+"*", "&*", "+@all").Err(); err != nil {
		failDetail(w, http.StatusBadGateway, "Gagal membuat ACL Redis", err.Error())
		return
	}
	_ = c.Do(ctx, "ACL", "SAVE").Err()
	enc, _ := p.encrypt(password)
	now := nowUTC()
	_, err = p.db.Exec(`INSERT INTO redis_allocations(app_id,acl_user,acl_password_enc,prefix,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(app_id) DO UPDATE SET acl_user=excluded.acl_user,acl_password_enc=excluded.acl_password_enc,prefix=excluded.prefix`,
		app.ID, username, enc, prefix, now)
	if err != nil {
		httpError(w, err)
		return
	}
	_, _ = p.db.Exec(`UPDATE apps SET needs_restart=1, updated_at=? WHERE id=?`, now, app.ID)
	p.audit("redis.allocate", "app", fmt.Sprint(app.ID), "Alokasi Redis "+username+" ("+prefix+"*) dibuat")
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "aclUser": username, "prefix": prefix, "note": "Password hanya ditampilkan sekali melalui variabel lingkungan aplikasi."})
}

func (p *Panel) handleRedisAllocation(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var appID int64
	var username string
	if err := p.db.QueryRow(`SELECT app_id,acl_user FROM redis_allocations WHERE id=?`, id).Scan(&appID, &username); err != nil {
		httpError(w, errNotFound)
		return
	}
	if r.Method != http.MethodDelete {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	c := p.redisAdminClient()
	defer c.Close()
	_ = c.Do(ctx, "ACL", "DELUSER", username).Err()
	_ = c.Do(ctx, "ACL", "SAVE").Err()
	_, _ = p.db.Exec(`DELETE FROM redis_allocations WHERE id=?`, id)
	_, _ = p.db.Exec(`UPDATE apps SET needs_restart=1, updated_at=? WHERE id=?`, nowUTC(), appID)
	p.audit("redis.revoke", "app", fmt.Sprint(appID), "Alokasi Redis "+username+" dicabut")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleRedisKeys(w http.ResponseWriter, r *http.Request) {
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	if prefix == "" {
		prefix = "*"
	}
	if !strings.HasSuffix(prefix, "*") {
		prefix += "*"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := p.redisAdminClient()
	defer c.Close()
	cursor, _ := strconv.ParseUint(r.URL.Query().Get("cursor"), 10, 64)
	count := int64(qInt(r, "count", 100))
	if count < 10 || count > 500 {
		count = 100
	}
	keys, next, err := c.Scan(ctx, cursor, prefix, count).Result()
	if err != nil {
		failDetail(w, http.StatusBadGateway, "Gagal memindai Redis", err.Error())
		return
	}
	type keyInfo struct {
		Key  string `json:"key"`
		Type string `json:"type"`
		TTL  int64  `json:"ttl"`
		Size int64  `json:"size"`
	}
	out := make([]keyInfo, 0, len(keys))
	for _, k := range keys {
		t, _ := c.Type(ctx, k).Result()
		ttl, _ := c.TTL(ctx, k).Result()
		var size int64
		switch t {
		case "string":
			size, _ = c.StrLen(ctx, k).Result()
		case "list":
			size, _ = c.LLen(ctx, k).Result()
		case "set":
			size, _ = c.SCard(ctx, k).Result()
		case "zset":
			size, _ = c.ZCard(ctx, k).Result()
		case "hash":
			size, _ = c.HLen(ctx, k).Result()
		case "stream":
			size, _ = c.XLen(ctx, k).Result()
		}
		out = append(out, keyInfo{Key: k, Type: t, TTL: int64(ttl.Seconds()), Size: size})
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out, "cursor": next, "prefix": strings.TrimSuffix(prefix, "*")})
}

func (p *Panel) handleRedisKey(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		key = strings.TrimSpace(r.PathValue("key"))
	}
	if key == "" {
		fail(w, http.StatusBadRequest, "Key wajib diisi")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := p.redisAdminClient()
	defer c.Close()
	switch r.Method {
	case http.MethodGet:
		t, err := c.Type(ctx, key).Result()
		if err != nil {
			failDetail(w, http.StatusBadGateway, "Gagal membaca key", err.Error())
			return
		}
		if t == "none" {
			fail(w, http.StatusNotFound, "Key tidak ditemukan")
			return
		}
		ttl, _ := c.TTL(ctx, key).Result()
		val := any(nil)
		switch t {
		case "string":
			val, _ = c.Get(ctx, key).Result()
		case "list":
			val, _ = c.LRange(ctx, key, 0, 99).Result()
		case "set":
			val, _ = c.SMembers(ctx, key).Result()
		case "zset":
			val, _ = c.ZRangeWithScores(ctx, key, 0, 99).Result()
		case "hash":
			val, _ = c.HGetAll(ctx, key).Result()
		}
		writeJSON(w, http.StatusOK, map[string]any{"key": key, "type": t, "ttl": int64(ttl.Seconds()), "value": val})
	case http.MethodDelete:
		n, err := c.Del(ctx, key).Result()
		if err != nil {
			failDetail(w, http.StatusBadGateway, "Gagal menghapus key", err.Error())
			return
		}
		p.audit("redis.key_delete", "redis", key, "Key Redis dihapus")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": n})
	case http.MethodPut:
		var in struct {
			TTL int64 `json:"ttl"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if in.TTL == 0 {
			_ = c.Persist(ctx, key).Err()
		} else {
			_ = c.Expire(ctx, key, time.Duration(in.TTL)*time.Second).Err()
		}
		p.audit("redis.key_ttl", "redis", key, "TTL key Redis diubah")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
	}
}

func (p *Panel) handleRedisFlush(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prefix  string `json:"prefix"`
		Confirm string `json:"confirm"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Prefix) == "" || in.Confirm != "HAPUS" {
		fail(w, http.StatusBadRequest, "Ketik HAPUS untuk konfirmasi; prefix wajib diisi")
		return
	}
	prefix := strings.TrimSuffix(in.Prefix, "*") + "*"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := p.redisAdminClient()
	defer c.Close()
	var cursor uint64
	deleted := 0
	for {
		keys, next, err := c.Scan(ctx, cursor, prefix, 200).Result()
		if err != nil {
			failDetail(w, http.StatusBadGateway, "Gagal memindai key", err.Error())
			return
		}
		if len(keys) > 0 {
			n, _ := c.Del(ctx, keys...).Result()
			deleted += int(n)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	p.audit("redis.flush", "redis", prefix, fmt.Sprintf("%d key dengan prefix %s dihapus", deleted, prefix))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
}

func (p *Panel) handleRedisService(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "linux" {
		fail(w, http.StatusBadRequest, "Aksi layanan Redis hanya tersedia di Linux")
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Action != "start" && in.Action != "stop" && in.Action != "restart" {
		fail(w, http.StatusBadRequest, "Aksi tidak dikenal")
		return
	}
	out, err := runCmdOutput(context.Background(), "systemctl "+in.Action+" redis-server", 30*time.Second)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Aksi layanan gagal", strings.TrimSpace(out))
		return
	}
	p.audit("redis.service", "redis", "", "Layanan Redis: "+in.Action)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": strings.TrimSpace(out)})
}
