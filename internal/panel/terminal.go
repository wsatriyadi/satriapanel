package panel

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type terminalIO interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Close() error
	Wait() error
}

var termUpgrader = websocket.Upgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		host := r.Host
		return strings.Contains(origin, "://"+host) || strings.HasSuffix(origin, "."+host)
	},
}

type termMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (p *Panel) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u == nil {
		fail(w, http.StatusUnauthorized, "Sesi tidak sah")
		return
	}
	cols, rows := qInt(r, "cols", 110), qInt(r, "rows", 32)
	if cols < 20 || cols > 400 {
		cols = 110
	}
	if rows < 8 || rows > 200 {
		rows = 32
	}
	term, err := startTerminal(cols, rows)
	if err != nil {
		failDetail(w, http.StatusInternalServerError, "Gagal membuka terminal", err.Error())
		return
	}
	defer term.Close()
	conn, err := termUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	p.auditWithIP("terminal.open", "panel", "", "Sesi terminal web dibuka", remoteIP(r))
	defer p.auditWithIP("terminal.close", "panel", "", "Sesi terminal web ditutup", remoteIP(r))

	var writeMu sync.Mutex
	writeJSONMsg := func(msg termMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(msg)
	}
	_ = writeJSONMsg(termMessage{Type: "ready", Data: "Terminal terhubung. Sesi ini tercatat di audit log.\r\n"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 32<<10)
		for {
			n, err := term.Read(buf)
			if n > 0 {
				if writeJSONMsg(termMessage{Type: "output", Data: string(buf[:n])}) != nil {
					return
				}
			}
			if err != nil {
				_ = writeJSONMsg(termMessage{Type: "exit", Data: err.Error()})
				return
			}
		}
	}()

	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Minute))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(30 * time.Minute))
	})
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ping.C:
				_ = conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
			}
		}
	}()

	for {
		var msg termMessage
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Minute))
		switch msg.Type {
		case "input":
			if len(msg.Data) > 65536 {
				continue
			}
			_, _ = term.Write([]byte(msg.Data))
		case "resize":
			if msg.Cols >= 20 && msg.Rows >= 8 {
				_ = term.Resize(msg.Cols, msg.Rows)
			}
		case "ping":
			_ = writeJSONMsg(termMessage{Type: "pong"})
		}
	}
	select {
	case <-done:
	default:
	}
}
