package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/events"
	"github.com/edgedelta/edgedelta-mcp-server/pkg/skills"
	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"

	"github.com/google/go-cmp/cmp"
)

func testCatalog(t *testing.T) *skills.Catalog {
	t.Helper()
	c, err := skills.NewCatalog(fstest.MapFS{
		"logs/SKILL.md":         {Data: []byte("---\nname: logs\ndescription: Search logs\n---\n# Logs\n")},
		"logs/reference/cql.md": {Data: []byte("# CQL\n")},
	})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return c
}

func TestHTTPSkills(t *testing.T) {
	m, err := NewHTTPServer(WithSkills(testCatalog(t)))
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()

	tests := []struct {
		desc  string
		body  string
		check func(map[string]any) any
		want  any
	}{
		{
			desc:  "initialize advertises the extension with directory reads",
			body:  `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
			check: func(r map[string]any) any { return dig(r, "result", "capabilities", "extensions", skills.Extension) },
			want:  map[string]any{"directoryRead": true},
		},
		{
			desc: "initialize declares resources",
			body: `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
			check: func(r map[string]any) any {
				return dig(r, "result", "capabilities", "resources") != nil
			},
			want: true,
		},
		{
			desc:  "skills/list lists the skill",
			body:  `{"jsonrpc":"2.0","id":2,"method":"skills/list","params":{}}`,
			check: func(r map[string]any) any { return dig(r, "result", "skills").([]any)[0].(map[string]any)["uri"] },
			want:  "skill://logs/SKILL.md",
		},
		{
			desc:  "skills/get returns the skill",
			body:  `{"jsonrpc":"2.0","id":3,"method":"skills/get","params":{"uri":"skill://logs/SKILL.md"}}`,
			check: func(r map[string]any) any { return dig(r, "result", "skill", "frontmatter", "name") },
			want:  "logs",
		},
		{
			desc:  "resources/read serves a skill file",
			body:  `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"skill://logs/reference/cql.md"}}`,
			check: func(r map[string]any) any { return dig(r, "result", "contents").([]any)[0].(map[string]any)["text"] },
			want:  "# CQL\n",
		},
		{
			desc:  "resources/read of an unknown skill file is invalid params",
			body:  `{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":"skill://logs/nope.md"}}`,
			check: func(r map[string]any) any { return dig(r, "error", "code") },
			want:  float64(-32602),
		},
		{
			desc: "resources/list lists each SKILL.md",
			body: `{"jsonrpc":"2.0","id":6,"method":"resources/list"}`,
			check: func(r map[string]any) any {
				var got []any
				for _, res := range dig(r, "result", "resources").([]any) {
					if uri := res.(map[string]any)["uri"].(string); strings.HasPrefix(uri, "skill://") {
						got = append(got, []any{uri, res.(map[string]any)["description"], res.(map[string]any)["mimeType"]})
					}
				}
				return got
			},
			want: []any{[]any{"skill://logs/SKILL.md", "Search logs", "text/markdown"}},
		},
		{
			desc:  "resources/directory/read lists a directory",
			body:  `{"jsonrpc":"2.0","id":7,"method":"resources/directory/read","params":{"uri":"skill://logs/reference"}}`,
			check: func(r map[string]any) any { return dig(r, "result", "resources").([]any)[0].(map[string]any)["uri"] },
			want:  "skill://logs/reference/cql.md",
		},
	}
	for _, tc := range tests {
		status, got := post(t, srv.URL, tc.body, nil)
		if status != http.StatusOK {
			t.Errorf("%s: status got: %d, want: 200", tc.desc, status)
		}
		if diff := cmp.Diff(tc.want, tc.check(got)); diff != "" {
			t.Errorf("%s: mismatch (-want, +got): %v", tc.desc, diff)
		}
	}
}

func TestHTTPWithoutSkills(t *testing.T) {
	m, err := NewHTTPServer(WithSkills(nil))
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	_, init := post(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`, nil)
	if got := dig(init, "result", "capabilities", "extensions", skills.Extension); got != nil {
		t.Errorf("extension declared without skills: %v", got)
	}
	_, list := post(t, srv.URL, `{"jsonrpc":"2.0","id":2,"method":"skills/list"}`, nil)
	if got := dig(list, "error", "code"); got != float64(-32601) {
		t.Errorf("skills/list without skills: error code %v, want -32601", got)
	}
}

func TestDefaultServesEmbeddedSkills(t *testing.T) {
	m, err := NewHTTPServer()
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	_, list := post(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"skills/list"}`, nil)
	var got []string
	for _, s := range dig(list, "result", "skills").([]any) {
		got = append(got, s.(map[string]any)["uri"].(string))
	}
	embedded, err := skills.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, s := range embedded.Skills() {
		want = append(want, s.URI)
	}
	sort.Strings(want)
	if len(want) == 0 || !cmp.Equal(want, got) {
		t.Errorf("skills got: %v, want: %v", got, want)
	}
}

func TestFilterSkills(t *testing.T) {
	x := extensions{
		events: events.NewHandler(tools.NewHTTPClient("https://api.edgedelta.com", "X-ED-API-Token")),
		skills: skills.NewHandler(testCatalog(t)),
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"skills/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"edgedelta://services"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"skill://logs/SKILL.md"}}`,
	}, "\n") + "\n"
	passed, next := io.Pipe()
	forwarded := make(chan string)
	go func() {
		b, _ := io.ReadAll(passed)
		forwarded <- strings.TrimSpace(string(b))
	}()
	out := &strings.Builder{}
	if err := filterExtensions(context.Background(), x, strings.NewReader(input), &lineWriter{w: out}, next); err != nil {
		t.Fatalf("filterExtensions: %v", err)
	}
	if got, want := <-forwarded, `{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"edgedelta://services"}}`; got != want {
		t.Errorf("forwarded got: %s, want: %s", got, want)
	}
	if n := strings.Count(strings.TrimSpace(out.String()), "\n") + 1; n != 2 {
		t.Errorf("answered %d requests, want 2: %s", n, out.String())
	}
}
