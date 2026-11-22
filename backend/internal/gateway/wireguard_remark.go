package gateway

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const wgRemarkPrefix = "# FoXray-Remark: "

func validateWGRemark(value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", errors.New("备注须为文本")
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > 200 {
		return "", errors.New("备注最多200个字符")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", errors.New("备注不允许换行或控制字符")
		}
	}
	return strings.TrimSpace(s), nil
}
func readWGRemark(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, wgRemarkPrefix) {
			var s string
			if json.Unmarshal([]byte(strings.TrimPrefix(line, wgRemarkPrefix)), &s) == nil {
				if value, err := validateWGRemark(s); err == nil {
					return value
				}
			}
		}
	}
	return ""
}

// Only the application comment changes; all other configuration bytes survive.
func rewriteWGRemark(raw, remark string) string {
	ending := "\n"
	if strings.Contains(raw, "\r\n") {
		ending = "\r\n"
	}
	var out strings.Builder
	if remark != "" {
		value, _ := json.Marshal(remark)
		out.WriteString(wgRemarkPrefix + string(value) + ending)
	}
	for _, line := range strings.SplitAfter(raw, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), wgRemarkPrefix) {
			out.WriteString(line)
		}
	}
	return out.String()
}
func (n *Network) planWGRemarkFile(p *NetworkPlan, name, path string, value any) error {
	remark, err := validateWGRemark(value)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("备注仅支持已有的普通 wg-quick 配置文件")
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return errors.New("无法读取接口配置")
	}
	payload := []byte(rewriteWGRemark(string(old), remark))
	backup := filepath.Join(n.app.base, "private", "network-backups", p.ID+".conf")
	p.restoreFile = &FileRestore{Target: path, Backup: backup, Existed: true, OldHash: fingerprint(string(old)), ExpectedHash: fingerprint(string(payload))}
	p.precondition = func() error {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("接口配置状态已变化，请重新预览")
		}
		current, err := os.ReadFile(path)
		if err != nil || fingerprint(string(current)) != fingerprint(string(old)) {
			return errors.New("接口配置已变化，请重新预览")
		}
		return nil
	}
	p.applyFiles = func() error {
		if err := atomicFile(backup, old, 0600); err != nil {
			return err
		}
		if err := n.writeSpec(p, 0); err != nil {
			return err
		}
		return atomicFile(path, payload, 0600)
	}
	p.Summary = "修改 WireGuard 备注 · " + name
	p.Preview = append(p.Preview, "备注："+readWGRemark(string(old))+" → "+remark)
	p.Warnings = append(p.Warnings, "仅更新备注，不启动、停止或重启接口，不调整地址、Peer、路由或保护状态；120秒内确认，否则恢复原备注")
	return nil
}
