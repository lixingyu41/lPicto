package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"lpicto/backend/internal/db"
)

func TestExternalBaseURLValidatesAndFormatsIP(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{host: "192.168.2.82", port: 8090, want: "http://192.168.2.82:8090"},
		{host: "2001:db8::1", port: 9000, want: "http://[2001:db8::1]:9000"},
	}
	for _, test := range tests {
		got, err := ExternalBaseURL(test.host, test.port)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("ExternalBaseURL(%q, %d) = %q, want %q", test.host, test.port, got, test.want)
		}
	}
	if _, err := ExternalBaseURL("computer.local", 8090); err == nil {
		t.Fatal("hostname should be rejected")
	}
	if _, err := ExternalBaseURL("192.168.2.82", 0); err == nil {
		t.Fatal("invalid port should be rejected")
	}
}

func TestComputeNodesSupportsSingleAndDualModes(t *testing.T) {
	settings := db.AISettings{ComputeMode: db.AIComputeModeDual, ExternalHost: "192.168.2.82", ExternalPort: 8090}
	nodes, err := ComputeNodes(settings, "http://ai:8090/", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].ID != "ubuntu" || nodes[0].BaseURL != "http://ai:8090" || nodes[1].ID != "external" || !nodes[1].External || nodes[1].Token != "secret" {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
	settings.ComputeMode = db.AIComputeModeExternal
	nodes, err = ComputeNodes(settings, "http://ai:8090", "secret")
	if err != nil || len(nodes) != 1 || nodes[0].ID != "external" {
		t.Fatalf("external nodes = %#v, err = %v", nodes, err)
	}
}

func TestProbeComputeNodeDistinguishesHealthStates(t *testing.T) {
	statusCode := http.StatusOK
	body := `{"status":"ok","service":"lpicto-ai","protocolVersion":1}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" || r.Header.Get("X-LPicto-AI-Token") != "secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	node := ComputeNode{ID: "external", BaseURL: server.URL, External: true, Token: "secret"}
	if status := ProbeComputeNode(context.Background(), node); status.State != "online" {
		t.Fatalf("online state = %q (%s)", status.State, status.Message)
	}
	body = `{"status":"paused","service":"lpicto-ai","protocolVersion":1}`
	if status := ProbeComputeNode(context.Background(), node); status.State != "paused" {
		t.Fatalf("paused state = %q (%s)", status.State, status.Message)
	}
	body = `{"status":"starting","service":"lpicto-ai","protocolVersion":1}`
	if status := ProbeComputeNode(context.Background(), node); status.State != "offline" {
		t.Fatalf("invalid state = %q (%s)", status.State, status.Message)
	}
	statusCode = http.StatusServiceUnavailable
	if status := ProbeComputeNode(context.Background(), node); status.State != "offline" {
		t.Fatalf("unavailable state = %q (%s)", status.State, status.Message)
	}
}
