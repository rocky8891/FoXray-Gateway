//go:build !linux

package gateway

import "os"

func securePlan(s os.FileInfo) bool        { return s.Mode().IsRegular() }
func lockPlan(path string) (func(), error) { return func() {}, nil }
