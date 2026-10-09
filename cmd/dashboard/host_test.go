package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostHeader_AllowsLoopbackRejectsForeign(t *testing.T) {
	allowed := allowedHTTPHosts("127.0.0.1")
	if !hostHeaderAllowed("127.0.0.1:9100", allowed, 9100) {
		t.Fatal("ipv4 loopback")
	}
	if !hostHeaderAllowed("localhost:9100", allowed, 9100) {
		t.Fatal("localhost")
	}
	if !hostHeaderAllowed("[::1]:9100", allowed, 9100) {
		t.Fatal("ipv6 loopback")
	}
	if hostHeaderAllowed("attacker.example:9100", allowed, 9100) {
		t.Fatal("foreign host accepted")
	}
	if hostHeaderAllowed("127.0.0.1:80", allowed, 9100) {
		t.Fatal("wrong port accepted")
	}
	if hostHeaderAllowed("127.0.0.1", allowed, 9100) {
		t.Fatal("missing port accepted")
	}
}

func TestHostHeader_WildcardBindDoesNotAllowArbitraryHost(t *testing.T) {
	allowed := allowedHTTPHosts("0.0.0.0")
	if _, ok := allowed["0.0.0.0"]; ok {
		t.Fatal("wildcard bind must not become an allowed Host")
	}
	if hostHeaderAllowed("attacker.example:9100", allowed, 9100) {
		t.Fatal("wildcard bind disabled host protection")
	}
	if !hostHeaderAllowed("localhost:9100", allowed, 9100) {
		t.Fatal("loopback should remain allowed")
	}
}

func TestHostHeader_SpecificBindIsAllowed(t *testing.T) {
	allowed := allowedHTTPHosts("192.168.1.5")
	if !hostHeaderAllowed("192.168.1.5:9100", allowed, 9100) {
		t.Fatal("specific bind host")
	}
	if hostHeaderAllowed("192.168.1.9:9100", allowed, 9100) {
		t.Fatal("other lan host")
	}
}

func TestGuardHost_BlocksBeforeHandler(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	h := guardHost(allowedHTTPHosts("127.0.0.1"), 9100, next)
	req := httptest.NewRequest(http.MethodGet, "http://attacker.example:9100/api/backups", nil)
	req.Host = "attacker.example:9100"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if called {
		t.Fatal("handler ran for a foreign Host")
	}
	if rr.Code != http.StatusMisdirectedRequest {
		t.Fatalf("status %d", rr.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9100/api/trash/restore", nil)
	req.Host = "127.0.0.1:9100"
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !called {
		t.Fatal("loopback POST was blocked")
	}
}
