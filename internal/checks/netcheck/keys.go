package netcheck

// Message keys used by this package; every one exists in
// internal/i18n/locales/en/netcheck.json.
const (
	keyHTTPOK          = "http.ok"           // code, ms
	keyHTTPSlow        = "http.slow"         // code, ms, limit
	keyHTTPBadStatus   = "http.bad_status"   // code, ms, max
	keyHTTPMissingText = "http.missing_text" // code, text
	keyHTTPTimeout     = "http.timeout"      // timeout
	keyHTTPError       = "http.error"        // error

	keyTLSOK               = "tls.ok"                // days, date, issuer
	keyTLSExpiresSoon      = "tls.expires_soon"      // days, date, issuer
	keyTLSExpiresCritical  = "tls.expires_critical"  // days, date, issuer
	keyTLSExpired          = "tls.expired"           // days, date
	keyTLSUntrusted        = "tls.untrusted"         // error
	keyTLSHostnameMismatch = "tls.hostname_mismatch" // error
	keyTLSHandshakeFailed  = "tls.handshake_failed"  // error

	keyDomainOK              = "domain.ok"               // days, date
	keyDomainExpiresSoon     = "domain.expires_soon"     // days, date
	keyDomainExpiresCritical = "domain.expires_critical" // days, date
	keyDomainExpired         = "domain.expired"          // days, date
	keyDomainLookupFailed    = "domain.lookup_failed"    // error
	keyDomainNoExpiry        = "domain.no_expiry"
)

var allKeys = []string{
	keyHTTPOK, keyHTTPSlow, keyHTTPBadStatus, keyHTTPMissingText, keyHTTPTimeout, keyHTTPError,
	keyTLSOK, keyTLSExpiresSoon, keyTLSExpiresCritical, keyTLSExpired, keyTLSUntrusted,
	keyTLSHostnameMismatch, keyTLSHandshakeFailed,
	keyDomainOK, keyDomainExpiresSoon, keyDomainExpiresCritical, keyDomainExpired,
	keyDomainLookupFailed, keyDomainNoExpiry,
}
