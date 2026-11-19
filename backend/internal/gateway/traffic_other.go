//go:build !linux

package gateway

func warpTrafficCounter() (trafficCounter, error) { return trafficCounter{}, errTrafficUnavailable }
