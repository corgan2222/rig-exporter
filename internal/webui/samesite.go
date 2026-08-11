//go:build windows

package webui

import (
	"net/http"
	neturl "net/url"
	"strings"
)

// Only this page may change anything here.
//
// A form post is a CORS simple request: no preflight, no permission asked, and
// the answer being unreadable to the sender does not stop the request from
// arriving. So without this check a web page somebody visits can submit a form
// to this interface, and every setting on it is one post away — including the
// ones that decide where the stored broker password and InfluxDB token get
// sent. That holds on plain loopback; web_bind_all only widens who else can try.
//
// The check is the browser's own word for where the request came from rather
// than a token in every form. It costs nothing, needs no session, and cannot
// fall out of sync with a page that was left open. What it cannot do is stop a
// client that simply does not send the header — curl on the local network is
// not a browser and answers to a different guard, which is why the secrets are
// dropped when their target moves.
func sameSite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !changesSomething(r) || fromThisPage(r) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "cross-site request", http.StatusForbidden)
	})
}

// changesSomething is true for the methods that are not just reading. GET and
// HEAD are left alone: a bookmark, a link and a refresh all arrive as one of
// those, and refusing them would break opening the page at all.
func changesSomething(r *http.Request) bool {
	return r.Method != http.MethodGet && r.Method != http.MethodHead
}

// fromThisPage decides whether a request came from the interface itself.
func fromThisPage(r *http.Request) bool {
	// What the browser says. same-origin is this page's own form or fetch;
	// none is somebody typing the address or following a bookmark. Every
	// current browser sends this, and it is the one thing a foreign page
	// cannot fake.
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
		// A client too old to say, or not a browser at all. Then Origin
		// decides — and its absence is not evidence of anything, so it is not
		// treated as such: curl sends neither header and is refused nothing
		// here. It is refused elsewhere, by not being handed a secret.
		origin := r.Header.Get("Origin")
		return origin == "" || sameHost(origin, r.Host)
	default:
		// cross-site and same-site. same-site is refused on purpose: a
		// different port on this machine is a different application.
		return false
	}
}

// sameHost reports whether an Origin header names this listener.
func sameHost(origin, host string) bool {
	parsed, err := neturl.Parse(origin)
	if err != nil || parsed.Host == "" {
		// Including the literal "null" a sandboxed frame sends, which parses
		// without error and names nothing.
		return false
	}
	return strings.EqualFold(parsed.Host, host)
}
