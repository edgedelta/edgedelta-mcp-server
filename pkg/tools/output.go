package tools

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/mark3labs/mcp-go/mcp"
)

// propertyDoc describes one top-level property of a tool's output, where its meaning depends on the tool.
type propertyDoc struct{ name, description string }

func doc(name, description string) propertyDoc { return propertyDoc{name, description} }

// outputSchemaFor derives the JSON Schema of T, the type a tool's handler marshals its result from. Field
// descriptions come from jsonschema struct tags, and docs override those of top-level properties. A
// json.RawMessage, which passes the Edge Delta API's response through, may be any JSON value.
func outputSchemaFor[T any](docs ...propertyDoc) (json.RawMessage, error) {
	s, err := jsonschema.For[T](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{reflect.TypeFor[json.RawMessage](): {}},
	})
	if err != nil {
		return nil, err
	}
	if s.Type != "object" {
		return nil, fmt.Errorf("output schema of %T must be an object, not %q", *new(T), s.Type)
	}
	for _, d := range docs {
		p, ok := s.Properties[d.name]
		if !ok {
			return nil, fmt.Errorf("output schema of %T has no property %q", *new(T), d.name)
		}
		p.Description = d.description
	}
	return json.Marshal(s)
}

// outputSchema declares that a tool returns T as structuredContent, through structuredResult. It panics on a
// type that has no JSON Schema, which tests catch for every tool.
func outputSchema[T any](docs ...propertyDoc) mcp.ToolOption {
	schema, err := outputSchemaFor[T](docs...)
	if err != nil {
		panic(err)
	}
	return mcp.WithRawOutputSchema(schema)
}

// structuredResult returns a tool's JSON result as structuredContent, and as text for clients that read only
// text content.
func structuredResult(result []byte) *mcp.CallToolResult {
	return mcp.NewToolResultStructured(json.RawMessage(result), string(result))
}

// rawJSON is the Edge Delta API's response body as a JSON value, or as a JSON string when it isn't JSON.
func rawJSON(body []byte) json.RawMessage {
	if json.Valid(body) {
		return body
	}
	s, _ := json.Marshal(string(body))
	return s
}
