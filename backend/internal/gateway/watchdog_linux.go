//go:build linux

package gateway

import (
	"os"
	"syscall"
)

func securePlan(s os.FileInfo) bool {
	st, ok := s.Sys().(*syscall.Stat_t)
	return ok && st.Uid == 0 && s.Mode().Perm()&0077 == 0
}
func lockPlan(path string) (func(), error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); e != nil {
		f.Close()
		return nil, e
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
