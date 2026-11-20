//go:build !windows

package panel

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type ptyTerm struct {
	cmd  *exec.Cmd
	file *os.File
}

func startTerminal(cols, rows int) (terminalIO, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell, "-l")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &ptyTerm{cmd: cmd, file: f}, nil
}

func (t *ptyTerm) Read(p []byte) (int, error)  { return t.file.Read(p) }
func (t *ptyTerm) Write(p []byte) (int, error) { return t.file.Write(p) }
func (t *ptyTerm) Close() error {
	_ = t.file.Close()
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	return nil
}
func (t *ptyTerm) Wait() error { return t.cmd.Wait() }
func (t *ptyTerm) Resize(cols, rows int) error {
	return pty.Setsize(t.file, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
