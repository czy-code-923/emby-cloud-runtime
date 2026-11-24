package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// NormalizePublicBaseURL validates a display/link origin. It never resolves or contacts the host.
func NormalizePublicBaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	invalid := func() (string, error) {
		return "", fmt.Errorf("Global.public_base_url 必须为不含账号、路径、查询或片段的 HTTPS 域名地址，例如 https://admin.example.com")
	}
	if strings.ContainsAny(value, "\\\r\n\t") {
		return invalid()
	}
	if strings.HasSuffix(value, ":") {
		return invalid()
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return invalid()
	}
	host := strings.ToLower(u.Hostname())
	if len(host) > 253 || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return invalid()
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return invalid()
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return invalid()
			}
		}
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return invalid()
		}
	}
	u.Path = ""
	u.RawPath = ""
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}
