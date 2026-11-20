package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// mcpToolDef describes one entry in the MCP catalog. Scope is the minimum
// scope required to even see the tool; db_query and docker_action refine this
// dynamically per call.
type mcpToolDef struct {
	Name        string
	Description string
	Scope       string
	InputSchema map[string]any
}

var mcpRegistry map[string]mcpToolDef

func mcpSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": true}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func mcpP(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

func mcpEnum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}

var mcpAppArg = mcpP("string", "Slug atau ID numerik aplikasi")
var mcpConfirmArg = mcpP("boolean", "Wajib true untuk aksi destruktif")
var mcpTargetArg = mcpP("string", "Identifier target yang diulang persis untuk konfirmasi")

func mcpToolRegistry() map[string]mcpToolDef {
	if mcpRegistry != nil {
		return mcpRegistry
	}
	tools := []mcpToolDef{
		{
			Name: "server_status", Scope: "read",
			Description: "Status server: CPU, RAM, disk, load, uptime, versi panel, ringkasan aplikasi dan Redis.",
			InputSchema: mcpSchema(map[string]any{}),
		},
		{
			Name: "tunnel_status", Scope: "read",
			Description: "Status Cloudflare Tunnel, konfigurasi ingress, pemetaan port, dan mode sinkronisasi.",
			InputSchema: mcpSchema(map[string]any{}),
		},
		{
			Name: "tunnel_apply_ingress", Scope: "admin",
			Description: "Tulis konfigurasi ingress Cloudflare, validasi cloudflared, reload layanan, dan sinkronkan DNS. Destruktif: confirm=true dan target=Nama host panel.",
			InputSchema: mcpSchema(map[string]any{"confirm": mcpConfirmArg, "target": mcpTargetArg}, "confirm", "target"),
		},
		{
			Name: "apps_list", Scope: "read",
			Description: "Daftar seluruh aplikasi beserta status, port, runtime, dan health.",
			InputSchema: mcpSchema(map[string]any{}),
		},
		{
			Name: "app_get", Scope: "read",
			Description: "Detail aplikasi, rilis, deployment terakhir, dan variabel lingkungan tersamar.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg}, "app"),
		},
		{
			Name: "app_logs", Scope: "read",
			Description: "Tail log aplikasi. Output dibatasi agar aman untuk konteks.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg, "lines": mcpP("integer", "Jumlah baris 1-2000 (default 200)")}, "app"),
		},
		{
			Name: "app_deploy", Scope: "deploy",
			Description: "Mulai deploy asinkron. Kembalikan deployment ID; pantau dengan deploy_status.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg}, "app"),
		},
		{
			Name: "deploy_status", Scope: "read",
			Description: "Status deployment berdasarkan ID, atau deployment terbaru untuk aplikasi.",
			InputSchema: mcpSchema(map[string]any{"deploymentId": mcpP("integer", "ID deployment"), "app": mcpAppArg}),
		},
		{
			Name: "app_releases", Scope: "read",
			Description: "Riwayat rilis aplikasi.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg}, "app"),
		},
		{
			Name: "app_rollback", Scope: "deploy",
			Description: "Rollback aplikasi ke rilis tertentu dengan health check dan pemulihan otomatis.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg, "releaseId": mcpP("integer", "ID rilis tujuan")}, "app", "releaseId"),
		},
		{
			Name: "app_start", Scope: "deploy",
			Description: "Jalankan aplikasi yang berhenti.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg}, "app"),
		},
		{
			Name: "app_stop", Scope: "deploy",
			Description: "Hentikan aplikasi.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg}, "app"),
		},
		{
			Name: "app_restart", Scope: "deploy",
			Description: "Restart aplikasi.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg}, "app"),
		},
		{
			Name: "app_create", Scope: "admin",
			Description: "Buat aplikasi baru. Port kosong akan dialokasikan otomatis dari kolam panel.",
			InputSchema: mcpSchema(map[string]any{
				"name":           mcpP("string", "Nama aplikasi 2-64 karakter"),
				"slug":           mcpP("string", "Slug opsional"),
				"sourceType":     mcpEnum("Sumber kode", "github", "git_https", "git_ssh", "image"),
				"repoUrl":        mcpP("string", "URL repo Git"),
				"branch":         mcpP("string", "Branch (default main)"),
				"runtime":        mcpP("string", "Runtime: node, python, go, php, static, docker"),
				"runtimeVersion": mcpP("string", "Versi runtime"),
				"mode":           mcpEnum("Mode jalan", "native", "docker", "compose"),
				"driver":         mcpP("string", "Driver native: direct, pm2, systemd, supervisor"),
				"installCmd":     mcpP("string", "Perintah instalasi"),
				"buildCmd":       mcpP("string", "Perintah build"),
				"startCmd":       mcpP("string", "Perintah start"),
				"port":           mcpP("integer", "Port host; 0 = otomatis"),
				"hostname":       mcpP("string", "Hostname publik"),
				"autoDeploy":     mcpP("boolean", "Deploy otomatis dari webhook"),
				"persistentDirs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Folder persisten relatif"},
				"healthPath":     mcpP("string", "Path health check"),
				"image":          mcpP("string", "Image Docker"),
				"internalPort":   mcpP("integer", "Port internal container"),
				"composeService": mcpP("string", "Nama service compose"),
			}, "name"),
		},
		{
			Name: "app_delete", Scope: "admin",
			Description: "Hapus aplikasi beserta rilis dan data panel terkait. Destruktif: confirm=true dan target=slug aplikasi.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg, "deleteFiles": mcpP("boolean", "Hapus folder kerja aplikasi"), "confirm": mcpConfirmArg, "target": mcpTargetArg}, "app", "confirm", "target"),
		},
		{
			Name: "app_env_set", Scope: "admin",
			Description: "Ganti variabel lingkungan aplikasi. Nilai rahasia tidak pernah dikembalikan. Aplikasi perlu restart.",
			InputSchema: mcpSchema(map[string]any{
				"app": mcpAppArg,
				"vars": map[string]any{"type": "array", "description": "Daftar variabel", "items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"key": mcpP("string", "Nama variabel"), "value": mcpP("string", "Nilai"),
						"isSecret": mcpP("boolean", "Simpan terenkripsi"), "keep": mcpP("boolean", "Pertahankan nilai rahasia lama"),
					},
					"required": []string{"key"},
				}},
			}, "app", "vars"),
		},
		{
			Name: "db_list", Scope: "read",
			Description: "Daftar database terkelola dan koneksi manual. Password tidak pernah ditampilkan.",
			InputSchema: mcpSchema(map[string]any{}),
		},
		{
			Name: "db_schema", Scope: "read",
			Description: "Skema tabel, kolom, dan perkiraan jumlah baris untuk satu database.",
			InputSchema: mcpSchema(map[string]any{"database": mcpP("string", "Nama atau ID database")}, "database"),
		},
		{
			Name: "db_query", Scope: "read",
			Description: "Jalankan SQL. SELECT butuh read; INSERT/UPDATE/DELETE butuh data-write; DDL butuh admin. DDL dan DELETE/UPDATE tanpa WHERE wajib confirm=true + target=nama database.",
			InputSchema: mcpSchema(map[string]any{"database": mcpP("string", "Nama atau ID database"), "query": mcpP("string", "SQL"), "confirm": mcpConfirmArg, "target": mcpTargetArg}, "database", "query"),
		},
		{
			Name: "db_delete", Scope: "admin",
			Description: "Hapus pendaftaran database dan opsional berkas SQLite. Destruktif: confirm=true dan target=nama database.",
			InputSchema: mcpSchema(map[string]any{"database": mcpP("string", "Nama atau ID database"), "deleteFile": mcpP("boolean", "Hapus berkas SQLite"), "confirm": mcpConfirmArg, "target": mcpTargetArg}, "database", "confirm", "target"),
		},
		{
			Name: "redis_allocations", Scope: "read",
			Description: "Alokasi Redis per aplikasi, prefix ACL, dan status koneksi. Password tidak pernah ditampilkan.",
			InputSchema: mcpSchema(map[string]any{}),
		},
		{
			Name: "redis_keys", Scope: "read",
			Description: "Pindai key Redis hanya di dalam prefix milik aplikasi.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg, "pattern": mcpP("string", "Pola tambahan relatif terhadap prefix, mis. session:*"), "cursor": mcpP("integer", "Cursor SCAN"), "count": mcpP("integer", "Jumlah 10-500")}, "app"),
		},
		{
			Name: "redis_flush_app", Scope: "admin",
			Description: "Hapus seluruh key Redis milik satu aplikasi. Destruktif: confirm=true dan target=slug aplikasi.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg, "confirm": mcpConfirmArg, "target": mcpTargetArg}, "app", "confirm", "target"),
		},
		{
			Name: "docker_list", Scope: "read",
			Description: "Ringkasan Docker: container, image, volume, network, dan pemakaian disk.",
			InputSchema: mcpSchema(map[string]any{}),
		},
		{
			Name: "docker_action", Scope: "deploy",
			Description: "Aksi container. start/stop/restart/pause/unpause butuh deploy; remove/prune butuh admin + confirm=true + target persis.",
			InputSchema: mcpSchema(map[string]any{
				"action":      mcpEnum("Aksi", "start", "stop", "restart", "pause", "unpause", "remove", "prune"),
				"containerId": mcpP("string", "ID atau nama container"),
				"target":      mcpP("string", "Target prune: containers/images/volumes/networks/builder, atau ID container untuk remove"),
				"confirm":     mcpConfirmArg,
			}, "action"),
		},
		{
			Name: "cron_list", Scope: "read",
			Description: "Daftar cron, jadwal, status terakhir, dan jadwal berikutnya.",
			InputSchema: mcpSchema(map[string]any{}),
		},
		{
			Name: "cron_upsert", Scope: "cron",
			Description: "Buat atau ubah cron. Cron bawaan hanya boleh diubah jadwal/statusnya.",
			InputSchema: mcpSchema(map[string]any{
				"id": mcpP("integer", "ID cron untuk diubah"), "name": mcpP("string", "Nama cron"),
				"app": mcpAppArg, "command": mcpP("string", "Perintah"), "workdir": mcpP("string", "Direktori kerja"),
				"schedule": mcpP("string", "Jadwal cron 5 kolom, mis. 0 2 * * *"),
				"enabled":  mcpP("boolean", "Aktif"), "timeoutSeconds": mcpP("integer", "Timeout detik"),
			}),
		},
		{
			Name: "cron_run_now", Scope: "cron",
			Description: "Jalankan cron sekarang dan kembalikan ID run.",
			InputSchema: mcpSchema(map[string]any{"id": mcpP("integer", "ID cron")}, "id"),
		},
		{
			Name: "cron_runs", Scope: "read",
			Description: "Riwayat eksekusi cron beserta output.",
			InputSchema: mcpSchema(map[string]any{"id": mcpP("integer", "ID cron (opsional)"), "limit": mcpP("integer", "Jumlah 1-200")}),
		},
		{
			Name: "cron_delete", Scope: "cron",
			Description: "Hapus cron non-bawaan. Cron bawaan dilindungi.",
			InputSchema: mcpSchema(map[string]any{"id": mcpP("integer", "ID cron")}, "id"),
		},
		{
			Name: "file_list", Scope: "read",
			Description: "Daftar berkas di folder kerja aplikasi. Panel data dan symlink keluar diblokir.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg, "path": mcpP("string", "Path relatif")}, "app"),
		},
		{
			Name: "file_read", Scope: "read",
			Description: "Baca berkas teks aplikasi maksimal 4 MB. Berkas kredensial panel tidak dapat diakses.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg, "path": mcpP("string", "Path relatif")}, "app", "path"),
		},
		{
			Name: "file_write", Scope: "admin",
			Description: "Tulis berkas teks aplikasi. Destruktif: confirm=true dan target=path persis.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg, "path": mcpP("string", "Path relatif"), "content": mcpP("string", "Isi berkas"), "mtime": mcpP("string", "Mtime untuk cegah konflik"), "confirm": mcpConfirmArg, "target": mcpTargetArg}, "app", "path", "content", "confirm", "target"),
		},
		{
			Name: "file_delete", Scope: "admin",
			Description: "Hapus berkas atau folder aplikasi. Destruktif: confirm=true dan target=path persis.",
			InputSchema: mcpSchema(map[string]any{"app": mcpAppArg, "path": mcpP("string", "Path relatif"), "confirm": mcpConfirmArg, "target": mcpTargetArg}, "app", "path", "confirm", "target"),
		},
		{
			Name: "shell_run", Scope: "admin",
			Description: "Jalankan perintah shell dengan output dibatasi. Destruktif: confirm=true dan target=perintah persis. Dilarang membaca secret.key/panel.db dan data kredensial panel.",
			InputSchema: mcpSchema(map[string]any{"command": mcpP("string", "Perintah shell"), "app": mcpAppArg, "workdir": mcpP("string", "Direktori kerja relatif terhadap aplikasi"), "timeoutSeconds": mcpP("integer", "Timeout 1-300 detik"), "confirm": mcpConfirmArg, "target": mcpTargetArg}, "command", "confirm", "target"),
		},
		{
			Name: "backup_list", Scope: "read",
			Description: "Riwayat backup lokal dan Google Drive.",
			InputSchema: mcpSchema(map[string]any{}),
		},
		{
			Name: "backup_start", Scope: "backup",
			Description: "Mulai backup asinkron. Kembalikan backup ID; pantau dengan backup_status.",
			InputSchema: mcpSchema(map[string]any{}),
		},
		{
			Name: "backup_status", Scope: "read",
			Description: "Status dan detail satu backup.",
			InputSchema: mcpSchema(map[string]any{"backupId": mcpP("integer", "ID backup")}, "backupId"),
		},
		{
			Name: "backup_restore", Scope: "admin",
			Description: "Pulihkan aplikasi, panel, atau database dari backup dengan verifikasi checksum. Destruktif: confirm=true dan target=ID backup persis.",
			InputSchema: mcpSchema(map[string]any{"backupId": mcpP("integer", "ID backup"), "scope": mcpP("string", "app:<slug/id>, panel, atau database:<nama/id>"), "app": mcpAppArg, "database": mcpP("string", "Nama atau ID database"), "confirm": mcpConfirmArg, "target": mcpTargetArg}, "backupId", "scope", "confirm", "target"),
		},
		{
			Name: "audit_list", Scope: "read",
			Description: "Audit log panel termasuk panggilan MCP, dengan penanda sumber dan nama token.",
			InputSchema: mcpSchema(map[string]any{"q": mcpP("string", "Pencarian"), "action": mcpP("string", "Filter aksi"), "actor": mcpP("string", "Filter aktor"), "limit": mcpP("integer", "1-500"), "offset": mcpP("integer", "Offset")}),
		},
	}
	mcpRegistry = make(map[string]mcpToolDef, len(tools))
	for _, t := range tools {
		mcpRegistry[t.Name] = t
	}
	return mcpRegistry
}

func toolListForToken(t *MCPToken) []map[string]any {
	reg := mcpToolRegistry()
	names := make([]string, 0, len(reg))
	for name := range reg {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []map[string]any{}
	for _, name := range names {
		tool := reg[name]
		if !t.hasScope(tool.Scope) {
			continue
		}
		out = append(out, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		})
	}
	return out
}

func (p *Panel) executeMCPTool(ctx context.Context, t *MCPToken, name string, args map[string]any) (any, error) {
	switch name {
	case "server_status":
		info := p.collectServerInfo()
		return map[string]any{
			"server": info, "apps": p.appsSummary(), "redis": p.redisSummary(),
			"version": p.version, "baseUrl": p.setting("base_url", ""),
			"mcp": map[string]any{"enabled": p.boolSetting("mcp_enabled", true), "rateLimit": p.intSetting("mcp_rate_limit", 60)},
		}, nil

	case "tunnel_status":
		host, _ := os.Hostname()
		_, stErr := os.Stat(p.setting("cloudflared_config", "/etc/cloudflared/config.yml"))
		status := "belum dikonfigurasi"
		if stErr == nil {
			status = "konfigurasi tersedia"
		}
		return map[string]any{
			"panelHostname": p.setting("panel_hostname", "panel.satriyadi.com"),
			"configPath":    p.setting("cloudflared_config", "/etc/cloudflared/config.yml"),
			"configExists":  stErr == nil, "configStatus": status,
			"service": p.setting("cloudflared_service", "cloudflared"), "autoSync": p.boolSetting("cloudflare_auto_sync", true),
			"ports": p.portMappings(), "hostname": host,
		}, nil

	case "tunnel_apply_ingress":
		host := p.setting("panel_hostname", "panel.satriyadi.com")
		if err := mcpConfirm(args, host); err != nil {
			return nil, err
		}
		return p.mcpDo(http.MethodPost, "/api/integrations/cloudflare/apply", nil, map[string]any{"confirm": "TERAPKAN"}, p.handleCloudflareApply)

	case "apps_list":
		apps, err := p.listApps()
		if err != nil {
			return nil, err
		}
		return map[string]any{"apps": apps, "count": len(apps)}, nil

	case "app_get":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		releases, _ := p.listReleases(a.ID)
		deps, _ := p.listDeployments(a.ID, 10)
		env, _ := p.listEnv(a.ID)
		return map[string]any{"app": a, "releases": releases, "deployments": deps, "env": env}, nil

	case "app_logs":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		path, _, err := p.appLogPath(a.ID)
		if err != nil {
			return nil, err
		}
		lines := int(mArgInt(args, "lines"))
		if lines < 1 || lines > 2000 {
			lines = 200
		}
		tail := tailFile(path, lines)
		return map[string]any{"app": a.Slug, "lines": tail, "count": len(tail), "path": "logs/apps/" + a.Slug + ".log"}, nil

	case "app_deploy":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		dep, err := p.deployMgr.enqueue(a.ID, "mcp")
		if err != nil {
			return nil, err
		}
		return map[string]any{"deploymentId": dep.ID, "status": dep.Status, "app": a.Slug, "async": true}, nil

	case "deploy_status":
		if id := mArgInt(args, "deploymentId"); id > 0 {
			d, err := p.getDeployment(id)
			if err != nil {
				return nil, err
			}
			return map[string]any{"deployment": d}, nil
		}
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, errors.New("deploymentId atau app wajib diisi")
		}
		deps, err := p.listDeployments(a.ID, 20)
		if err != nil {
			return nil, err
		}
		return map[string]any{"app": a.Slug, "deployments": deps}, nil

	case "app_releases":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		rels, err := p.listReleases(a.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"app": a.Slug, "releases": rels}, nil

	case "app_rollback":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		relID := mArgInt(args, "releaseId")
		if relID == 0 {
			return nil, errors.New("releaseId wajib diisi")
		}
		return p.mcpDo(http.MethodPost, fmt.Sprintf("/api/apps/%d/rollback/%d", a.ID, relID),
			map[string]string{"id": strconv.FormatInt(a.ID, 10), "release": strconv.FormatInt(relID, 10)}, nil, p.handleRollback)

	case "app_start", "app_stop", "app_restart":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		action := strings.TrimPrefix(name, "app_")
		return p.mcpDo(http.MethodPost, fmt.Sprintf("/api/apps/%d/%s", a.ID, action),
			map[string]string{"id": strconv.FormatInt(a.ID, 10), "action": action}, nil, p.handleAppAction)

	case "app_create":
		in := appInput{
			Name: mArgString(args, "name"), Slug: mArgString(args, "slug"), SourceType: mArgString(args, "sourceType"),
			RepoURL: mArgString(args, "repoUrl"), Branch: mArgString(args, "branch"), Runtime: mArgString(args, "runtime"),
			RuntimeVersion: mArgString(args, "runtimeVersion"), Mode: mArgString(args, "mode"), Driver: mArgString(args, "driver"),
			InstallCmd: mArgString(args, "installCmd"), BuildCmd: mArgString(args, "buildCmd"), StartCmd: mArgString(args, "startCmd"),
			Port: int(mArgInt(args, "port")), Hostname: mArgString(args, "hostname"), AutoDeploy: mArgBool(args, "autoDeploy"),
			PersistentDirs: mArgStringSlice(args, "persistentDirs"), HealthPath: mArgString(args, "healthPath"),
			Image: mArgString(args, "image"), InternalPort: int(mArgInt(args, "internalPort")),
			ComposeService: mArgString(args, "composeService"),
		}
		return p.mcpDo(http.MethodPost, "/api/apps", nil, in, p.handleApps)

	case "app_delete":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		if err := mcpConfirm(args, a.Slug); err != nil {
			return nil, err
		}
		p.deleteApp(a, mArgBool(args, "deleteFiles"))
		return map[string]any{"ok": true, "deleted": a.Slug}, nil

	case "app_env_set":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		vars := []map[string]any{}
		if raw, ok := args["vars"].([]any); ok {
			for _, item := range raw {
				if m, ok := item.(map[string]any); ok {
					vars = append(vars, map[string]any{
						"key": mArgString(m, "key"), "value": mArgString(m, "value"),
						"isSecret": mArgBool(m, "isSecret"), "keep": mArgBool(m, "keep"),
					})
				}
			}
		} else {
			return nil, errors.New("vars harus berupa array {key,value,isSecret,keep}")
		}
		return p.mcpDo(http.MethodPut, fmt.Sprintf("/api/apps/%d/env", a.ID), map[string]string{"id": strconv.FormatInt(a.ID, 10)}, map[string]any{"vars": vars}, p.handleAppEnv)

	case "db_list":
		dbs := p.listDatabases()
		return map[string]any{"databases": dbs, "count": len(dbs)}, nil

	case "db_schema":
		d, password, err := p.mcpResolveDatabase(args)
		if err != nil {
			return nil, err
		}
		db, _, err := openDatabase(d, password)
		if err != nil {
			return nil, err
		}
		defer db.Close()
		tables, err := p.introspect(db, d)
		if err != nil {
			return nil, err
		}
		return map[string]any{"database": d.Name, "engine": d.Engine, "tables": tables}, nil

	case "db_query":
		d, _, err := p.mcpResolveDatabase(args)
		if err != nil {
			return nil, err
		}
		q := strings.TrimSpace(mArgString(args, "query"))
		if q == "" {
			return nil, errors.New("query wajib diisi")
		}
		kind, risky := classifyMCPSQL(q)
		ddl := kind == "admin"
		body := map[string]any{"sql": q}
		if ddl || risky {
			if err := mcpConfirm(args, d.Name); err != nil {
				return nil, err
			}
			body["confirm"] = "HAPUS"
		}
		return p.mcpDo(http.MethodPost, fmt.Sprintf("/api/databases/%d/query", d.ID), map[string]string{"id": strconv.FormatInt(d.ID, 10)}, body, p.handleDatabaseQuery)

	case "db_delete":
		d, _, err := p.mcpResolveDatabase(args)
		if err != nil {
			return nil, err
		}
		if err := mcpConfirm(args, d.Name); err != nil {
			return nil, err
		}
		target := fmt.Sprintf("/api/databases/%d?confirm=%s", d.ID, url.QueryEscape(d.Name))
		if mArgBool(args, "deleteFile") {
			target += "&deleteFile=1"
		}
		return p.mcpDo(http.MethodDelete, target, map[string]string{"id": strconv.FormatInt(d.ID, 10)}, nil, p.handleDatabase)

	case "redis_allocations":
		return p.mcpDo(http.MethodGet, "/api/redis", nil, nil, p.handleRedis)

	case "redis_keys":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		alloc, err := p.redisAllocation(a.ID)
		if err != nil {
			return nil, errors.New("aplikasi belum memiliki alokasi Redis")
		}
		pattern := strings.TrimSpace(mArgString(args, "pattern"))
		if pattern == "" {
			pattern = "*"
		}
		prefix := alloc.Prefix + strings.TrimPrefix(pattern, "*")
		if !strings.HasPrefix(prefix, alloc.Prefix) {
			return nil, errors.New("pola key harus berada di dalam prefix aplikasi")
		}
		q := url.Values{}
		q.Set("prefix", prefix)
		q.Set("cursor", strconv.FormatInt(mArgInt(args, "cursor"), 10))
		q.Set("count", strconv.FormatInt(mArgInt(args, "count"), 10))
		return p.mcpDo(http.MethodGet, "/api/redis/keys?"+q.Encode(), nil, nil, p.handleRedisKeys)

	case "redis_flush_app":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		if err := mcpConfirm(args, a.Slug); err != nil {
			return nil, err
		}
		alloc, err := p.redisAllocation(a.ID)
		if err != nil {
			return nil, errors.New("aplikasi belum memiliki alokasi Redis")
		}
		return p.mcpDo(http.MethodPost, "/api/redis/flush", nil, map[string]any{"prefix": alloc.Prefix, "confirm": "HAPUS"}, p.handleRedisFlush)

	case "docker_list":
		return map[string]any{"docker": p.dockerOverview()}, nil

	case "docker_action":
		action := mArgString(args, "action")
		switch action {
		case "start", "stop", "restart", "pause", "unpause":
			id := strings.TrimSpace(mArgString(args, "containerId"))
			if id == "" {
				id = strings.TrimSpace(mArgString(args, "target"))
			}
			if id == "" {
				return nil, errors.New("containerId wajib diisi")
			}
			return p.mcpDo(http.MethodPost, "/api/docker/containers/"+url.PathEscape(id)+"/"+action,
				map[string]string{"id": id, "action": action}, nil, p.handleDockerContainer)
		case "remove":
			id := strings.TrimSpace(mArgString(args, "containerId"))
			if id == "" {
				id = strings.TrimSpace(mArgString(args, "target"))
			}
			if id == "" {
				return nil, errors.New("containerId wajib diisi")
			}
			if err := mcpConfirm(args, id); err != nil {
				return nil, err
			}
			return p.mcpDo(http.MethodPost, "/api/docker/containers/"+url.PathEscape(id)+"/remove",
				map[string]string{"id": id, "action": "remove"}, nil, p.handleDockerContainer)
		case "prune":
			target := mArgString(args, "target")
			if target == "" {
				return nil, errors.New("target prune wajib diisi")
			}
			if err := mcpConfirm(args, target); err != nil {
				return nil, err
			}
			return p.mcpDo(http.MethodPost, "/api/docker", nil, map[string]any{"action": "prune", "target": target}, p.handleDocker)
		default:
			return nil, errors.New("aksi docker tidak dikenal")
		}

	case "cron_list":
		jobs, err := p.listCronJobs()
		if err != nil {
			return nil, err
		}
		return map[string]any{"jobs": jobs, "timezone": "UTC"}, nil

	case "cron_upsert":
		id := mArgInt(args, "id")
		schedule := strings.TrimSpace(mArgString(args, "schedule"))
		if schedule == "" && id > 0 {
			_ = p.db.QueryRow(`SELECT schedule_expr FROM cron_jobs WHERE id=?`, id).Scan(&schedule)
		}
		if schedule == "" {
			return nil, errors.New("schedule wajib diisi (5 kolom)")
		}
		if id > 0 {
			job, err := p.mcpCronJob(id)
			if err != nil {
				return nil, err
			}
			body := map[string]any{
				"name": mArgString(args, "name"), "command": mArgString(args, "command"),
				"schedule": schedule, "enabled": mArgBool(args, "enabled"),
				"timeoutSeconds": int(mArgInt(args, "timeoutSeconds")), "workdir": mArgString(args, "workdir"),
			}
			if body["name"] == "" {
				body["name"] = job.Name
			}
			if _, ok := args["enabled"]; !ok {
				body["enabled"] = job.Enabled
			}
			return p.mcpDo(http.MethodPut, fmt.Sprintf("/api/cron/%d", id), map[string]string{"id": strconv.FormatInt(id, 10)}, body, p.handleCronJob)
		}
		var appID int64
		if a, err := p.mcpResolveApp(args); err == nil {
			appID = a.ID
		}
		body := map[string]any{
			"name": mArgString(args, "name"), "appId": appID, "command": mArgString(args, "command"),
			"workdir": mArgString(args, "workdir"), "schedule": schedule,
			"enabled": mArgBool(args, "enabled"), "timeoutSeconds": int(mArgInt(args, "timeoutSeconds")),
		}
		if _, ok := args["enabled"]; !ok {
			body["enabled"] = true
		}
		return p.mcpDo(http.MethodPost, "/api/cron", nil, body, p.handleCron)

	case "cron_run_now":
		id := mArgInt(args, "id")
		if id == 0 {
			return nil, errors.New("id cron wajib diisi")
		}
		return p.mcpDo(http.MethodPost, fmt.Sprintf("/api/cron/%d/run", id), map[string]string{"id": strconv.FormatInt(id, 10)}, nil, p.handleCronRun)

	case "cron_runs":
		limit := int(mArgInt(args, "limit"))
		if limit < 1 || limit > 200 {
			limit = 50
		}
		runs := p.listCronRuns(mArgInt(args, "id"), limit)
		return map[string]any{"runs": runs, "count": len(runs)}, nil

	case "cron_delete":
		id := mArgInt(args, "id")
		if id == 0 {
			return nil, errors.New("id cron wajib diisi")
		}
		job, err := p.mcpCronJob(id)
		if err != nil {
			return nil, err
		}
		if job.BuiltinKey != "" {
			return nil, errors.New("cron bawaan panel tidak dapat dihapus; nonaktifkan saja")
		}
		return p.mcpDo(http.MethodDelete, fmt.Sprintf("/api/cron/%d", id), map[string]string{"id": strconv.FormatInt(id, 10)}, nil, p.handleCronJob)

	case "file_list":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		rel := mArgString(args, "path")
		q := url.Values{}
		q.Set("appId", strconv.FormatInt(a.ID, 10))
		q.Set("path", rel)
		return p.mcpDo(http.MethodGet, "/api/files?"+q.Encode(), nil, nil, p.handleFiles)

	case "file_read":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		rel := mArgString(args, "path")
		q := url.Values{}
		q.Set("appId", strconv.FormatInt(a.ID, 10))
		q.Set("path", rel)
		return p.mcpDo(http.MethodGet, "/api/files/content?"+q.Encode(), nil, nil, p.handleFileContent)

	case "file_write":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		rel := mArgString(args, "path")
		if err := mcpConfirm(args, rel); err != nil {
			return nil, err
		}
		content := mArgString(args, "content")
		if len(content) > 4<<20 {
			return nil, errors.New("isi berkas melebihi 4 MB")
		}
		q := url.Values{}
		q.Set("appId", strconv.FormatInt(a.ID, 10))
		q.Set("path", rel)
		return p.mcpDo(http.MethodPut, "/api/files/content?"+q.Encode(), nil, map[string]any{"content": content, "mtime": mArgString(args, "mtime")}, p.handleFileContent)

	case "file_delete":
		a, err := p.mcpResolveApp(args)
		if err != nil {
			return nil, err
		}
		rel := mArgString(args, "path")
		if err := mcpConfirm(args, rel); err != nil {
			return nil, err
		}
		body := map[string]any{"appId": a.ID, "op": "delete", "path": rel, "confirm": "HAPUS"}
		return p.mcpDo(http.MethodPost, "/api/files/op", nil, body, p.handleFileOp)

	case "shell_run":
		command := strings.TrimSpace(mArgString(args, "command"))
		if command == "" {
			return nil, errors.New("command wajib diisi")
		}
		if err := mcpConfirm(args, command); err != nil {
			return nil, err
		}
		low := strings.ToLower(command)
		for _, forbidden := range []string{"secret.key", "panel.db", "mcp_tokens", "credentials.json"} {
			if strings.Contains(low, forbidden) {
				return nil, fmt.Errorf("perintah ditolak: menyentuh berkas kredensial panel (%s)", forbidden)
			}
		}
		if strings.Contains(command, p.cfg.DataDir) {
			return nil, errors.New("perintah ditolak: tidak boleh menyentuh direktori data panel")
		}
		timeout := int(mArgInt(args, "timeoutSeconds"))
		if timeout < 1 || timeout > 300 {
			timeout = 60
		}
		workdir := ""
		if a, err := p.mcpResolveApp(args); err == nil {
			workdir = a.Workdir
			if rel := strings.TrimSpace(mArgString(args, "workdir")); rel != "" {
				full, _, err := p.safeAppPath(a.ID, rel)
				if err != nil {
					return nil, err
				}
				if info, err := os.Stat(full); err == nil && info.IsDir() {
					workdir = full
				}
			}
		}
		cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
		cmd := shellCommand(cctx, command)
		if workdir != "" {
			cmd.Dir = workdir
		}
		out, err := cmd.CombinedOutput()
		text := string(out)
		truncated := false
		if len(text) > 64<<10 {
			text = text[:64<<10] + "\n[output dipotong pada 64 KB]"
			truncated = true
		}
		res := map[string]any{"output": text, "exitCode": 0, "truncated": truncated, "timeoutSeconds": timeout}
		if err != nil {
			res["error"] = err.Error()
			if cctx.Err() == context.DeadlineExceeded {
				res["timeout"] = true
			} else {
				res["exitCode"] = 1
			}
		}
		return res, nil

	case "backup_list":
		bs := p.listBackups()
		return map[string]any{"backups": bs, "count": len(bs)}, nil

	case "backup_start":
		b, err := p.startBackupAsync("mcp")
		if err != nil {
			return nil, err
		}
		return map[string]any{"backupId": b.ID, "status": b.Status, "async": true}, nil

	case "backup_status":
		id := mArgInt(args, "backupId")
		if id == 0 {
			id = mArgInt(args, "id")
		}
		if id == 0 {
			return nil, errors.New("backupId wajib diisi")
		}
		b, err := p.getBackup(id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"backup": b}, nil

	case "backup_restore":
		id := mArgInt(args, "backupId")
		if id == 0 {
			id = mArgInt(args, "id")
		}
		if id == 0 {
			return nil, errors.New("backupId wajib diisi")
		}
		if err := mcpConfirm(args, strconv.FormatInt(id, 10)); err != nil {
			return nil, err
		}
		scope := strings.TrimSpace(mArgString(args, "scope"))
		var appID, dbID int64
		if strings.HasPrefix(scope, "app:") {
			val := strings.TrimPrefix(scope, "app:")
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				appID = n
			} else if a, err := p.mcpResolveApp(map[string]any{"app": val}); err == nil {
				appID = a.ID
			}
			if appID == 0 {
				return nil, errors.New("scope app tidak valid")
			}
			scope = "app:" + strconv.FormatInt(appID, 10)
		} else if strings.HasPrefix(scope, "database:") {
			val := strings.TrimPrefix(scope, "database:")
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				dbID = n
			} else if d, _, err := p.mcpResolveDatabase(map[string]any{"database": val}); err == nil {
				dbID = d.ID
			}
			if dbID == 0 {
				return nil, errors.New("scope database tidak valid")
			}
			scope = "database:" + strconv.FormatInt(dbID, 10)
		} else if scope != "panel" {
			return nil, errors.New("scope harus app:<id>, panel, atau database:<id>")
		}
		return p.mcpDo(http.MethodPost, fmt.Sprintf("/api/backups/%d/restore", id), map[string]string{"id": strconv.FormatInt(id, 10)},
			map[string]any{"confirm": "PULIHKAN", "scope": scope, "appId": appID, "databaseId": dbID}, p.handleBackupRestore)

	case "audit_list":
		q := url.Values{}
		for _, key := range []string{"q", "action", "actor", "limit", "offset"} {
			if v := mArgString(args, key); v != "" {
				q.Set(key, v)
			}
		}
		req := httptest.NewRequest(http.MethodGet, "/api/audit?"+q.Encode(), nil)
		logs, total := p.queryAudit(req)
		return map[string]any{"logs": logs, "total": total, "count": len(logs)}, nil
	}
	return nil, errors.New("tool belum diimplementasikan: " + name)
}

func (p *Panel) mcpResolveApp(args map[string]any) (*App, error) {
	raw := strings.TrimSpace(mArgString(args, "app"))
	if raw == "" {
		raw = strings.TrimSpace(mArgString(args, "appId"))
	}
	if raw == "" {
		return nil, errors.New("app wajib diisi (slug atau ID)")
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
		return p.getApp(id)
	}
	var id int64
	err := p.db.QueryRow(`SELECT id FROM apps WHERE slug=? OR name=? COLLATE NOCASE LIMIT 1`, raw, raw).Scan(&id)
	if err != nil {
		return nil, errors.New("aplikasi tidak ditemukan: " + raw)
	}
	return p.getApp(id)
}

func (p *Panel) mcpResolveDatabase(args map[string]any) (*Database, string, error) {
	raw := strings.TrimSpace(mArgString(args, "database"))
	if raw == "" {
		raw = strings.TrimSpace(mArgString(args, "databaseId"))
	}
	if raw == "" {
		return nil, "", errors.New("database wajib diisi (nama atau ID)")
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
		return p.getDatabase(id)
	}
	var id int64
	if err := p.db.QueryRow(`SELECT id FROM databases WHERE name=? COLLATE NOCASE LIMIT 1`, raw).Scan(&id); err != nil {
		return nil, "", errors.New("database tidak ditemukan: " + raw)
	}
	return p.getDatabase(id)
}

func (p *Panel) mcpCronJob(id int64) (*CronJob, error) {
	var j CronJob
	var appID interface{}
	var enabled int
	var builtin interface{}
	err := p.db.QueryRow(`SELECT id,name,kind,app_id,command,workdir,schedule_expr,enabled,timeout_seconds,COALESCE(builtin_key,'') FROM cron_jobs WHERE id=?`, id).
		Scan(&j.ID, &j.Name, &j.Kind, &appID, &j.Command, &j.Workdir, &j.Schedule, &enabled, &j.Timeout, &builtin)
	if err != nil {
		return nil, errors.New("cron tidak ditemukan")
	}
	j.Enabled = enabled == 1
	j.BuiltinKey = fmt.Sprint(builtin)
	if j.BuiltinKey == "<nil>" {
		j.BuiltinKey = ""
	}
	return &j, nil
}

// mcpDo invokes an existing HTTP handler in-process. This reuses validation,
// transactions, audit records, and notifications without exposing a session.
func (p *Panel) mcpDo(method, target string, pathValues map[string]string, body any, handler http.HandlerFunc) (map[string]any, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if res.StatusCode >= 400 {
		msg := "permintaan gagal"
		if out != nil {
			if v, ok := out["error"].(string); ok && v != "" {
				msg = v
			}
			if v, ok := out["detail"].(string); ok && v != "" {
				msg += ": " + v
			}
		}
		return out, errors.New(msg)
	}
	if out == nil {
		out = map[string]any{"ok": true, "raw": string(raw)}
	}
	out["httpStatus"] = res.StatusCode
	return out, nil
}

func mcpSafeArgs(args map[string]any) string {
	safe := map[string]any{}
	for k, v := range args {
		lk := strings.ToLower(k)
		switch {
		case strings.Contains(lk, "secret"), strings.Contains(lk, "password"), strings.Contains(lk, "token"),
			strings.Contains(lk, "content"), lk == "vars", lk == "command":
			safe[k] = "(disamarkan)"
		default:
			safe[k] = v
		}
	}
	raw, _ := json.Marshal(safe)
	return truncate(string(raw), 500)
}

var _ = filepath.Separator
