//go:build windows

package safety

// processAlive conservatively reports true: Windows has no signal-0 probe.
// Stale locks are taken over by age instead (lockMaxAge).
func processAlive(pid int) bool { return pid > 0 }
