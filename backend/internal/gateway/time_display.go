package gateway

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var defaultClockServers = []string{"https://www.cloudflare.com/", "https://www.microsoft.com/", "https://www.baidu.com/"}

var displayClock struct {
	sync.Mutex
	at     time.Time
	stamp  time.Time
	target string
	server string
}

func validClockServer(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && strings.TrimSpace(raw) == raw && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func clockHTTPTime(client *http.Client, endpoint string) (time.Time, error) {
	req, err := http.NewRequest(http.MethodHead, endpoint, nil)
	if err != nil {
		return time.Time{}, errors.New("时间服务器地址无效")
	}
	req.Header.Set("Cache-Control", "no-cache")
	start := time.Now()
	res, err := client.Do(req)
	if err != nil {
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return time.Time{}, errors.New("连接超时")
		}
		return time.Time{}, errors.New("网络连接或TLS验证失败")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return time.Time{}, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	stamp, err := http.ParseTime(res.Header.Get("Date"))
	if err != nil {
		return time.Time{}, errors.New("响应缺少有效的Date时间")
	}
	return stamp.Add(time.Since(start) / 2), nil
}

func displayTime(source, endpoint string) M {
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("时间服务器重定向无效")
		}
		return nil
	}}
	return displayTimeWithClient(source, endpoint, client)
}

func displayTimeWithClient(source, endpoint string, client *http.Client) M {
	out := M{"server_time": float64(time.Now().UnixMilli()) / 1000}
	if source != "internet" {
		return out
	}
	if !validClockServer(endpoint) {
		out["error"] = "时间服务器须为不含账号、查询参数或片段的HTTPS地址"
		return out
	}
	displayClock.Lock()
	defer displayClock.Unlock()
	if time.Since(displayClock.at) > time.Minute || displayClock.stamp.IsZero() || displayClock.target != endpoint {
		targets := defaultClockServers
		if endpoint != "" {
			targets = []string{endpoint}
		}
		var failure error
		for _, target := range targets {
			stamp, err := clockHTTPTime(client, target)
			if err != nil {
				failure = err
				continue
			}
			displayClock.at = time.Now()
			displayClock.stamp = stamp
			displayClock.target = endpoint
			displayClock.server = target
			failure = nil
			break
		}
		if failure != nil {
			out["error"] = "互联网取时失败：" + failure.Error()
			return out
		}
	}
	out["internet_time"] = float64(displayClock.stamp.Add(time.Since(displayClock.at)).UnixMilli()) / 1000
	out["source"] = "HTTPS Date"
	out["time_server"] = displayClock.server
	return out
}
