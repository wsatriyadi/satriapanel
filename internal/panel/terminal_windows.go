//go:build windows

package panel

import (
	"io"
	"os/exec"
	"sync"
)

type pipeTerm struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	once   sync.Once
}

func startTerminal(cols, rows int) (terminalIO, error) {
	cmd := exec.Command("cmd.exe")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &pipeTerm{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}

func (t *pipeTerm) Read(p []byte) (int, error)  { return t.stdout.Read(p) }
func (t *pipeTerm) Write(p []byte) (int, error) { return t.stdin.Write(p) }
func (t *pipeTerm) Resize(cols, rows int) error { return nil }
func (t *pipeTerm) Wait() error                 { return t.cmd.Wait() }
func (t *pipeTerm) Close() error {
	var err error
	t.once.Do(func() {
		_ = t.stdin.Close()
		if t.cmd.Process != nil {
			err = t.cmd.Process.Kill()
		}
	})
	return err
}
