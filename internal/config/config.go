// Package config loads patrolbox settings. Every field has a working default,
// so patrolbox runs with no config file at all.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that reads "10s", "26h", "7d" from YAML.
type Duration time.Duration

func (d Duration) D() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := ParseDuration(s)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*d = Duration(v)
	return nil
}

// ParseDuration is time.ParseDuration plus a "d" (day) suffix.
func ParseDuration(s string) (time.Duration, error) {
	if n := len(s); n > 1 && s[n-1] == 'd' {
		var days float64
		if _, err := fmt.Sscanf(s[:n-1], "%g", &days); err == nil {
			return time.Duration(days * 24 * float64(time.Hour)), nil
		}
	}
	return time.ParseDuration(s)
}

type Config struct {
	Lang      string   `yaml:"lang"`       // "" = detect from LANG / LC_ALL
	StateFile string   `yaml:"state_file"` // remembers the last status of every check
	Sites     []string `yaml:"sites"`      // extra hosts to watch, on top of discovery
	Exclude   []string `yaml:"exclude"`    // hosts to skip; "*.example.com" wildcards allowed

	Discover Discover `yaml:"discover"`
	HTTP     HTTP     `yaml:"http"`
	TLS      TLS      `yaml:"tls"`
	Domain   Domain   `yaml:"domain"`
	Docker   Docker   `yaml:"docker"`
	Disk     Disk     `yaml:"disk"`
	Backups  []Backup `yaml:"backups"`
	Notify   Notify   `yaml:"notify"`
}

// Discover controls where site names are looked up. A path of "auto" tries
// the usual locations, "off" disables that source.
type Discover struct {
	Caddy  string `yaml:"caddy"`  // "auto", "off", or a Caddyfile path (globs allowed)
	Nginx  string `yaml:"nginx"`  // "auto", "off", or a config path/glob
	Docker bool   `yaml:"docker"` // read caddy / traefik labels from running containers
}

type HTTP struct {
	Enabled bool     `yaml:"enabled"`
	Timeout Duration `yaml:"timeout"`
	Slow    Duration `yaml:"slow"`     // slower than this = warn
	Contain string   `yaml:"contain"`  // optional text every page must contain
	MaxCode int      `yaml:"max_code"` // highest status code still counted as up (default 399)
}

type TLS struct {
	Enabled  bool     `yaml:"enabled"`
	Timeout  Duration `yaml:"timeout"`
	WarnDays int      `yaml:"warn_days"`
	CritDays int      `yaml:"crit_days"`
}

type Domain struct {
	Enabled  bool     `yaml:"enabled"`
	Timeout  Duration `yaml:"timeout"`
	WarnDays int      `yaml:"warn_days"`
	CritDays int      `yaml:"crit_days"`
}

type Docker struct {
	Enabled      bool     `yaml:"enabled"`
	Socket       string   `yaml:"socket"`
	MaxRestarts  int      `yaml:"max_restarts"` // restart count above this = warn
	Ignore       []string `yaml:"ignore"`       // container names to skip; wildcards allowed
	RequireNames []string `yaml:"require"`      // containers that must be running
}

type Disk struct {
	Enabled     bool     `yaml:"enabled"`
	WarnPercent int      `yaml:"warn_percent"`
	CritPercent int      `yaml:"crit_percent"`
	Mounts      []string `yaml:"mounts"` // empty = every real filesystem
}

// Backup checks that the newest file matching Path is fresh and not tiny.
type Backup struct {
	Name    string   `yaml:"name"`
	Path    string   `yaml:"path"`     // file, directory, or glob
	MaxAge  Duration `yaml:"max_age"`  // default 26h
	MinSize int64    `yaml:"min_size"` // bytes; 0 = any non-empty file
}

type Notify struct {
	// Repeat re-sends a still-failing alert after this long. 0 = only on change.
	Repeat   Duration `yaml:"repeat"`
	Telegram Telegram `yaml:"telegram"`
	Webhook  Webhook  `yaml:"webhook"`
	Slack    Webhook  `yaml:"slack"`
	Discord  Webhook  `yaml:"discord"`
	Ntfy     Webhook  `yaml:"ntfy"`
	Email    Email    `yaml:"email"`
}

type Telegram struct {
	BotToken string `yaml:"bot_token"`
	ChatID   string `yaml:"chat_id"`
}

type Webhook struct {
	URL string `yaml:"url"`
}

type Email struct {
	Host     string   `yaml:"host"`
	Port     int      `yaml:"port"`
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
	From     string   `yaml:"from"`
	To       []string `yaml:"to"`
}

// Default returns the settings used when no config file exists.
func Default() Config {
	return Config{
		StateFile: "/var/lib/patrolbox/state.json",
		Discover:  Discover{Caddy: "auto", Nginx: "auto", Docker: true},
		HTTP:      HTTP{Enabled: true, Timeout: Duration(10 * time.Second), Slow: Duration(3 * time.Second), MaxCode: 399},
		TLS:       TLS{Enabled: true, Timeout: Duration(10 * time.Second), WarnDays: 14, CritDays: 5},
		Domain:    Domain{Enabled: true, Timeout: Duration(10 * time.Second), WarnDays: 30, CritDays: 7},
		Docker:    Docker{Enabled: true, Socket: "/var/run/docker.sock", MaxRestarts: 3},
		Disk:      Disk{Enabled: true, WarnPercent: 80, CritPercent: 90},
	}
}

// DefaultPaths are tried in order when --config is not given.
var DefaultPaths = []string{"./patrolbox.yml", "/etc/patrolbox/patrolbox.yml"}

// Load reads path on top of the defaults. An empty path tries DefaultPaths
// and falls back to pure defaults when none exists. ${VAR} is expanded from
// the environment so secrets can stay out of the file.
func Load(path string) (Config, string, error) {
	cfg := Default()
	paths := DefaultPaths
	if path != "" {
		paths = []string{path}
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) && path == "" {
			continue
		}
		if err != nil {
			return cfg, p, err
		}
		dec := yaml.NewDecoder(stringsReader(os.Expand(string(raw), expandEnv)))
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil && err.Error() != "EOF" {
			return cfg, p, fmt.Errorf("%s: %w", p, err)
		}
		cfg.fill()
		return cfg, p, nil
	}
	return cfg, "", nil
}

// expandEnv keeps a literal "$" that is not followed by a known variable
// (passwords often contain one).
func expandEnv(name string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return "$" + name
}

func (c *Config) fill() {
	for i := range c.Backups {
		if c.Backups[i].MaxAge == 0 {
			c.Backups[i].MaxAge = Duration(26 * time.Hour)
		}
		if c.Backups[i].Name == "" {
			c.Backups[i].Name = c.Backups[i].Path
		}
	}
	if c.HTTP.MaxCode == 0 {
		c.HTTP.MaxCode = 399
	}
}
