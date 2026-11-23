//go:build linux

package gateway

import (
	"context"
	"net"
	"syscall"
)

func directResolver() *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		d := net.Dialer{Control: func(network, address string, raw syscall.RawConn) error {
			var e error
			if err := raw.Control(func(fd uintptr) {
				e = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, 65743)
				if device := uplink(); e == nil && device != "" && !isLoopbackAddress(address) {
					e = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, device)
				}
			}); err != nil {
				return err
			}
			return e
		}}
		if dns := currentTopology().DNS; dns != "" {
			address = net.JoinHostPort(dns, "53")
		}
		return d.DialContext(ctx, network, address)
	}}
}

func directDialer() *net.Dialer {
	return &net.Dialer{Control: func(network, address string, raw syscall.RawConn) error {
		var e error
		if err := raw.Control(func(fd uintptr) {
			e = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, 65743)
			if device := uplink(); e == nil && device != "" && !isLoopbackAddress(address) {
				e = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, device)
			}
		}); err != nil {
			return err
		}
		return e
	}}
}

func warpDialer() *net.Dialer {
	makeDial := func() *net.Dialer {
		return &net.Dialer{Control: func(network, address string, raw syscall.RawConn) error {
			var e error
			err := raw.Control(func(fd uintptr) {
				e = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, "CloudflareWARP")
			})
			if err != nil {
				return err
			}
			return e
		}}
	}
	d := makeDial()
	d.Resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return makeDial().DialContext(ctx, "udp", "1.1.1.1:53")
	}}
	return d
}

func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	return err == nil && net.ParseIP(host).IsLoopback()
}
