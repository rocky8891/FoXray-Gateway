package gateway

import (
	"encoding/binary"
	"errors"
	"strings"
)

type dnsQuestion struct {
	name        string
	kind, class uint16
	end         int
}

func dnsName(raw []byte, offset int) (string, int, error) {
	labels := []string{}
	end := -1
	visited := map[int]bool{}
	length := 0
	for steps := 0; steps < 128; steps++ {
		if offset >= len(raw) || visited[offset] {
			break
		}
		visited[offset] = true
		n := int(raw[offset])
		offset++
		if n == 0 {
			if end < 0 {
				end = offset
			}
			return strings.ToLower(strings.Join(labels, ".")), end, nil
		}
		if n&0xc0 == 0xc0 {
			if offset >= len(raw) {
				break
			}
			if end < 0 {
				end = offset + 1
			}
			offset = ((n & 63) << 8) | int(raw[offset])
			continue
		}
		if n > 63 || offset+n > len(raw) {
			break
		}
		length += n + 1
		if length > 254 {
			break
		}
		labels = append(labels, string(raw[offset:offset+n]))
		offset += n
	}
	return "", 0, errors.New("DNS名称编码无效")
}
func parseDNSQuestion(raw []byte) (dnsQuestion, error) {
	if len(raw) < 12 || binary.BigEndian.Uint16(raw[4:6]) != 1 || raw[2]&0x78 != 0 {
		return dnsQuestion{}, errors.New("仅支持标准单问题DNS查询")
	}
	name, end, err := dnsName(raw, 12)
	if err != nil || end+4 > len(raw) {
		return dnsQuestion{}, errors.New("DNS问题不完整")
	}
	return dnsQuestion{name: name, kind: binary.BigEndian.Uint16(raw[end : end+2]), class: binary.BigEndian.Uint16(raw[end+2 : end+4]), end: end + 4}, nil
}
func validDNSReply(query, response []byte) bool {
	if len(response) < 12 || len(query) < 12 || response[0] != query[0] || response[1] != query[1] || response[2]&0x80 == 0 {
		return false
	}
	a, err := parseDNSQuestion(query)
	if err != nil {
		return false
	}
	b, err := parseDNSQuestion(response)
	return err == nil && a.name == b.name && a.kind == b.kind && a.class == b.class
}
func dnsErrorReply(query []byte, code byte, truncated bool) []byte {
	if len(query) < 12 {
		return nil
	}
	q, err := parseDNSQuestion(query)
	end := 12
	if err == nil {
		end = q.end
	}
	result := append([]byte{}, query[:end]...)
	result[2] = 0x80 | (query[2] & 1)
	result[3] = 0x80 | code
	if truncated {
		result[2] |= 2
	}
	if err != nil {
		result[4] = 0
		result[5] = 0
	}
	for i := 6; i < 12; i++ {
		result[i] = 0
	}
	return result
}
func dnsUDPSize(query []byte) int {
	q, err := parseDNSQuestion(query)
	if err != nil {
		return 512
	}
	offset := q.end
	// Queries normally carry only an EDNS OPT additional record. Never allow
	// unbounded UDP amplification, even when a client advertises 65535 bytes.
	if len(query) < 12 || binary.BigEndian.Uint16(query[6:8]) != 0 || binary.BigEndian.Uint16(query[8:10]) != 0 {
		return 512
	}
	for i := 0; i < int(binary.BigEndian.Uint16(query[10:12])) && i < 16; i++ {
		_, end, err := dnsName(query, offset)
		if err != nil || end+10 > len(query) {
			break
		}
		kind := binary.BigEndian.Uint16(query[end : end+2])
		size := int(binary.BigEndian.Uint16(query[end+2 : end+4]))
		n := int(binary.BigEndian.Uint16(query[end+8 : end+10]))
		offset = end + 10 + n
		if offset > len(query) {
			break
		}
		if kind == 41 {
			return min(4096, max(512, size))
		}
	}
	return 512
}
