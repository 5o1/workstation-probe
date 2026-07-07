// Package cors contains the origin-pattern grammar shared between the
// runtime middleware and the startup-time config validator. Both layers
// must agree on what a valid allowed_origin looks like, otherwise a config
// that passes validation can still be rejected (or worse, silently
// mis-parsed) at request time.
package cors

import (
	"fmt"
	"strings"
)

// Pattern is one parsed allowed_origin. It supports the grammar:
//
//	scheme://host         — exact host match
//	scheme://*.host       — wildcard subdomain match
//
// scheme must be http or https. userinfo, mid-segment wildcards, bare "*",
// and top-level-domain wildcards are all rejected.
type Pattern struct {
	raw      string
	scheme   string
	host     string
	wildcard bool // leading "*." wildcard in host
}

// Raw returns the original input string. Used for error messages and the
// Access-Control-Allow-Origin echo.
func (p Pattern) Raw() string { return p.raw }

// Parse validates s against the allowed_origin grammar. On success the
// returned Pattern is safe to use in Match.
func Parse(s string) (Pattern, error) {
	if s == "" {
		return Pattern{}, fmt.Errorf("empty origin")
	}
	if s == "*" {
		return Pattern{}, fmt.Errorf("bare '*' is not allowed")
	}

	// split scheme://rest
	colon := strings.IndexByte(s, ':')
	if colon <= 0 {
		return Pattern{}, fmt.Errorf("missing scheme")
	}
	scheme := s[:colon]
	if scheme != "http" && scheme != "https" {
		return Pattern{}, fmt.Errorf("scheme must be http or https, got %q", scheme)
	}
	rest := s[colon+1:]
	if len(rest) < 3 || rest[0] != '/' || rest[1] != '/' {
		return Pattern{}, fmt.Errorf("origin must start with scheme://")
	}
	rest = rest[2:]
	if rest == "" {
		return Pattern{}, fmt.Errorf("missing host")
	}

	// reject userinfo — it has no legitimate use in an Origin header
	if strings.ContainsRune(rest, '@') {
		return Pattern{}, fmt.Errorf("userinfo is not allowed")
	}

	host := rest
	wildcard := false
	if len(host) >= 2 && host[0] == '*' && host[1] == '.' {
		wildcard = true
		host = host[2:]
		if host == "" {
			return Pattern{}, fmt.Errorf("wildcard host is empty")
		}
		// reject top-level-domain wildcard like "*.com"
		if !strings.Contains(host, ".") {
			return Pattern{}, fmt.Errorf("wildcard host must contain a dot")
		}
		// reject mid-segment wildcards: after stripping the leading "*.",
		// the remainder must contain no further '*'
		if strings.ContainsRune(host, '*') {
			return Pattern{}, fmt.Errorf("only one leading '*.' wildcard is allowed")
		}
	} else if strings.ContainsRune(host, '*') {
		return Pattern{}, fmt.Errorf("'*' must be the leading segment of the host")
	}
	if host == "" {
		return Pattern{}, fmt.Errorf("empty host")
	}
	return Pattern{raw: s, scheme: scheme, host: host, wildcard: wildcard}, nil
}

// Match reports whether origin is allowed by this pattern. The input is
// the value of the request's Origin header (or "" for same-origin /
// non-browser callers, which never matches a configured pattern).
//
// A wildcard pattern matches <sub>.<host> where <sub> is a single non-empty
// label with no '*' of its own. A literal pattern matches host exactly.
// Both scheme and host must match.
func (p Pattern) Match(origin string) bool {
	if origin == "" {
		return false
	}
	colon := strings.IndexByte(origin, ':')
	if colon <= 0 {
		return false
	}
	if origin[:colon] != p.scheme {
		return false
	}
	rest := origin[colon+1:]
	if len(rest) < 2 || rest[0] != '/' || rest[1] != '/' {
		return false
	}
	host := rest[2:]
	if strings.ContainsRune(host, '@') {
		return false
	}
	if p.wildcard {
		// match host as <sub>.<p.host>; the subdomain label itself must
		// not contain another '*' (otherwise an attacker can replay the
		// literal "*" label and reuse the resulting Allow-Origin header
		// from a non-browser client).
		idx := strings.IndexByte(host, '.')
		if idx < 0 {
			return false
		}
		sub := host[:idx]
		rest := host[idx+1:]
		if sub == "" || strings.ContainsRune(sub, '*') || rest != p.host {
			return false
		}
		return true
	}
	return host == p.host
}
