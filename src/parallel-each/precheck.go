package main

import (
	"net/url"
	"strconv"
	"strings"
)

// urlLooksValid is a static URL sanity check applied to live-add inputs
// when --input-type=url. It rejects obvious paste mistakes (no scheme,
// pseudo-URL like "missav.aijavascript:;", typos like "quit") without
// touching the network.
//
// Network reachability is intentionally NOT checked: many real sites
// (Cloudflare-fronted, geo-blocked, login-walled) return 403/503 to
// curl-style probes despite being perfectly usable from a real browser.
// A network pre-check would produce false rejections and is the wrong
// boundary — the actual fetcher (e.g. Playwright) is the source of
// truth for reachability.
func urlLooksValid(s string) (bool, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, "empty input"
	}
	u, err := url.Parse(s)
	if err != nil {
		return false, "parse error: " + err.Error()
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false, "scheme must be http or https (got " + strconv.Quote(u.Scheme) + ")"
	}
	if u.Host == "" {
		return false, "missing host"
	}
	if !strings.Contains(u.Host, ".") {
		return false, "host has no dot: " + u.Host
	}
	return true, ""
}
