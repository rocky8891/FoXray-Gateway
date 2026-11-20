//go:build !linux

package gateway

import "net"

func enableDNSOriginalDestination(conn *net.UDPConn) error { return nil }
func dnsOriginalUDP(oob []byte) string                     { return "" }
func dnsOriginalTCP(conn net.Conn) string                  { return "" }

func dnsOriginalConntrack(peer string) string { return "" }
