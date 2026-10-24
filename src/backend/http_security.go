package main

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" || host == "localhost.localdomain" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func localSecurityMiddleware(config AppConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}

		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if !isLoopbackHost(host) {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		_, hostPort, portErr := net.SplitHostPort(r.Host)
		if portErr != nil || hostPort != strconv.Itoa(config.Port) {
			http.Error(w, "invalid host port", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, parseErr := url.Parse(origin)
			if parseErr != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" ||
				parsed.Fragment != "" || !isLoopbackHost(parsed.Hostname()) {
				http.Error(w, "invalid origin", http.StatusForbidden)
				return
			}
			originPort := parsed.Port()
			if originPort == "" {
				originPort = "80"
			}
			appPort := strconv.Itoa(config.Port)
			if originPort != appPort && !(config.DevelopmentMode && originPort == "5173") {
				http.Error(w, "invalid origin", http.StatusForbidden)
				return
			}
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
		}
		next.ServeHTTP(w, r)
	})
}
