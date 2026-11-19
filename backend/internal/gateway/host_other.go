//go:build !linux

package gateway

import "errors"

func hostRaw(interfaces []any) (M, error) { return nil, errors.New("主机监控需要 Linux procfs") }
