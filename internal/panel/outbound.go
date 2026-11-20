package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type telegramCredentials struct {
	Token  string `json:"token"`
	ChatID string `json:"chatId"`
}

type whatsappCredentials struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	Phone string `json:"phone"`
}

func (p *Panel) notifRules() map[string]bool {
	rules := map[string]bool{}
	_ = json.Unmarshal([]byte(p.setting("notif_rules", `{}`)), &rules)
	return rules
}

func (p *Panel) sendOutbound(kind, title, body string) {
	rules := p.notifRules()
	if !rules[kind] {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var tg telegramCredentials
		if p.getCredJSON("telegram", &tg) == nil && tg.Token != "" && tg.ChatID != "" {
			if err := sendTelegram(ctx, tg, title, body); err != nil {
				p.logf("telegram: %v", err)
			}
		}
		var wa whatsappCredentials
		if p.getCredJSON("whatsapp", &wa) == nil && wa.URL != "" {
			if err := sendWhatsApp(ctx, wa, title, body); err != nil {
				p.logf("whatsapp: %v", err)
			}
		}
	}()
}

func sendTelegram(ctx context.Context, c telegramCredentials, title, body string) error {
	text := "🔔 " + title
	if body != "" {
		text += "\n" + body
	}
	form := url.Values{}
	form.Set("chat_id", c.ChatID)
	form.Set("text", text)
	form.Set("disable_web_page_preview", "true")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+c.Token+"/sendMessage", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Telegram %s: %s", resp.Status, truncate(string(data), 200))
	}
	return nil
}

func sendWhatsApp(ctx context.Context, c whatsappCredentials, title, body string) error {
	payload := map[string]any{"target": c.Phone, "message": "🔔 " + title + "\n" + body}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", c.Token)
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("WhatsApp %s: %s", resp.Status, truncate(string(data), 200))
	}
	return nil
}

func (p *Panel) handleNotificationChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		var tg telegramCredentials
		_ = p.getCredJSON("telegram", &tg)
		var wa whatsappCredentials
		_ = p.getCredJSON("whatsapp", &wa)
		writeJSON(w, http.StatusOK, map[string]any{
			"telegram": map[string]any{"configured": tg.Token != "", "chatId": tg.ChatID, "hasToken": tg.Token != ""},
			"whatsapp": map[string]any{"configured": wa.URL != "", "url": wa.URL, "phone": wa.Phone, "hasToken": wa.Token != ""},
			"rules":    p.notifRules(),
		})
		return
	}
	var in struct {
		Channel string `json:"channel"`
		Token   string `json:"token"`
		ChatID  string `json:"chatId"`
		URL     string `json:"url"`
		Phone   string `json:"phone"`
		Keep    bool   `json:"keep"`
		Clear   bool   `json:"clear"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	switch in.Channel {
	case "telegram":
		if in.Clear {
			_, _ = p.db.Exec(`DELETE FROM credentials WHERE kind='telegram'`)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		old := telegramCredentials{}
		_ = p.getCredJSON("telegram", &old)
		if in.Keep && old.Token != "" {
			in.Token = old.Token
		}
		if strings.TrimSpace(in.Token) == "" || strings.TrimSpace(in.ChatID) == "" {
			fail(w, http.StatusBadRequest, "Bot token dan Chat ID wajib diisi")
			return
		}
		_ = p.saveCredJSON("telegram", "Telegram", telegramCredentials{Token: strings.TrimSpace(in.Token), ChatID: strings.TrimSpace(in.ChatID)})
	case "whatsapp":
		if in.Clear {
			_, _ = p.db.Exec(`DELETE FROM credentials WHERE kind='whatsapp'`)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		old := whatsappCredentials{}
		_ = p.getCredJSON("whatsapp", &old)
		if in.Keep && old.Token != "" {
			in.Token = old.Token
		}
		if strings.TrimSpace(in.URL) == "" {
			fail(w, http.StatusBadRequest, "URL API WhatsApp wajib diisi")
			return
		}
		_ = p.saveCredJSON("whatsapp", "WhatsApp gateway", whatsappCredentials{URL: strings.TrimSpace(in.URL), Token: in.Token, Phone: in.Phone})
	default:
		fail(w, http.StatusBadRequest, "Channel tidak dikenal")
		return
	}
	p.audit("notif.channel", "notification", in.Channel, "Kanal notifikasi "+in.Channel+" disimpan")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleNotificationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Channel string `json:"channel"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	switch in.Channel {
	case "telegram":
		var c telegramCredentials
		if err := p.getCredJSON("telegram", &c); err != nil {
			fail(w, http.StatusBadRequest, "Telegram belum dikonfigurasi")
			return
		}
		if err := sendTelegram(ctx, c, "Tes notifikasi SatriaPanel", "Kanal Telegram berfungsi."); err != nil {
			failDetail(w, http.StatusBadGateway, "Tes Telegram gagal", err.Error())
			return
		}
	case "whatsapp":
		var c whatsappCredentials
		if err := p.getCredJSON("whatsapp", &c); err != nil {
			fail(w, http.StatusBadRequest, "WhatsApp belum dikonfigurasi")
			return
		}
		if err := sendWhatsApp(ctx, c, "Tes notifikasi SatriaPanel", "Kanal WhatsApp berfungsi."); err != nil {
			failDetail(w, http.StatusBadGateway, "Tes WhatsApp gagal", err.Error())
			return
		}
	case "panel":
		p.notify("security", "Tes notifikasi panel", "Notifikasi panel berfungsi.", nil)
	default:
		fail(w, http.StatusBadRequest, "Channel tidak dikenal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
