package events

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const linkWindow = 30 * time.Minute

// searchItem is an event as the event search API returns it.
type searchItem struct {
	Timestamp    int64             `json:"timestamp"`
	EventDomain  string            `json:"event_domain"`
	EventType    string            `json:"event_type"`
	SeverityText string            `json:"severity_text"`
	Body         string            `json:"body"`
	Resource     map[string]string `json:"resource"`
	Attributes   map[string]string `json:"attributes"`
}

// Occurrence is one event as the extension delivers it.
type Occurrence struct {
	EventID   string         `json:"eventId"`
	Name      string         `json:"name"`
	Timestamp string         `json:"timestamp"`
	Data      map[string]any `json:"data"`
}

// id is the event's own ID, or a digest of its content for events written before they had one.
func (it searchItem) id() string {
	if id := it.Attributes["event.id"]; id != "" {
		return id
	}
	parts := []string{strconv.FormatInt(it.Timestamp, 10), it.EventDomain, it.EventType, it.SeverityText, it.Body}
	keys := make([]string, 0, len(it.Resource))
	for k := range it.Resource {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		parts = append(parts, k+"="+it.Resource[k])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])[:32]
}

func (it searchItem) occurrence(name, appURL string) Occurrence {
	at := time.UnixMilli(it.Timestamp).UTC()
	id := it.id()
	find := fmt.Sprintf("@event.id:%s", id)
	if it.Attributes["event.id"] == "" {
		find = fmt.Sprintf("event.name:%s", quote(it.Resource["event.name"]))
	}
	data := map[string]any{
		"title":       it.Body,
		"url":         eventLink(appURL, find, at),
		"monitorName": it.Resource["event.name"],
		"state":       it.SeverityText,
		"monitorType": it.EventType,
	}
	set := func(key, value string) {
		if value != "" {
			data[key] = value
		}
	}
	set("monitorId", it.Resource["ed.monitor.id"])
	set("service", it.Resource["service.name"])
	set("priority", it.Attributes["ed.monitor.priority"])
	set("group", it.Attributes["ed.monitor.group.name"])
	set("message", it.Attributes["ed.monitor.notification.message"])
	set("value", it.Attributes["ed.monitor.evaluated.value"])
	set("evaluatedFrom", it.Attributes["ed.monitor.evaluated.from"])
	set("evaluatedTo", it.Attributes["ed.monitor.evaluated.to"])
	set("observationUrl", it.Attributes["ed.monitor.observation.url"])
	set("screenshotUrl", it.Attributes["ed.monitor.screenshot.url"])
	if p := patterns(it.Attributes); len(p) > 0 {
		data["patterns"] = p
	}
	if len(it.Resource) > 0 {
		data["resource"] = it.Resource
	}
	return Occurrence{EventID: id, Name: name, Timestamp: at.Format(time.RFC3339Nano), Data: data}
}

// patterns are ed.monitor.patterns.0, .1, ... in order.
func patterns(attrs map[string]string) []string {
	type indexed struct {
		i int
		s string
	}
	var found []indexed
	for k, v := range attrs {
		n, ok := strings.CutPrefix(k, "ed.monitor.patterns.")
		if !ok {
			continue
		}
		i, err := strconv.Atoi(n)
		if err != nil {
			continue
		}
		found = append(found, indexed{i, v})
	}
	slices.SortFunc(found, func(a, b indexed) int { return a.i - b.i })
	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, f.s)
	}
	return out
}

// eventLink opens the event in the app's monitor events, as monitor notifications link to it.
func eventLink(appURL, query string, at time.Time) string {
	q := url.Values{}
	q.Set("query", query)
	q.Set("from", at.Add(-linkWindow).Format(timeFormat))
	q.Set("to", at.Add(linkWindow).Format(timeFormat))
	return fmt.Sprintf("%s/events/monitors?%s", strings.TrimSuffix(appURL, "/"), q.Encode())
}
