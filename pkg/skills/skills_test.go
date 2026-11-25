package skills

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
)

func skillMD(name, description, extra string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n%s---\n# %s\n", name, description, extra, name)
}

var binary = []byte{0xff, 0x00, 0xfe, 0x01}

func fixture() fstest.MapFS {
	return fstest.MapFS{
		"logs/SKILL.md":           {Data: []byte(skillMD("logs", "Search logs", "metadata:\n  version: \"1.0.0\"\n"))},
		"logs/reference/cql.md":   {Data: []byte("# CQL\n")},
		"logs/scripts/tail.sh":    {Data: []byte("echo tail\n")},
		"logs/scripts/icon.png":   {Data: binary},
		"logs/.DS_Store":          {Data: []byte("hidden")},
		"triage/SKILL.md":         {Data: []byte(skillMD("triage", "Triage a page", "disable-model-invocation: true\n"))},
		"README.md":               {Data: []byte("not a skill")},
		"notes/plain.md":          {Data: []byte("no SKILL.md, not a skill")},
		".hidden/SKILL.md":        {Data: []byte(skillMD("hidden", "Hidden", ""))},
		"metrics/SKILL.md":        {Data: []byte(skillMD("metrics", "Query metrics", ""))},
		"metrics/examples/a.json": {Data: []byte("{}\n")},
	}
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newFixtureHandler(t *testing.T, opts ...Option) *Handler {
	t.Helper()
	c, err := NewCatalog(fixture())
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return NewHandler(c, opts...)
}

// call sends one request and returns its result or error, decoded.
func call(t *testing.T, h *Handler, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Intercepts(raw) {
		t.Fatalf("%s is not intercepted", raw)
	}
	resp, _ := h.Handle(raw)
	var r struct {
		Result map[string]any `json:"result"`
		Error  map[string]any `json:"error"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("decode %s: %v", resp, err)
	}
	return r.Result, r.Error
}

func uris(items any) []string {
	var out []string
	for _, item := range items.([]any) {
		out = append(out, item.(map[string]any)["uri"].(string))
	}
	return out
}

func TestList(t *testing.T) {
	tests := []struct {
		desc     string
		pageSize int
		want     []string
	}{
		{desc: "one page", pageSize: 100, want: []string{"skill://logs/SKILL.md", "skill://metrics/SKILL.md", "skill://triage/SKILL.md"}},
		{desc: "a page per skill", pageSize: 1, want: []string{"skill://logs/SKILL.md", "skill://metrics/SKILL.md", "skill://triage/SKILL.md"}},
		{desc: "pages of two", pageSize: 2, want: []string{"skill://logs/SKILL.md", "skill://metrics/SKILL.md", "skill://triage/SKILL.md"}},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h := newFixtureHandler(t, WithPageSize(tc.pageSize))
			var got []string
			params := map[string]any{}
			for pages := 0; ; pages++ {
				if pages > 10 {
					t.Fatal("too many pages")
				}
				result, e := call(t, h, "skills/list", params)
				if e != nil {
					t.Fatalf("error: %v", e)
				}
				for _, k := range []string{"resultType", "ttlMs", "cacheScope"} {
					if _, ok := result[k]; !ok {
						t.Errorf("result has no %s", k)
					}
				}
				got = append(got, uris(result["skills"])...)
				next, ok := result["nextCursor"].(string)
				if !ok {
					break
				}
				params = map[string]any{"cursor": next}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("skills mismatch (-want, +got): %v", diff)
			}
		})
	}
}

func TestListEntry(t *testing.T) {
	result, e := call(t, newFixtureHandler(t), "skills/list", nil)
	if e != nil {
		t.Fatalf("error: %v", e)
	}
	logs := result["skills"].([]any)[0]
	want := map[string]any{
		"uri":         "skill://logs/SKILL.md",
		"frontmatter": map[string]any{"name": "logs", "description": "Search logs", "metadata": map[string]any{"version": "1.0.0"}},
		"resources": []any{
			map[string]any{"uri": "skill://logs/SKILL.md", "size": float64(len(fixture()["logs/SKILL.md"].Data)), "digest": digestOf(fixture()["logs/SKILL.md"].Data)},
			map[string]any{"uri": "skill://logs/reference/cql.md", "size": float64(6), "digest": digestOf([]byte("# CQL\n"))},
			map[string]any{"uri": "skill://logs/scripts/icon.png", "size": float64(4), "digest": digestOf(binary)},
			map[string]any{"uri": "skill://logs/scripts/tail.sh", "size": float64(10), "digest": digestOf([]byte("echo tail\n"))},
		},
	}
	if diff := cmp.Diff(want, logs); diff != "" {
		t.Errorf("entry mismatch (-want, +got): %v", diff)
	}
	triage := result["skills"].([]any)[2].(map[string]any)
	if got := triage["frontmatter"].(map[string]any)["disable-model-invocation"]; got != true {
		t.Errorf("disable-model-invocation = %v, want true", got)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		desc     string
		method   string
		params   any
		wantCode float64
	}{
		{desc: "list with an invalid cursor", method: "skills/list", params: map[string]any{"cursor": "!!"}, wantCode: codeInvalidParams},
		{desc: "list with a cursor that isn't ours", method: "skills/list", params: map[string]any{"cursor": base64.RawURLEncoding.EncodeToString([]byte("x"))}, wantCode: codeInvalidParams},
		{desc: "get an unknown skill", method: "skills/get", params: map[string]any{"uri": "skill://nope/SKILL.md"}, wantCode: codeInvalidParams},
		{desc: "get by name, not uri", method: "skills/get", params: map[string]any{"name": "logs"}, wantCode: codeInvalidParams},
		{desc: "get a file that isn't SKILL.md", method: "skills/get", params: map[string]any{"uri": "skill://logs/reference/cql.md"}, wantCode: codeInvalidParams},
		{desc: "read an unknown file", method: "resources/read", params: map[string]any{"uri": "skill://logs/nope.md"}, wantCode: codeInvalidParams},
		{desc: "read a hidden file", method: "resources/read", params: map[string]any{"uri": "skill://logs/.DS_Store"}, wantCode: codeInvalidParams},
		{desc: "read a directory", method: "resources/read", params: map[string]any{"uri": "skill://logs/reference"}, wantCode: codeInvalidParams},
		{desc: "directory of a file", method: "resources/directory/read", params: map[string]any{"uri": "skill://logs/SKILL.md"}, wantCode: codeInvalidParams},
		{desc: "directory unknown", method: "resources/directory/read", params: map[string]any{"uri": "skill://nope"}, wantCode: codeInvalidParams},
		{desc: "unknown skills method", method: "skills/nope", wantCode: codeMethodNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h := newFixtureHandler(t)
			raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": tc.method, "params": tc.params})
			resp, _ := h.Handle(raw)
			var r struct {
				Error *Error `json:"error"`
			}
			if err := json.Unmarshal(resp, &r); err != nil {
				t.Fatal(err)
			}
			if r.Error == nil || float64(r.Error.Code) != tc.wantCode {
				t.Errorf("got %s, want error code %v", resp, tc.wantCode)
			}
		})
	}
}

func TestGet(t *testing.T) {
	h := newFixtureHandler(t)
	list, _ := call(t, h, "skills/list", nil)
	for _, entry := range list["skills"].([]any) {
		uri := entry.(map[string]any)["uri"].(string)
		t.Run(uri, func(t *testing.T) {
			result, e := call(t, h, "skills/get", map[string]any{"uri": uri})
			if e != nil {
				t.Fatalf("error: %v", e)
			}
			if diff := cmp.Diff(entry, result["skill"]); diff != "" {
				t.Errorf("get differs from list (-list, +get): %v", diff)
			}
			for _, k := range []string{"resultType", "ttlMs", "cacheScope"} {
				if _, ok := result[k]; !ok {
					t.Errorf("result has no %s", k)
				}
			}
		})
	}
}

// TestReadMatchesDigests reads every listed file and checks its bytes against the listed size and digest, as a
// client does.
func TestReadMatchesDigests(t *testing.T) {
	tests := []struct {
		uri      string
		mimeType string
		blob     bool
	}{
		{uri: "skill://logs/SKILL.md", mimeType: "text/markdown"},
		{uri: "skill://logs/reference/cql.md", mimeType: "text/markdown"},
		{uri: "skill://logs/scripts/tail.sh", mimeType: "application/x-sh"},
		{uri: "skill://logs/scripts/icon.png", mimeType: "image/png", blob: true},
		{uri: "skill://metrics/SKILL.md", mimeType: "text/markdown"},
		{uri: "skill://metrics/examples/a.json", mimeType: "application/json"},
		{uri: "skill://triage/SKILL.md", mimeType: "text/markdown"},
	}
	h := newFixtureHandler(t)
	listed := map[string]map[string]any{}
	list, _ := call(t, h, "skills/list", nil)
	for _, entry := range list["skills"].([]any) {
		for _, r := range entry.(map[string]any)["resources"].([]any) {
			listed[r.(map[string]any)["uri"].(string)] = r.(map[string]any)
		}
	}
	if len(listed) != len(tests) {
		t.Errorf("listed %d files, want %d", len(listed), len(tests))
	}
	for _, tc := range tests {
		t.Run(tc.uri, func(t *testing.T) {
			result, e := call(t, h, "resources/read", map[string]any{"uri": tc.uri})
			if e != nil {
				t.Fatalf("error: %v", e)
			}
			content := result["contents"].([]any)[0].(map[string]any)
			if content["uri"] != tc.uri || content["mimeType"] != tc.mimeType {
				t.Errorf("content uri/mimeType = %v/%v, want %v/%v", content["uri"], content["mimeType"], tc.uri, tc.mimeType)
			}
			var data []byte
			if blob, ok := content["blob"].(string); ok {
				if !tc.blob {
					t.Errorf("got a blob, want text")
				}
				var err error
				if data, err = base64.StdEncoding.DecodeString(blob); err != nil {
					t.Fatal(err)
				}
			} else {
				if tc.blob {
					t.Errorf("got text, want a blob")
				}
				data = []byte(content["text"].(string))
			}
			entry := listed[tc.uri]
			if entry == nil {
				t.Fatalf("%s is not listed", tc.uri)
			}
			if got, want := float64(len(data)), entry["size"]; got != want {
				t.Errorf("size = %v, listed %v", got, want)
			}
			if got, want := digestOf(data), entry["digest"]; got != want {
				t.Errorf("digest = %v, listed %v", got, want)
			}
		})
	}
}

func TestDirectoryRead(t *testing.T) {
	size := func(n int) map[string]any { return map[string]any{"size": float64(n)} }
	entry := func(uri, name, mimeType string, extra map[string]any) map[string]any {
		m := map[string]any{"uri": uri, "name": name, "mimeType": mimeType}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	logsMD := len(fixture()["logs/SKILL.md"].Data)
	tests := []struct {
		uri  string
		want []any
	}{
		{uri: "skill://logs", want: []any{
			entry("skill://logs/SKILL.md", "SKILL.md", "text/markdown", size(logsMD)),
			entry("skill://logs/reference", "reference", DirectoryMIMEType, nil),
			entry("skill://logs/scripts", "scripts", DirectoryMIMEType, nil),
		}},
		{uri: "skill://logs/scripts", want: []any{
			entry("skill://logs/scripts/icon.png", "icon.png", "image/png", size(4)),
			entry("skill://logs/scripts/tail.sh", "tail.sh", "application/x-sh", size(10)),
		}},
	}
	h := newFixtureHandler(t)
	for _, tc := range tests {
		t.Run(tc.uri, func(t *testing.T) {
			result, e := call(t, h, "resources/directory/read", map[string]any{"uri": tc.uri})
			if e != nil {
				t.Fatalf("error: %v", e)
			}
			if diff := cmp.Diff(tc.want, result["resources"]); diff != "" {
				t.Errorf("children mismatch (-want, +got): %v", diff)
			}
		})
	}
}

func TestIntercepts(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: `{"jsonrpc":"2.0","id":1,"method":"skills/list"}`, want: true},
		{msg: `{"jsonrpc":"2.0","id":1,"method":"skills/get","params":{"uri":"skill://logs/SKILL.md"}}`, want: true},
		{msg: `{"jsonrpc":"2.0","id":1,"method":"resources/directory/read","params":{"uri":"skill://logs"}}`, want: true},
		{msg: `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"skill://logs/SKILL.md"}}`, want: true},
		{msg: `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"edgedelta://services"}}`, want: false},
		{msg: `{"jsonrpc":"2.0","method":"skills/list"}`, want: false},
		{msg: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, want: false},
		{msg: `not json`, want: false},
	}
	h := newFixtureHandler(t)
	for _, tc := range tests {
		if got := h.Intercepts([]byte(tc.msg)); got != tc.want {
			t.Errorf("Intercepts(%s) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestNewCatalogRejects(t *testing.T) {
	tests := []struct {
		desc  string
		files fstest.MapFS
		want  string
	}{
		{desc: "name differs from its folder", files: fstest.MapFS{"logs/SKILL.md": {Data: []byte(skillMD("log", "d", ""))}}, want: "must match its folder"},
		{desc: "name breaks the naming rule", files: fstest.MapFS{"Logs/SKILL.md": {Data: []byte(skillMD("Logs", "d", ""))}}, want: "lowercase letters"},
		{desc: "no description", files: fstest.MapFS{"logs/SKILL.md": {Data: []byte("---\nname: logs\n---\n")}}, want: "description"},
		{desc: "no frontmatter", files: fstest.MapFS{"logs/SKILL.md": {Data: []byte("# logs\n")}}, want: `begin with a "---"`},
		{desc: "unclosed frontmatter", files: fstest.MapFS{"logs/SKILL.md": {Data: []byte("---\nname: logs\n")}}, want: "closing"},
		{desc: "an unquoted date", files: fstest.MapFS{"logs/SKILL.md": {Data: []byte(skillMD("logs", "d", "updated: 2026-01-02\n"))}}, want: "timestamp"},
		{desc: "a metadata number", files: fstest.MapFS{"logs/SKILL.md": {Data: []byte(skillMD("logs", "d", "metadata:\n  version: 1\n"))}}, want: "metadata"},
		{desc: "a name that needs encoding", files: fstest.MapFS{"logs/SKILL.md": {Data: []byte(skillMD("logs", "d", ""))}, "logs/a b.md": {Data: []byte("x")}}, want: "percent-encoding"},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := NewCatalog(tc.files)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("NewCatalog error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

// TestEmbedded checks the skills the binary serves: every synced skill folder is a skill, and every file in it is
// listed with the size and digest of the file on disk.
func TestEmbedded(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	dirs, err := os.ReadDir("data")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, d := range dirs {
		want = append(want, "skill://"+d.Name()+"/SKILL.md")
	}
	var got []string
	for _, s := range c.Skills() {
		got = append(got, s.URI)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("skills mismatch (-want, +got): %v", diff)
	}
	for _, s := range c.Skills() {
		t.Run(s.Name(), func(t *testing.T) {
			root := filepath.Join("data", s.Name())
			var onDisk []Resource
			err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel("data", p)
				onDisk = append(onDisk, Resource{URI: "skill://" + filepath.ToSlash(rel), Digest: digestOf(b), Size: len(b)})
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			sort.Slice(onDisk, func(i, j int) bool { return onDisk[i].URI < onDisk[j].URI })
			if diff := cmp.Diff(onDisk, s.Resources); diff != "" {
				t.Errorf("resources mismatch (-disk, +listed): %v", diff)
			}
		})
	}
}
