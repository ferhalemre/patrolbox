// patrolbox is a zero-config watchdog for a self-hosted server: it finds the
// sites you host and checks them, their certificates and domains, your
// containers, disks and backups, and tells you when something changes.
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ferhalemre/patrolbox/internal/checks/hostcheck"
	"github.com/ferhalemre/patrolbox/internal/checks/netcheck"
	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
	"github.com/ferhalemre/patrolbox/internal/discover"
	"github.com/ferhalemre/patrolbox/internal/i18n"
	"github.com/ferhalemre/patrolbox/internal/notify"
	"github.com/ferhalemre/patrolbox/internal/report"
	"github.com/ferhalemre/patrolbox/internal/state"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

//go:embed patrolbox.example.yml
var exampleConfig string

// Exit codes follow the monitoring-plugin convention so patrolbox can be
// used from scripts and other monitoring systems.
const (
	exitOK = iota
	exitWarn
	exitCrit
	exitUnknown
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	cmd := "check"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("patrolbox", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("config", "", "")
	langFlag := fs.String("lang", "", "")
	asJSON := fs.Bool("json", false, "")
	quiet := fs.Bool("quiet", false, "")
	noNotify := fs.Bool("no-notify", false, "")
	fs.Usage = func() { usage(i18n.Resolve(*langFlag)) }
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return exitOK
		}
		return exitUnknown
	}

	if cmd == "version" {
		fmt.Println("patrolbox", version)
		return exitOK
	}
	if cmd == "help" {
		usage(i18n.Resolve(*langFlag))
		return exitOK
	}

	cfg, usedPath, err := config.Load(*cfgPath)
	lang := i18n.Resolve(firstNonEmpty(*langFlag, cfg.Lang))
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T(lang, "cli.config_error", map[string]any{"error": err}))
		return exitUnknown
	}
	host, _ := os.Hostname()
	ctx := context.Background()

	switch cmd {
	case "languages":
		fmt.Println(strings.Join(i18n.Languages(), "\n"))
		return exitOK

	case "init":
		path := firstNonEmpty(*cfgPath, "patrolbox.yml")
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintln(os.Stderr, i18n.T(lang, "cli.init_exists", map[string]any{"path": path}))
			return exitUnknown
		}
		// 0600: the file will hold notification secrets.
		if err := os.WriteFile(path, []byte(exampleConfig), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitUnknown
		}
		fmt.Println(i18n.T(lang, "cli.init_written", map[string]any{"path": path}))
		return exitOK

	case "discover":
		sites := findSites(ctx, cfg)
		if *asJSON {
			_ = report.JSON(os.Stdout, host, sites, nil)
			return exitOK
		}
		if len(sites) == 0 {
			fmt.Println(i18n.T(lang, "report.no_sites", nil))
			return exitOK
		}
		for _, s := range sites {
			fmt.Printf("%-8s %s\n", s.Source, s.Host)
		}
		fmt.Println()
		fmt.Println(i18n.T(lang, "report.sites_found", map[string]any{"count": len(sites)}))
		return exitOK

	case "test":
		chans := notify.Channels(cfg.Notify)
		if len(chans) == 0 {
			fmt.Fprintln(os.Stderr, i18n.T(lang, "notify.none", nil))
			return exitUnknown
		}
		msg := notify.Message{
			Title: "patrolbox",
			Body:  i18n.T(lang, "notify.test", map[string]any{"host": host}),
			Level: "test",
		}
		if !send(ctx, lang, chans, msg, false) {
			return exitCrit
		}
		return exitOK

	case "check":
		// handled below

	default:
		fmt.Fprintln(os.Stderr, i18n.T(lang, "cli.unknown_command", map[string]any{"name": cmd}))
		usage(lang)
		return exitUnknown
	}

	sites := findSites(ctx, cfg)
	results := report.Translate(lang, runChecks(ctx, cfg, sites))

	if *asJSON {
		_ = report.JSON(os.Stdout, host, sites, results)
	} else {
		if !*quiet {
			if usedPath == "" {
				fmt.Println(i18n.T(lang, "report.config_none", nil))
			} else {
				fmt.Println(i18n.T(lang, "report.config_used", map[string]any{"path": usedPath}))
			}
			if len(sites) == 0 {
				fmt.Println(i18n.T(lang, "report.no_sites", nil))
			}
			fmt.Println()
		}
		report.Text(os.Stdout, lang, results, *quiet, useColor())
	}

	if !*noNotify {
		st, _ := state.Load(cfg.StateFile)
		diff := st.Update(results, time.Now(), cfg.Notify.Repeat.D())
		delivered := true
		if !diff.Empty() {
			if chans := notify.Channels(cfg.Notify); len(chans) > 0 {
				delivered = send(ctx, lang, chans, report.Message(lang, host, diff), *asJSON || *quiet)
			}
		}
		// If every channel failed, keep the old state so the next run retries
		// instead of silently swallowing the alert.
		if delivered {
			if err := st.Save(cfg.StateFile); err != nil {
				fmt.Fprintln(os.Stderr, i18n.T(lang, "cli.state_error", map[string]any{"path": cfg.StateFile, "error": err}))
			}
		}
	}

	switch core.Worst(results) {
	case core.OK:
		return exitOK
	case core.Warn:
		return exitWarn
	case core.Crit:
		return exitCrit
	default:
		return exitUnknown
	}
}

// findSites merges discovery with the config's own list and applies excludes.
func findSites(ctx context.Context, cfg config.Config) []core.Site {
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	found := discover.Sites(dctx, cfg.Discover, cfg.Docker.Socket)
	seen := map[string]bool{}
	var out []core.Site
	add := func(s core.Site) {
		s.Host = strings.ToLower(strings.TrimSpace(s.Host))
		bare := s.Host
		if i := strings.LastIndex(bare, ":"); i > 0 {
			bare = bare[:i]
		}
		if s.Host == "" || seen[s.Host] || config.Match(cfg.Exclude, bare) || config.Match(cfg.Exclude, s.Host) {
			return
		}
		seen[s.Host] = true
		out = append(out, s)
	}
	for _, h := range cfg.Sites {
		add(core.Site{Host: h, Source: "config"})
	}
	for _, s := range found {
		add(s)
	}
	return out
}

// runChecks runs every enabled check group concurrently.
func runChecks(ctx context.Context, cfg config.Config, sites []core.Site) []core.Result {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	hosts := make([]string, len(sites))
	for i, s := range sites {
		hosts[i] = s.Host
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		all []core.Result
	)
	group := func(enabled bool, fn func() []core.Result) {
		if !enabled {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			rs := fn()
			mu.Lock()
			all = append(all, rs...)
			mu.Unlock()
		}()
	}
	group(cfg.HTTP.Enabled && len(hosts) > 0, func() []core.Result { return netcheck.HTTP(ctx, cfg.HTTP, hosts) })
	group(cfg.TLS.Enabled && len(hosts) > 0, func() []core.Result { return netcheck.TLS(ctx, cfg.TLS, hosts) })
	group(cfg.Domain.Enabled && len(hosts) > 0, func() []core.Result { return netcheck.Domain(ctx, cfg.Domain, hosts) })
	group(cfg.Docker.Enabled, func() []core.Result { return hostcheck.Docker(ctx, cfg.Docker) })
	group(cfg.Disk.Enabled, func() []core.Result { return hostcheck.Disk(cfg.Disk) })
	group(len(cfg.Backups) > 0, func() []core.Result { return hostcheck.Backup(cfg.Backups, time.Now()) })
	wg.Wait()
	return all
}

// send delivers msg to every channel and reports whether at least one worked.
func send(ctx context.Context, lang string, chans []notify.Channel, msg notify.Message, silent bool) bool {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ok := false
	for _, c := range chans {
		if err := c.Send(ctx, msg); err != nil {
			fmt.Fprintln(os.Stderr, i18n.T(lang, "notify.failed", map[string]any{"channel": c.Name(), "error": err}))
			continue
		}
		ok = true
		if !silent {
			fmt.Println(i18n.T(lang, "notify.sent", map[string]any{"channel": c.Name()}))
		}
	}
	return ok
}

func usage(lang string) {
	t := func(k string) string { return i18n.T(lang, k, nil) }
	fmt.Fprintf(os.Stderr, "%s\n\n%s:\n", t("cli.usage"), t("cli.commands"))
	for _, c := range [][2]string{
		{"check", "cli.cmd_check"}, {"discover", "cli.cmd_discover"}, {"init", "cli.cmd_init"},
		{"test", "cli.cmd_test"}, {"languages", "cli.cmd_languages"}, {"version", "cli.cmd_version"},
	} {
		fmt.Fprintf(os.Stderr, "  %-10s %s\n", c[0], t(c[1]))
	}
	fmt.Fprintf(os.Stderr, "\n%s:\n", t("cli.flags"))
	for _, f := range [][2]string{
		{"--config PATH", "cli.flag_config"}, {"--lang CODE", "cli.flag_lang"}, {"--json", "cli.flag_json"},
		{"--quiet", "cli.flag_quiet"}, {"--no-notify", "cli.flag_no_notify"},
	} {
		fmt.Fprintf(os.Stderr, "  %-14s %s\n", f[0], t(f[1]))
	}
}

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
