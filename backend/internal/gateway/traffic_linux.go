//go:build linux

package gateway

import (
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
	"time"
)

func warpTrafficCounter() (trafficCounter, error) {
	ns, e := netns.GetFromName("foxray-warp")
	if e != nil {
		return trafficCounter{}, e
	}
	defer ns.Close()
	handle, e := netlink.NewHandleAt(ns, unix.NETLINK_ROUTE)
	if e != nil {
		return trafficCounter{}, e
	}
	defer handle.Close()
	_ = handle.SetSocketTimeout(2 * time.Second)
	link, e := handle.LinkByName("CloudflareWARP")
	if e != nil {
		return trafficCounter{}, e
	}
	stats := link.Attrs().Statistics
	if stats == nil {
		return trafficCounter{}, errTrafficUnavailable
	}
	return trafficCounter{Up: stats.TxBytes, Down: stats.RxBytes, Available: true, Status: "隧道接口计数"}, nil
}
