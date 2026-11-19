//go:build !linux

package gateway

import "net"

func directResolver() *net.Resolver { return net.DefaultResolver }

func directDialer() *net.Dialer { return &net.Dialer{} }

func warpDialer() *net.Dialer { return &net.Dialer{} }
