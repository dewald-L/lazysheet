// Package sheet reads osheet's config and reads and writes its timesheet
// workbooks. It is a port of osheet's config, path and Excel code
// (~/Projects/osheet/osheet): the files it writes must stay readable by osheet
// and by Excel, so behaviour here follows osheet exactly.
package sheet

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultSentiments is used when the config has no sentiments (as osheet does).
var DefaultSentiments = []string{"Neutral", "Happy", "Sad"}

// AskFields are the entry questions that can be skipped, in form order. The
// description is always asked.
var AskFields = []string{"day", "project", "category", "time", "location", "sentiment"}

// notAskedByDefault are skipped unless ask_every_time turns them on.
var notAskedByDefault = map[string]bool{"sentiment": true}

// Project is one project from the template's Lookup sheet.
type Project struct {
	Name       string
	Billable   string // "Yes" or "No"
	Categories []string
}

// Defaults are the answers used for questions that aren't asked every time.
type Defaults struct {
	Project, Category, Location, Sentiment string
	// Time is the default duration as osheet stores it (e.g. "1h"); empty when unset.
	Time string
}

// Config is osheet's config.json. lazysheet only reads it.
type Config struct {
	Template     string
	Output       string
	GroupBy      string
	Defaults     Defaults
	AskEveryTime map[string]any
	Projects     []Project // in config (template) order
	Locations    []string
	Sentiments   []string
}

// ErrNoConfig means osheet hasn't been set up yet.
var ErrNoConfig = errors.New("no osheet config")

// ConfigDir is osheet's config directory (config_dir in osheet).
func ConfigDir() string {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("APPDATA")
		if base == "" {
			base = ExpandUser(`~\AppData\Roaming`)
		}
	case "darwin":
		base = ExpandUser("~/Library/Application Support")
	default:
		base = os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = ExpandUser("~/.config")
		}
	}
	return filepath.Join(base, "osheet")
}

// ConfigPath is osheet's config.json.
func ConfigPath() string { return filepath.Join(ConfigDir(), "config.json") }

// TemplateCopyPath is the template copy osheet keeps next to its config.
func TemplateCopyPath() string { return filepath.Join(ConfigDir(), "template.xlsx") }

// LoadConfig reads osheet's config. A missing or unreadable file (osheet
// treats both as "not set up") returns an error wrapping ErrNoConfig.
func LoadConfig() (*Config, error) {
	return loadConfig(ConfigPath())
}

type rawConfig struct {
	Template     string          `json:"template"`
	Output       string          `json:"output"`
	GroupBy      string          `json:"group_by"`
	Defaults     map[string]any  `json:"defaults"`
	AskEveryTime map[string]any  `json:"ask_every_time"`
	Projects     orderedProjects `json:"projects"`
	Locations    []string        `json:"locations"`
	Sentiments   []string        `json:"sentiments"`
	Columns      []string        `json:"columns"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoConfig, path)
	}
	var raw rawConfig
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: %s is not valid JSON (%v)", ErrNoConfig, path, err)
	}
	if len(raw.Projects) == 0 {
		return nil, fmt.Errorf("%s has no projects; run osheet --setup again", path)
	}
	if raw.Output == "" {
		return nil, fmt.Errorf("%s has no output folder; run osheet --setup again", path)
	}
	cfg := &Config{
		Template:     raw.Template,
		Output:       raw.Output,
		GroupBy:      raw.GroupBy,
		AskEveryTime: raw.AskEveryTime,
		Projects:     raw.Projects,
		Locations:    raw.Locations,
		Sentiments:   raw.Sentiments,
		Defaults: Defaults{
			Project:   pyStr(raw.Defaults["project"]),
			Category:  pyStr(raw.Defaults["category"]),
			Location:  pyStr(raw.Defaults["location"]),
			Sentiment: pyStr(raw.Defaults["sentiment"]),
			Time:      pyStr(raw.Defaults["time"]),
		},
	}
	return cfg, nil
}

// pyStr is Python's str() for a JSON value as osheet would see it (absent → "").
func pyStr(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "True"
		}
		return "False"
	}
	return fmt.Sprint(v)
}

// orderedProjects decodes the "projects" object keeping its key order, which
// is the order osheet lists projects in.
type orderedProjects []Project

func (p *orderedProjects) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("projects: expected an object")
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		var v struct {
			Billable   string   `json:"billable"`
			Categories []string `json:"categories"`
		}
		if err := dec.Decode(&v); err != nil {
			return err
		}
		*p = append(*p, Project{Name: key.(string), Billable: v.Billable, Categories: v.Categories})
	}
	return nil
}

// ProjectNames lists the projects in config order.
func (c *Config) ProjectNames() []string {
	out := make([]string, len(c.Projects))
	for i, p := range c.Projects {
		out[i] = p.Name
	}
	return out
}

// Project returns the named project, or nil.
func (c *Config) Project(name string) *Project {
	for i := range c.Projects {
		if c.Projects[i].Name == name {
			return &c.Projects[i]
		}
	}
	return nil
}

// SentimentOptions are the configured sentiments, or osheet's defaults.
func (c *Config) SentimentOptions() []string {
	if len(c.Sentiments) > 0 {
		return c.Sentiments
	}
	return DefaultSentiments
}

// Asking reports, per entry question, whether it is asked every time
// (osheet's ask_flags): only an explicit false skips a question, and a
// missing flag means "ask" except for the sentiment.
func (c *Config) Asking() map[string]bool {
	out := make(map[string]bool, len(AskFields))
	for _, k := range AskFields {
		v, ok := c.AskEveryTime[k]
		if !ok {
			out[k] = !notAskedByDefault[k]
			continue
		}
		b, isBool := v.(bool)
		out[k] = !(isBool && !b)
	}
	return out
}

// ExpandUser is Python's os.path.expanduser for "~" and "~/...".
func ExpandUser(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home := os.Getenv("HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		} else {
			return p
		}
	}
	return home + p[1:]
}

// absPath is Python's os.path.abspath(os.path.expanduser(p)).
func absPath(p string) string {
	p = ExpandUser(p)
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
}

// TildePath shortens a path under the home directory to ~/...
func TildePath(p string) string {
	home := os.Getenv("HOME")
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}
