//go:build !windows

package panel

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = syscall.Kill(pid, syscall.SIGTERM)
	time.Sleep(250 * time.Millisecond)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func killPortOwner(port int) {
	if port <= 0 {
		return
	}
	_ = exec.Command("sh", "-c", fmt.Sprintf("fuser -k -n tcp %d 2>/dev/null || lsof -ti tcp:%d 2>/dev/null | xargs -r kill -9 2>/dev/null || true", port, port)).Run()
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
