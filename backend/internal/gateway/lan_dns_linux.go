//go:build linux

package gateway

import (
	"encoding/binary"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"net"
	"runtime"
	"strconv"
	"unsafe"
)

func enableDNSOriginalDestination(conn *net.UDPConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var controlErr error
	err = raw.Control(func(fd uintptr) { controlErr = unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_RECVORIGDSTADDR, 1) })
	if err != nil {
		return err
	}
	return controlErr
}
func dnsOriginalUDP(oob []byte) string {
	messages, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return ""
	}
	for _, m := range messages {
		if m.Header.Level == unix.SOL_IP && m.Header.Type == unix.IP_ORIGDSTADDR && len(m.Data) >= 8 {
			return net.IP(m.Data[4:8]).String()
		}
	}
	return ""
}
func dnsOriginalTCP(conn net.Conn) string {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return ""
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return ""
	}
	data := [16]byte{}
	length := uint32(len(data))
	var callErr unix.Errno
	err = raw.Control(func(fd uintptr) {
		_, _, callErr = unix.Syscall6(unix.SYS_GETSOCKOPT, fd, unix.SOL_IP, 80, uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&length)), 0)
	})
	runtime.KeepAlive(data)
	if err != nil || callErr != 0 || length < 8 || binary.NativeEndian.Uint16(data[:2]) != unix.AF_INET {
		return ""
	}
	return net.IP(data[4:8]).String()
}

func dnsOriginalConntrack(peer string) string {
	host, port, err := net.SplitHostPort(peer)
	if err != nil {
		return ""
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return ""
	}
	flows, err := netlink.ConntrackTableList(netlink.ConntrackTable, netlink.InetFamily(unix.AF_INET))
	if err != nil {
		return ""
	}
	result := ""
	for _, flow := range flows {
		f, r := flow.Forward, flow.Reverse
		if f.Protocol != unix.IPPROTO_UDP || !f.SrcIP.Equal(net.ParseIP(host)) || int(f.SrcPort) != number || f.DstPort != 53 || r.SrcPort != lanDNSPort {
			continue
		}
		candidate := f.DstIP.String()
		if result != "" && result != candidate {
			return ""
		}
		result = candidate
	}
	return result
}
