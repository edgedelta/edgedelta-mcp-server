package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHTTPServerInitializeServerInfo(t *testing.T) {
	m, err := NewHTTPServer(WithServerVersion("1.2.3"))
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}
	srv := httptest.NewServer(m.HTTPServer())
	defer srv.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var got struct {
		Result struct {
			ServerInfo mcp.Implementation `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := mcp.Implementation{
		Name:       "edgedelta-mcp-server",
		Version:    "1.2.3",
		Title:      "Edge Delta",
		WebsiteURL: "https://edgedelta.com",
		Icons: []mcp.Icon{
			{Src: "https://app.edgedelta.com/icon-192.png", MIMEType: "image/png", Sizes: []string{"192x192"}},
			{Src: "https://app.edgedelta.com/icon.svg", MIMEType: "image/svg+xml", Sizes: []string{"any"}},
		},
	}
	if !reflect.DeepEqual(got.Result.ServerInfo, want) {
		t.Errorf("serverInfo = %+v, want %+v", got.Result.ServerInfo, want)
	}
}
