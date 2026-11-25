package server

import (
	"encoding/json"
	"testing"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"
)

// TestEveryToolDeclaresAnOutputSchema keeps new tools from going without one: tools/list must give each tool's
// outputSchema, an object schema, so clients can type its structuredContent.
func TestEveryToolDeclaresAnOutputSchema(t *testing.T) {
	s, _ := newMCPServer(defaultServerConfig, tools.NewHTTPClient("http://unused", "X-ED-API-Token"))
	listed := s.ListTools()
	if len(listed) == 0 {
		t.Fatal("no tools")
	}
	for name, st := range listed {
		b, err := json.Marshal(st.Tool)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var tool struct {
			OutputSchema *struct {
				Type       string         `json:"type"`
				Properties map[string]any `json:"properties"`
			} `json:"outputSchema"`
		}
		if err := json.Unmarshal(b, &tool); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if tool.OutputSchema == nil {
			t.Errorf("%s lists no outputSchema", name)
			continue
		}
		if tool.OutputSchema.Type != "object" || len(tool.OutputSchema.Properties) == 0 {
			t.Errorf("%s outputSchema is not an object schema with properties", name)
		}
	}
	t.Logf("%d tools, each with an outputSchema", len(listed))
}
