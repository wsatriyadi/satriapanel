package panel

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type FileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"modTime"`
	Symlink bool   `json:"symlink"`
}

func evalExisting(path string) string {
	for {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

func (p *Panel) safeAppPath(appID int64, rel string) (string, *App, error) {
	app, err := p.getApp(appID)
	if err != nil {
		return "", nil, err
	}
	root := app.Workdir
	if root == "" {
		return "", nil, fmt.Errorf("folder kerja aplikasi belum tersedia")
	}
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	rel = strings.TrimPrefix(rel, "/")
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." {
		clean = ""
	}
	full := filepath.Join(root, clean)
	rootClean := filepath.Clean(root)
	if full != rootClean && !strings.HasPrefix(full, rootClean+string(os.PathSeparator)) {
		return "", nil, fmt.Errorf("path keluar dari folder aplikasi")
	}
	// Cegah symlink keluar dari root, termasuk bila berkas tujuan belum ada.
	resolvedRoot := evalExisting(rootClean)
	resolved := evalExisting(full)
	if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(os.PathSeparator)) {
		return "", nil, fmt.Errorf("path melalui symlink tidak diizinkan")
	}
	return full, app, nil
}

func (p *Panel) handleFiles(w http.ResponseWriter, r *http.Request) {
	appID := int64(qInt(r, "appId", 0))
	if appID == 0 {
		apps, _ := p.listApps()
		writeJSON(w, http.StatusOK, map[string]any{"apps": apps, "entries": []FileEntry{}, "path": ""})
		return
	}
	full, app, err := p.safeAppPath(appID, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Tidak dapat membaca folder", err.Error())
		return
	}
	out := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(app.Workdir, filepath.Join(full, e.Name()))
		out = append(out, FileEntry{
			Name: e.Name(), Path: filepath.ToSlash(rel), IsDir: e.IsDir(), Size: info.Size(),
			Mode: info.Mode().String(), ModTime: info.ModTime().UTC().Format(time.RFC3339), Symlink: info.Mode()&os.ModeSymlink != 0,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	relPath, _ := filepath.Rel(app.Workdir, full)
	if relPath == "." {
		relPath = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "path": filepath.ToSlash(relPath), "app": app, "root": app.Workdir})
}

func (p *Panel) handleFileContent(w http.ResponseWriter, r *http.Request) {
	appID := int64(qInt(r, "appId", 0))
	full, app, err := p.safeAppPath(appID, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			fail(w, http.StatusNotFound, "Berkas tidak ditemukan")
			return
		}
		if info.Size() > 4<<20 {
			fail(w, http.StatusBadRequest, "Berkas lebih dari 4 MB; gunakan unduh")
			return
		}
		b, err := os.ReadFile(full)
		if err != nil {
			failDetail(w, http.StatusBadRequest, "Gagal membaca berkas", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"content": string(b), "path": r.URL.Query().Get("path"),
			"mtime": info.ModTime().UTC().Format(time.RFC3339Nano), "size": info.Size(), "app": app.Name,
		})
	case http.MethodPut:
		var in struct {
			Content string `json:"content"`
			MTime   string `json:"mtime"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		info, err := os.Stat(full)
		if err == nil && in.MTime != "" {
			if info.ModTime().UTC().Format(time.RFC3339Nano) != in.MTime {
				fail(w, http.StatusConflict, "Berkas berubah sejak dibuka. Muat ulang sebelum menyimpan.")
				return
			}
		}
		_ = os.MkdirAll(filepath.Dir(full), 0o750)
		if err := os.WriteFile(full, []byte(in.Content), 0o640); err != nil {
			failDetail(w, http.StatusBadRequest, "Gagal menyimpan berkas", err.Error())
			return
		}
		p.audit("file.write", "app", fmt.Sprint(appID), "Berkas "+r.URL.Query().Get("path")+" disunting")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
	}
}

func (p *Panel) handleFileOp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AppID   int64  `json:"appId"`
		Op      string `json:"op"`
		Path    string `json:"path"`
		To      string `json:"to"`
		Name    string `json:"name"`
		Confirm string `json:"confirm"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	full, app, err := p.safeAppPath(in.AppID, in.Path)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	switch in.Op {
	case "mkdir":
		if strings.TrimSpace(in.Name) == "" || strings.Contains(in.Name, "/") || strings.Contains(in.Name, "\\") {
			fail(w, http.StatusBadRequest, "Nama folder tidak valid")
			return
		}
		target := filepath.Join(full, in.Name)
		if err := os.MkdirAll(target, 0o750); err != nil {
			failDetail(w, http.StatusBadRequest, "Gagal membuat folder", err.Error())
			return
		}
		p.audit("file.mkdir", "app", fmt.Sprint(app.ID), "Folder "+in.Path+"/"+in.Name+" dibuat")
	case "rename":
		if strings.TrimSpace(in.Name) == "" || strings.ContainsAny(in.Name, `/\`) || in.Name == "." || in.Name == ".." {
			fail(w, http.StatusBadRequest, "Nama baru tidak valid")
			return
		}
		target := filepath.Join(filepath.Dir(full), in.Name)
		if err := os.Rename(full, target); err != nil {
			failDetail(w, http.StatusBadRequest, "Gagal mengganti nama", err.Error())
			return
		}
		p.audit("file.rename", "app", fmt.Sprint(app.ID), "Berkas "+in.Path+" diganti menjadi "+in.Name)
	case "move":
		dest, _, err := p.safeAppPath(in.AppID, in.To)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := os.MkdirAll(dest, 0o750); err != nil {
			fail(w, http.StatusBadRequest, "Folder tujuan tidak valid")
			return
		}
		target := filepath.Join(dest, filepath.Base(full))
		if err := os.Rename(full, target); err != nil {
			failDetail(w, http.StatusBadRequest, "Gagal memindahkan", err.Error())
			return
		}
		p.audit("file.move", "app", fmt.Sprint(app.ID), "Berkas "+in.Path+" dipindahkan ke "+in.To)
	case "delete":
		if in.Confirm != "HAPUS" {
			fail(w, http.StatusBadRequest, "Ketik HAPUS untuk konfirmasi")
			return
		}
		if filepath.Clean(full) == filepath.Clean(app.Workdir) {
			fail(w, http.StatusBadRequest, "Folder akar aplikasi tidak dapat dihapus")
			return
		}
		if err := os.RemoveAll(full); err != nil {
			failDetail(w, http.StatusBadRequest, "Gagal menghapus", err.Error())
			return
		}
		p.audit("file.delete", "app", fmt.Sprint(app.ID), "Berkas "+in.Path+" dihapus")
	case "unzip":
		if err := unzipInto(full, app.Workdir); err != nil {
			failDetail(w, http.StatusBadRequest, "Gagal mengekstrak", err.Error())
			return
		}
		p.audit("file.unzip", "app", fmt.Sprint(app.ID), "Arsip "+in.Path+" diekstrak")
	default:
		fail(w, http.StatusBadRequest, "Operasi tidak dikenal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func unzipInto(zipPath, root string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	rootClean := filepath.Clean(root)
	for _, f := range r.File {
		target := filepath.Join(filepath.Dir(zipPath), f.Name)
		clean := filepath.Clean(target)
		if clean != rootClean && !strings.HasPrefix(clean, rootClean+string(os.PathSeparator)) {
			return fmt.Errorf("arsip berisi path tidak aman: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(clean, 0o750)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(clean), 0o750)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(clean, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 500<<20))
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Panel) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 250<<20)
	appID := int64(qInt(r, "appId", 0))
	dir, app, err := p.safeAppPath(appID, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Berkas unggahan tidak valid", err.Error())
		return
	}
	defer file.Close()
	name := filepath.Base(header.Filename)
	if name == "" || name == "." || name == ".." {
		fail(w, http.StatusBadRequest, "Nama berkas tidak valid")
		return
	}
	_ = os.MkdirAll(dir, 0o750)
	target := filepath.Join(dir, name)
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Gagal menyimpan berkas", err.Error())
		return
	}
	n, err := io.Copy(out, file)
	out.Close()
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Gagal menulis berkas", err.Error())
		return
	}
	p.audit("file.upload", "app", fmt.Sprint(app.ID), "Berkas "+name+" diunggah ("+fmtBytes(n)+")")
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "name": name, "size": n})
}

func (p *Panel) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	appID := int64(qInt(r, "appId", 0))
	full, _, err := p.safeAppPath(appID, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		fail(w, http.StatusNotFound, "Berkas tidak ditemukan")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(full)))
	http.ServeFile(w, r, full)
}

func (p *Panel) handleFileZip(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AppID int64  `json:"appId"`
		Path  string `json:"path"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	full, app, err := p.safeAppPath(in.AppID, in.Path)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil || !info.IsDir() {
		fail(w, http.StatusBadRequest, "Hanya folder yang dapat diarsipkan")
		return
	}
	tmp := filepath.Join(p.cfg.DataDir, "tmp", fmt.Sprintf("zip-%d-%d.zip", app.ID, time.Now().UnixNano()))
	out, err := os.Create(tmp)
	if err != nil {
		httpError(w, err)
		return
	}
	zw := zip.NewWriter(out)
	base := filepath.Dir(full)
	err = filepath.Walk(full, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(base, path)
		if fi.IsDir() {
			_, err := zw.Create(filepath.ToSlash(rel) + "/")
			return err
		}
		if fi.Size() > 500<<20 {
			return nil
		}
		h, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	zw.Close()
	out.Close()
	if err != nil {
		os.Remove(tmp)
		failDetail(w, http.StatusInternalServerError, "Gagal membuat arsip", err.Error())
		return
	}
	p.audit("file.zip", "app", fmt.Sprint(app.ID), "Folder "+in.Path+" diarsipkan")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(full)+".zip"))
	http.ServeFile(w, r, tmp)
	go func() {
		time.Sleep(5 * time.Minute)
		_ = os.Remove(tmp)
	}()
}

func (p *Panel) handleFilesMeta(w http.ResponseWriter, r *http.Request) {
	apps, _ := p.listApps()
	out := []map[string]any{}
	for _, a := range apps {
		out = append(out, map[string]any{"id": a.ID, "name": a.Name, "slug": a.Slug, "workdir": a.Workdir, "status": a.Status})
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out})
}

// dipakai untuk menampilkan isi file JSON pada UI file manager
func prettyJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
