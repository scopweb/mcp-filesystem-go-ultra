package main

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

func isWildcardBind(host string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]")) {
	case "", "0.0.0.0", "::", "::0":
		return true
	default:
		return false
	}
}

// allowedHTTPHosts is the Host-header allowlist. --host is the listen address.
// A wildcard bind does not allow arbitrary Host values.
func allowedHTTPHosts(bindHost string) map[string]struct{} {
	names := []string{"localhost", "127.0.0.1", "::1"}
	if !isWildcardBind(bindHost) {
		names = append(names, bindHost)
	}
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.Trim(strings.TrimSpace(name), "[]")
		if name == "" || isWildcardBind(name) {
			continue
		}
		if ip := net.ParseIP(name); ip != nil {
			name = ip.String()
		}
		out[strings.ToLower(name)] = struct{}{}
	}
	return out
}

func hostHeaderAllowed(header string, allowed map[string]struct{}, port int) bool {
	header = strings.TrimSpace(header)
	if header == "" || strings.Contains(header, "@") || strings.Contains(header, " ") {
		return false
	}
	name, p, err := net.SplitHostPort(header)
	if err != nil {
		return false
	}
	if p != strconv.Itoa(port) {
		return false
	}
	name = strings.Trim(name, "[]")
	if ip := net.ParseIP(name); ip != nil {
		name = ip.String()
	} else if !validDNSName(name) {
		return false
	}
	_, ok := allowed[strings.ToLower(name)]
	return ok
}

func validDNSName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

func guardHost(allowed map[string]struct{}, port int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostHeaderAllowed(r.Host, allowed, port) {
			http.Error(w, "invalid host", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}
