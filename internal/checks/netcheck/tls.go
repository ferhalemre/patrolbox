package netcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

// rootCAs overrides the system roots (tests only); nil means system roots.
var rootCAs *x509.CertPool

// TLS checks the certificate served by every host.
func TLS(ctx context.Context, cfg config.TLS, hosts []string) []core.Result {
	return forEach(uniqueHosts(hosts), func(h string) []core.Result {
		return []core.Result{tlsOne(ctx, cfg, h)}
	})
}

func tlsOne(ctx context.Context, cfg config.TLS, target string) core.Result {
	res := core.Result{Check: "tls", Target: target}
	start := time.Now()
	fail := func(status core.Status, key string, args map[string]any) core.Result {
		res.Took = time.Since(start)
		res.Status, res.MsgKey, res.Args = status, key, args
		return res
	}

	host, port := splitHost(target)
	if port == "" {
		port = "443"
	}
	ctx, cancel := context.WithTimeout(ctx, timeoutOr(cfg.Timeout.D(), 10*time.Second))
	defer cancel()

	raw, err := dialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fail(core.Crit, keyTLSHandshakeFailed, map[string]any{"error": shortErr(err)})
	}
	defer raw.Close()
	conn := tls.Client(raw, &tls.Config{ServerName: host, InsecureSkipVerify: true})
	if err := conn.HandshakeContext(ctx); err != nil {
		return fail(core.Crit, keyTLSHandshakeFailed, map[string]any{"error": shortErr(err)})
	}
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return fail(core.Crit, keyTLSHandshakeFailed, map[string]any{"error": "no certificate presented"})
	}
	leaf := certs[0]
	days := daysUntil(leaf.NotAfter)
	date := leaf.NotAfter.UTC().Format("2006-01-02")
	issuer := issuerName(leaf)
	args := map[string]any{"days": days, "date": date, "issuer": issuer}

	inter := x509.NewCertPool()
	for _, c := range certs[1:] {
		inter.AddCert(c)
	}
	_, verr := leaf.Verify(x509.VerifyOptions{DNSName: host, Roots: rootCAs, Intermediates: inter})
	if verr != nil {
		var he x509.HostnameError
		var ci x509.CertificateInvalidError
		switch {
		case errors.As(verr, &he):
			return fail(core.Crit, keyTLSHostnameMismatch, map[string]any{"error": verr.Error(), "days": days, "date": date, "issuer": issuer})
		case errors.As(verr, &ci) && ci.Reason == x509.Expired:
			args["days"] = -days
			return fail(core.Crit, keyTLSExpired, args)
		default:
			return fail(core.Crit, keyTLSUntrusted, map[string]any{"error": verr.Error(), "days": days, "date": date, "issuer": issuer})
		}
	}
	switch {
	case days < 0:
		args["days"] = -days
		return fail(core.Crit, keyTLSExpired, args)
	case days <= cfg.CritDays:
		return fail(core.Crit, keyTLSExpiresCritical, args)
	case days <= cfg.WarnDays:
		return fail(core.Warn, keyTLSExpiresSoon, args)
	}
	return fail(core.OK, keyTLSOK, args)
}

func issuerName(c *x509.Certificate) string {
	if c.Issuer.CommonName != "" {
		return c.Issuer.CommonName
	}
	if len(c.Issuer.Organization) > 0 {
		return c.Issuer.Organization[0]
	}
	return "unknown"
}
