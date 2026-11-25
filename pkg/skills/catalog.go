// Package skills serves Edge Delta's agent skills through the MCP Skills extension (SEP-2640).
//
// The skills are synced from github.com/edgedelta/agent-skills into data/ by script/sync-skills and embedded in the
// binary, so every build serves the skills it was built with. Each file of a skill is a skill://<skill>/<path>
// resource, listed in the skill's entry with its size and sha256 digest.
package skills

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Extension is the extension's identifier under the server capabilities' extensions.
const Extension = "io.modelcontextprotocol/skills"

// DirectoryMIMEType is the mimeType of a directory resource.
const DirectoryMIMEType = "inode/directory"

// The per-skill limits hosts must accept; a server should not publish a skill over them.
const (
	maxFiles = 512
	maxBytes = 16 << 20
)

//go:embed all:data
var embedded embed.FS

// Embedded returns the catalog of the skills embedded in the binary.
func Embedded() (*Catalog, error) {
	data, err := fs.Sub(embedded, "data")
	if err != nil {
		return nil, err
	}
	return NewCatalog(data)
}

// Skill is a skill's entry in skills/list and skills/get.
type Skill struct {
	URI         string         `json:"uri"`
	Frontmatter map[string]any `json:"frontmatter"`
	Resources   []Resource     `json:"resources"`
}

// Resource is one file of a skill, in its entry.
type Resource struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}

// Name is the skill's frontmatter name.
func (s Skill) Name() string { return s.Frontmatter["name"].(string) }

// Description is the skill's frontmatter description.
func (s Skill) Description() string { return s.Frontmatter["description"].(string) }

// DirEntry is a direct child of a directory, in resources/directory/read.
type DirEntry struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	MIMEType string `json:"mimeType"`
	Size     *int   `json:"size,omitempty"`
}

type file struct {
	data     []byte
	mimeType string
}

// Catalog holds the skills of one folder: each directory under it with a SKILL.md is a skill named after it.
type Catalog struct {
	skills []Skill // sorted by URI
	byURI  map[string]int
	files  map[string]file
	dirs   map[string][]DirEntry
}

// NewCatalog reads the skills of fsys, failing on any skill that breaks the extension's or Agent Skills' rules.
func NewCatalog(fsys fs.FS) (*Catalog, error) {
	c := &Catalog{skills: []Skill{}, byURI: map[string]int{}, files: map[string]file{}, dirs: map[string][]DirEntry{}}
	top, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	for _, e := range top {
		if !e.IsDir() || hidden(e.Name()) {
			continue
		}
		if _, err := fs.Stat(fsys, path.Join(e.Name(), "SKILL.md")); err != nil {
			continue
		}
		skill, err := c.load(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("skill %s: %w", e.Name(), err)
		}
		c.skills = append(c.skills, skill)
	}
	sort.Slice(c.skills, func(i, j int) bool { return c.skills[i].URI < c.skills[j].URI })
	for i, s := range c.skills {
		c.byURI[s.URI] = i
	}
	return c, nil
}

func hidden(name string) bool { return strings.HasPrefix(name, ".") }

// URI returns the skill:// URI of a path under the catalog, its first segment being the skill.
func URI(p string) string { return "skill://" + p }

var namePattern = regexp.MustCompile(`^[0-9a-z]+(-[0-9a-z]+)*$`)

func (c *Catalog) load(fsys fs.FS, dir string) (Skill, error) {
	skill := Skill{URI: URI(dir + "/SKILL.md")}
	total := 0
	err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if hidden(d.Name()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p != dir && !uriSafe(d.Name()) {
			return fmt.Errorf("%s: the name needs percent-encoding in a URI; rename it", p)
		}
		if d.IsDir() {
			c.dirs[URI(p)] = []DirEntry{}
			if p != dir {
				parent := URI(path.Dir(p))
				c.dirs[parent] = append(c.dirs[parent], DirEntry{URI: URI(p), Name: d.Name(), MIMEType: DirectoryMIMEType})
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: not a regular file", p)
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		uri := URI(p)
		size := len(data)
		f := file{data: data, mimeType: mimeTypeFor(d.Name())}
		c.files[uri] = f
		parent := URI(path.Dir(p))
		c.dirs[parent] = append(c.dirs[parent], DirEntry{URI: uri, Name: d.Name(), MIMEType: f.mimeType, Size: &size})
		skill.Resources = append(skill.Resources, Resource{URI: uri, Digest: Digest(data), Size: size})
		total += size
		return nil
	})
	if err != nil {
		return Skill{}, err
	}
	sort.Slice(skill.Resources, func(i, j int) bool { return skill.Resources[i].URI < skill.Resources[j].URI })
	for _, children := range c.dirs {
		sort.Slice(children, func(i, j int) bool { return children[i].URI < children[j].URI })
	}
	if len(skill.Resources) > maxFiles || total > maxBytes {
		return Skill{}, fmt.Errorf("%d files / %d bytes is over the limit of %d files / %d bytes", len(skill.Resources), total, maxFiles, maxBytes)
	}
	skill.Frontmatter, err = parseFrontmatter(c.files[skill.URI].data)
	if err != nil {
		return Skill{}, fmt.Errorf("SKILL.md frontmatter: %w", err)
	}
	if err := validate(skill.Frontmatter, dir); err != nil {
		return Skill{}, err
	}
	return skill, nil
}

// uriSafe reports whether a file name stands for itself in a URI path, so its URI needs no encoding.
func uriSafe(name string) bool {
	if name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("-._~!$&'()*+,;=:@", r):
		default:
			return false
		}
	}
	return true
}

func validate(fm map[string]any, dir string) error {
	name, ok := fm["name"].(string)
	if !ok {
		return fmt.Errorf("frontmatter must have a string name")
	}
	description, ok := fm["description"].(string)
	if !ok {
		return fmt.Errorf("frontmatter must have a string description")
	}
	if len(name) > 64 || !namePattern.MatchString(name) {
		return fmt.Errorf("name %q must be 1-64 lowercase letters, digits and single hyphens", name)
	}
	if name != dir {
		return fmt.Errorf("name %q must match its folder %q", name, dir)
	}
	if n := utf8.RuneCountInString(description); n == 0 || n > 1024 {
		return fmt.Errorf("description must be 1-1024 characters")
	}
	if v, ok := fm["compatibility"]; ok {
		s, ok := v.(string)
		if n := utf8.RuneCountInString(s); !ok || n == 0 || n > 500 {
			return fmt.Errorf("compatibility must be a string of 1-500 characters")
		}
	}
	if v, ok := fm["metadata"]; ok {
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("metadata must map strings to strings")
		}
		for k, item := range m {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("metadata %q must be a string; quote it", k)
			}
		}
	}
	return nil
}

// parseFrontmatter reads SKILL.md's frontmatter as a JSON object, as a client parses it to check the entry.
func parseFrontmatter(md []byte) (map[string]any, error) {
	text := strings.ReplaceAll(string(bytes.TrimPrefix(md, []byte("\xef\xbb\xbf"))), "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, fmt.Errorf(`SKILL.md must begin with a "---" line`)
	}
	end := regexp.MustCompile(`(?m)^---[\t ]*$`).FindStringIndex(rest)
	if end == nil {
		return nil, fmt.Errorf(`no closing "---" line`)
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end[0]]), &fm); err != nil {
		return nil, err
	}
	if fm == nil {
		fm = map[string]any{}
	}
	v, err := jsonValue(fm)
	if err != nil {
		return nil, err
	}
	return v.(map[string]any), nil
}

// jsonValue checks that a YAML value means the same in JSON, and to a client's YAML 1.2 core schema reader.
func jsonValue(v any) (any, error) {
	switch v := v.(type) {
	case nil, string, bool, int:
		return v, nil
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, fmt.Errorf("%v cannot be represented in JSON; quote it", v)
		}
		return v, nil
	case time.Time:
		return nil, fmt.Errorf("%s reads as a timestamp; quote it", v.Format(time.RFC3339))
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			var err error
			if out[i], err = jsonValue(item); err != nil {
				return nil, err
			}
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			var err error
			if out[k], err = jsonValue(item); err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported value %v (%T); quote it", v, v)
	}
}

// Digest is the extension's digest of a file: sha256: and the lowercase hex SHA-256 of its bytes.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var mimeTypes = map[string]string{
	"md": "text/markdown", "markdown": "text/markdown", "txt": "text/plain", "json": "application/json",
	"yaml": "application/yaml", "yml": "application/yaml", "py": "text/x-python", "sh": "application/x-sh",
	"bash": "application/x-sh", "js": "text/javascript", "mjs": "text/javascript", "ts": "text/typescript",
	"html": "text/html", "css": "text/css", "csv": "text/csv", "xml": "application/xml", "svg": "image/svg+xml",
	"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp",
	"pdf": "application/pdf",
}

func mimeTypeFor(name string) string {
	dot := strings.LastIndex(name, ".")
	if dot <= 0 {
		return "application/octet-stream"
	}
	if t, ok := mimeTypes[strings.ToLower(name[dot+1:])]; ok {
		return t
	}
	return "application/octet-stream"
}

// Skills returns every skill, sorted by URI.
func (c *Catalog) Skills() []Skill { return c.skills }

// Get returns the skill whose SKILL.md is at uri.
func (c *Catalog) Get(uri string) (Skill, bool) {
	i, ok := c.byURI[uri]
	if !ok {
		return Skill{}, false
	}
	return c.skills[i], true
}

// Read returns a skill file's bytes and mimeType.
func (c *Catalog) Read(uri string) ([]byte, string, bool) {
	f, ok := c.files[uri]
	return f.data, f.mimeType, ok
}

// Directory returns a directory's direct children, sorted by URI.
func (c *Catalog) Directory(uri string) ([]DirEntry, bool) {
	children, ok := c.dirs[uri]
	return children, ok
}

// IsDirectory reports whether uri is a directory of a skill.
func (c *Catalog) IsDirectory(uri string) bool {
	_, ok := c.dirs[uri]
	return ok
}
