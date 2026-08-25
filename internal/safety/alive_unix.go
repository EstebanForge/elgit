//go:build !windows

package safety

import (
	"errors"
	"syscall"
)

// processAlive probes a pid with signal 0.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
