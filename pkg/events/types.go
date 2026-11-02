// Package events serves the MCP Events extension (SEP-3415) from Edge Delta's event search.
package events

import (
	"fmt"
	"slices"
	"strings"
)

// Extension is the extension's identifier in capabilities.
const Extension = "io.modelcontextprotocol/events"

const (
	monitorAlert   = "monitor.alert"
	patternAnomaly = "pattern.anomaly"
)

var (
	monitorStates = []string{"ALERT", "WARN", "RECOVERED", "NO DATA"}
	monitorTypes  = []string{
		"metric_threshold", "metric_change", "metric_anomaly", "log_threshold", "event_threshold",
		"pattern_anomaly", "composite", "synthetic", "notification_failed",
	}
)

// EventType describes a type of event as events/list returns it.
type EventType struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Delivery      []string       `json:"delivery"`
	InputSchema   map[string]any `json:"inputSchema"`
	PayloadSchema map[string]any `json:"payloadSchema"`

	// monitorType pins event.type, for types that are one kind of monitor.
	monitorType string
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func monitorInputSchema(withType bool) map[string]any {
	properties := map[string]any{
		"monitorId": str("Only this monitor's events, by its ID."),
		"monitor":   str("Only this monitor's events, by its exact name."),
		"service":   str("Only events for this service (service.name)."),
		"states": map[string]any{
			"type":        "array",
			"description": "Only these states. All of them when not given.",
			"items":       map[string]any{"type": "string", "enum": monitorStates},
			"uniqueItems": true,
		},
		"query": str(`More CQL to narrow the events, ANDed with the rest, e.g. @ed.monitor.priority:"High".`),
	}
	if withType {
		properties["type"] = map[string]any{
			"type":        "string",
			"description": "Only monitors of this type.",
			"enum":        monitorTypes,
		}
	}
	return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
}

func monitorPayloadSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":          str("The monitor's notification title."),
			"url":            str("The event in the Edge Delta app."),
			"monitorId":      str("The monitor's ID."),
			"monitorName":    str("The monitor's name."),
			"monitorType":    map[string]any{"type": "string", "enum": monitorTypes},
			"state":          map[string]any{"type": "string", "enum": monitorStates, "description": "The state the monitor moved to."},
			"priority":       str("The monitor's priority, such as High."),
			"service":        str("The service the event is for (service.name), when the monitor groups by it."),
			"group":          str("The group the monitor evaluated, such as service.name:api."),
			"message":        str("The notification message, in Markdown."),
			"value":          str("The value the monitor evaluated."),
			"evaluatedFrom":  map[string]any{"type": "string", "format": "date-time"},
			"evaluatedTo":    map[string]any{"type": "string", "format": "date-time"},
			"observationUrl": str("What the monitor observed, in the Edge Delta app."),
			"screenshotUrl":  str("A chart of what the monitor observed."),
			"patterns": map[string]any{
				"type":        "array",
				"description": "The log patterns behind a pattern anomaly, each with its count.",
				"items":       map[string]any{"type": "string"},
			},
			"resource": map[string]any{
				"type":                 "object",
				"description":          "The event's resource attributes, which carry the monitor's group-by dimensions.",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
		"required": []string{"title", "url", "monitorName", "state"},
	}
}

var eventTypes = []EventType{
	{
		Name:          monitorAlert,
		Description:   "Fires when an Edge Delta monitor changes state: it alerts, warns, recovers or sees no data.",
		Delivery:      []string{"poll"},
		InputSchema:   monitorInputSchema(true),
		PayloadSchema: monitorPayloadSchema(),
	},
	{
		Name:          patternAnomaly,
		Description:   "Fires when a pattern anomaly monitor changes state, with the log patterns behind it.",
		Delivery:      []string{"poll"},
		InputSchema:   monitorInputSchema(false),
		PayloadSchema: monitorPayloadSchema(),
		monitorType:   "pattern_anomaly",
	},
}

func lookupType(name string) (EventType, bool) {
	i := slices.IndexFunc(eventTypes, func(t EventType) bool { return t.Name == name })
	if i < 0 {
		return EventType{}, false
	}
	return eventTypes[i], true
}

// query turns a subscription's arguments into the CQL that finds its events.
func (t EventType) query(args map[string]any) (string, error) {
	properties := t.InputSchema["properties"].(map[string]any)
	clauses := []string{`event.domain:("Monitor" OR "Monitor Alerts")`}
	if t.monitorType != "" {
		clauses = append(clauses, fmt.Sprintf("event.type:%s", quote(t.monitorType)))
	}

	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if _, ok := properties[k]; !ok {
			return "", fmt.Errorf("%s takes no argument %q", t.Name, k)
		}
		v := args[k]
		if k == "states" {
			states, err := stringList(v)
			if err != nil {
				return "", fmt.Errorf("states: %w", err)
			}
			if len(states) == 0 {
				continue
			}
			quoted := make([]string, 0, len(states))
			for _, s := range states {
				if !slices.Contains(monitorStates, s) {
					return "", fmt.Errorf("states: %q is not one of %s", s, strings.Join(monitorStates, ", "))
				}
				quoted = append(quoted, quote(s))
			}
			clauses = append(clauses, fmt.Sprintf("severity_text:(%s)", strings.Join(quoted, " OR ")))
			continue
		}

		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("%s must be a string", k)
		}
		if s == "" {
			continue
		}
		switch k {
		case "monitorId":
			clauses = append(clauses, fmt.Sprintf("ed.monitor.id:%s", quote(s)))
		case "monitor":
			clauses = append(clauses, fmt.Sprintf("event.name:%s", quote(s)))
		case "service":
			clauses = append(clauses, fmt.Sprintf("service.name:%s", quote(s)))
		case "type":
			if !slices.Contains(monitorTypes, s) {
				return "", fmt.Errorf("type: %q is not one of %s", s, strings.Join(monitorTypes, ", "))
			}
			clauses = append(clauses, fmt.Sprintf("event.type:%s", quote(s)))
		case "query":
			clauses = append(clauses, fmt.Sprintf("(%s)", s))
		}
	}
	return strings.Join(clauses, " AND "), nil
}

func stringList(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array of strings")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("must be an array of strings")
		}
		out = append(out, s)
	}
	return out, nil
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
