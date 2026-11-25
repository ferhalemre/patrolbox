package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "patrolbox.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsWithoutFile(t *testing.T) {
	t.Chdir(t.TempDir())
	old := DefaultPaths
	DefaultPaths = []string{"./patrolbox.yml"}
	defer func() { DefaultPaths = old }()
	cfg, used, err := Load("")
	if err != nil || used != "" {
		t.Fatalf("used=%q err=%v", used, err)
	}
	if !cfg.HTTP.Enabled || cfg.TLS.WarnDays != 14 || cfg.Disk.CritPercent != 90 {
		t.Fatalf("defaults wrong: %+v", cfg)
	}
}

func TestLoadOverridesAndEnv(t *testing.T) {
	t.Setenv("PB_TOKEN", "123:abc")
	p := write(t, `
lang: tr
sites: [example.com]
tls: {warn_days: 21}
disk: {enabled: false}
backups:
  - path: /var/backups/*.gz
  - name: db
    path: /srv/db
    max_age: 2d
notify:
  repeat: 12h
  telegram: {bot_token: "${PB_TOKEN}", chat_id: "42"}
  email: {password: "pa$$word"}
`)
	cfg, used, err := Load(p)
	if err != nil || used != p {
		t.Fatalf("used=%q err=%v", used, err)
	}
	if cfg.Lang != "tr" || cfg.TLS.WarnDays != 21 || cfg.TLS.CritDays != 5 || cfg.Disk.Enabled {
		t.Errorf("merge with defaults wrong: %+v", cfg)
	}
	if !cfg.HTTP.Enabled {
		t.Error("untouched sections must keep their defaults")
	}
	if cfg.Notify.Telegram.BotToken != "123:abc" {
		t.Errorf("env not expanded: %q", cfg.Notify.Telegram.BotToken)
	}
	if cfg.Notify.Email.Password != "pa$$word" {
		t.Errorf("a literal $ must survive: %q", cfg.Notify.Email.Password)
	}
	if cfg.Notify.Repeat.D() != 12*time.Hour {
		t.Errorf("repeat = %v", cfg.Notify.Repeat.D())
	}
	if b := cfg.Backups[0]; b.Name != "/var/backups/*.gz" || b.MaxAge.D() != 26*time.Hour {
		t.Errorf("backup defaults: %+v", b)
	}
	if b := cfg.Backups[1]; b.MaxAge.D() != 48*time.Hour {
		t.Errorf("2d = %v", b.MaxAge.D())
	}
}

func TestUnknownFieldIsAnError(t *testing.T) {
	if _, _, err := Load(write(t, "htpp: {enabled: false}\n")); err == nil {
		t.Fatal("a typo in a key must be reported, not ignored")
	}
}

func TestEmptyFile(t *testing.T) {
	if _, _, err := Load(write(t, "# nothing\n")); err != nil {
		t.Fatalf("empty config should be fine: %v", err)
	}
}

func TestMissingExplicitPath(t *testing.T) {
	if _, _, err := Load(filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("an explicit --config that does not exist must be an error")
	}
}

func TestMatch(t *testing.T) {
	pats := []string{"*.Example.com", "tmp-*", "exact.org"}
	for name, want := range map[string]bool{
		"a.example.com": true, "example.com": false, "tmp-1": true, "EXACT.org": true, "other": false,
	} {
		if got := Match(pats, name); got != want {
			t.Errorf("Match(%q) = %v, want %v", name, got, want)
		}
	}
}
