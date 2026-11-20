// SatriaPanel — panel deploy & manajemen aplikasi untuk server sendiri.
// Satu binary Go: core/agent + aset antarmuka web tertanam + data SQLite.
package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"satriapanel/internal/panel"
)

//go:embed all:web
var webAssets embed.FS

const version = "0.3.0"

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	dataDir := flag.String("data", env("SATRIA_DATA", "./data"), "folder data panel (SQLite, log, backup)")
	addr := flag.String("addr", env("SATRIA_ADDR", "127.0.0.1:8282"), "alamat dengar panel (default hanya localhost, diakses lewat tunnel)")
	baseURL := flag.String("base-url", env("SATRIA_BASE_URL", ""), "URL publik panel, mis. https://panel.satriyadi.com")
	demo := flag.Bool("demo", env("SATRIA_DEMO", "") == "1", "isi data contoh saat basis data masih kosong")
	flag.Parse()

	cfg := panel.Config{
		Addr:    *addr,
		DataDir: *dataDir,
		BaseURL: *baseURL,
		Version: version,
		Demo:    *demo,
	}

	p, err := panel.New(cfg, webAssets, "web")
	if err != nil {
		log.Fatalf("satriapanel: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := p.Init(ctx); err != nil {
		log.Fatalf("satriapanel: %v", err)
	}

	log.Printf("SatriaPanel %s (%s/%s) — data: %s", version, runtime.GOOS, runtime.GOARCH, p.DataDir())
	log.Printf("antarmuka: http://%s", *addr)
	if err := p.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("satriapanel: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
}
