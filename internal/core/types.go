// Package core holds the types shared by every patrolbox package.
package core

import (
	"encoding/json"
	"time"
)

// Status is the outcome of a single check, ordered by severity.
type Status int

const (
	OK Status = iota
	Warn
	Crit
	Unknown
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Crit:
		return "crit"
	default:
		return "unknown"
	}
}

// MarshalJSON writes the status as its lowercase name.
func (s Status) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// UnmarshalJSON reads a status from its lowercase name.
func (s *Status) UnmarshalJSON(b []byte) error {
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch v {
	case "ok":
		*s = OK
	case "warn":
		*s = Warn
	case "crit":
		*s = Crit
	default:
		*s = Unknown
	}
	return nil
}

// Result is what one check reports about one target.
//
// Checks never build human text themselves: they set MsgKey to a key that
// exists in internal/i18n/locales/en/*.json and put the values the message
// needs into Args. The report layer fills Message in the user's language.
type Result struct {
	Check   string         `json:"check"`  // "http", "tls", "domain", "docker", "disk", "backup"
	Target  string         `json:"target"` // host name, container name, mount point, backup name
	Status  Status         `json:"status"`
	MsgKey  string         `json:"-"`
	Args    map[string]any `json:"-"`
	Message string         `json:"message"`
	Took    time.Duration  `json:"-"`
}

// ID identifies a result across runs, for change detection.
func (r Result) ID() string { return r.Check + ":" + r.Target }

// Site is a host name found by discovery or listed in the config.
type Site struct {
	Host   string `json:"host"`
	Source string `json:"source"` // "caddy", "nginx", "docker", "config"
}

// Worst returns the most severe status in rs (Unknown ranks between Warn and Crit).
func Worst(rs []Result) Status {
	rank := func(s Status) int {
		switch s {
		case OK:
			return 0
		case Warn:
			return 1
		case Unknown:
			return 2
		default:
			return 3
		}
	}
	w := OK
	for _, r := range rs {
		if rank(r.Status) > rank(w) {
			w = r.Status
		}
	}
	return w
}
