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
	monitorTriggered   = "monitor.triggered"
	monitorEscalated   = "monitor.escalated"
	monitorDeescalated = "monitor.deescalated"
	monitorRenotified  = "monitor.renotified"
	monitorNoData      = "monitor.no_data"
	monitorRecovered   = "monitor.recovered"
	patternAnomaly     = "pattern.anomaly"
)

var (
	monitorStates = []string{"ALERT", "WARN", "RECOVERED", "NO DATA"}
	// groupStates are the states a monitor group moves between; RECOVERED is the event of reaching OK.
	groupStates  = []string{"OK", "WARN", "ALERT", "NO DATA"}
	transitions  = []string{"triggered", "escalated", "deescalated", "renotified", "no_data", "recovered"}
	monitorTypes = []string{
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
	// transition pins ed.monitor.transition.kind, for types that are one kind of change.
	transition string
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func enumList(description string, values []string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items":       map[string]any{"type": "string", "enum": values},
		"uniqueItems": true,
	}
}

// monitorInputSchema takes states, the states a group moves to, and from, the states it leaves, where the type
// has a choice of them.
func monitorInputSchema(withType bool, states, from []string) map[string]any {
	properties := map[string]any{
		"monitorId": str("Only this monitor's events, by its ID."),
		"monitor":   str("Only this monitor's events, by its exact name."),
		"service":   str("Only events for this service (service.name)."),
		"query":     str(`More CQL to narrow the events, ANDed with the rest, e.g. @ed.monitor.priority:"High".`),
	}
	if len(states) > 0 {
		properties["states"] = enumList("Only these states. All of them when not given.", states)
	}
	if len(from) > 0 {
		properties["from"] = enumList("Only changes from these states. All of them when not given.", from)
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
			"title":       str("The monitor's notification title."),
			"url":         str("The event in the Edge Delta app."),
			"monitorId":   str("The monitor's ID."),
			"monitorName": str("The monitor's name."),
			"monitorType": map[string]any{"type": "string", "enum": monitorTypes},
			"state":       map[string]any{"type": "string", "enum": monitorStates, "description": "The state the monitor moved to."},
			"transition":  map[string]any{"type": "string", "enum": transitions, "description": "The kind of change, the type's name without monitor."},
			"from":        map[string]any{"type": "string", "enum": groupStates, "description": "The group's state before the event."},
			"to":          map[string]any{"type": "string", "enum": groupStates, "description": "The group's state after the event, OK once it recovered."},
			"episodeStartedAt": map[string]any{
				"type":        "string",
				"format":      "date-time",
				"description": "When the group left OK. The event's subject names this episode until the group recovers.",
			},
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
		Name: monitorTriggered,
		Description: "Fires when an Edge Delta monitor group starts alerting: it moves from OK or NO DATA to ALERT or WARN. " +
			"Its subject names the episode this starts, which the group's later events share until it recovers.",
		Delivery:      []string{"poll"},
		InputSchema:   monitorInputSchema(true, []string{"ALERT", "WARN"}, nil),
		PayloadSchema: monitorPayloadSchema(),
		transition:    "triggered",
	},
	{
		Name:          monitorEscalated,
		Description:   "Fires when a monitor group that warns starts alerting, WARN to ALERT, within its episode.",
		Delivery:      []string{"poll"},
		InputSchema:   monitorInputSchema(true, nil, nil),
		PayloadSchema: monitorPayloadSchema(),
		transition:    "escalated",
	},
	{
		Name:          monitorDeescalated,
		Description:   "Fires when a monitor group that alerts eases to a warning, ALERT to WARN, within its episode.",
		Delivery:      []string{"poll"},
		InputSchema:   monitorInputSchema(true, nil, nil),
		PayloadSchema: monitorPayloadSchema(),
		transition:    "deescalated",
	},
	{
		Name: monitorRenotified,
		Description: "Fires as a reminder that a monitor group is still in ALERT, WARN or NO DATA, at the monitor's " +
			"renotification interval. It repeats the episode's subject.",
		Delivery:      []string{"poll"},
		InputSchema:   monitorInputSchema(true, []string{"ALERT", "WARN", "NO DATA"}, nil),
		PayloadSchema: monitorPayloadSchema(),
		transition:    "renotified",
	},
	{
		Name: monitorNoData,
		Description: "Fires when a monitor stops seeing data for a group, from OK, which starts an episode, or from " +
			"ALERT or WARN within one.",
		Delivery:      []string{"poll"},
		InputSchema:   monitorInputSchema(true, nil, []string{"OK", "ALERT", "WARN"}),
		PayloadSchema: monitorPayloadSchema(),
		transition:    "no_data",
	},
	{
		Name:          monitorRecovered,
		Description:   "Fires when a monitor group returns to OK, ending its episode.",
		Delivery:      []string{"poll"},
		InputSchema:   monitorInputSchema(true, nil, []string{"ALERT", "WARN", "NO DATA"}),
		PayloadSchema: monitorPayloadSchema(),
		transition:    "recovered",
	},
	{
		Name:          patternAnomaly,
		Description:   "Fires on every state change of a pattern anomaly monitor, with the log patterns behind it.",
		Delivery:      []string{"poll"},
		InputSchema:   monitorInputSchema(false, monitorStates, nil),
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
	if t.transition != "" {
		clauses = append(clauses, fmt.Sprintf("@ed.monitor.transition.kind:%s", quote(t.transition)))
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
		if field, ok := listFields[k]; ok {
			values, err := stringList(v)
			if err != nil {
				return "", fmt.Errorf("%s: %w", k, err)
			}
			if len(values) == 0 {
				continue
			}
			allowed := properties[k].(map[string]any)["items"].(map[string]any)["enum"].([]string)
			quoted := make([]string, 0, len(values))
			for _, s := range values {
				if !slices.Contains(allowed, s) {
					return "", fmt.Errorf("%s: %q is not one of %s", k, s, strings.Join(allowed, ", "))
				}
				quoted = append(quoted, quote(s))
			}
			clauses = append(clauses, fmt.Sprintf("%s:(%s)", field, strings.Join(quoted, " OR ")))
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
			if !grouped(s) {
				return "", fmt.Errorf("query must be one CQL expression, its parentheses and quotes balanced")
			}
			clauses = append(clauses, fmt.Sprintf("(%s)", s))
		}
	}
	return strings.Join(clauses, " AND "), nil
}

// listFields are the fields the array arguments match.
var listFields = map[string]string{
	"states": "severity_text",
	"from":   "@ed.monitor.transition.from",
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

// grouped reports whether s can't close the parentheses it's wrapped in, reading escapes and quotes as CQL's
// lexer does.
func grouped(s string) bool {
	depth, quoted, escaped := 0, false, false
	for _, r := range s {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '(':
			depth++
		case r == ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0 && !quoted && !escaped
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
