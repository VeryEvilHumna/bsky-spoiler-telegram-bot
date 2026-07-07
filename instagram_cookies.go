package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// IGCookieEntry mirrors a single cookie as exported by browser devtools
// (Cookie Editor / EditThisCookie extension format).
type IGCookieEntry struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly,omitempty"`
}

// igCookieJar holds Instagram session cookies in memory and persists
// Set-Cookie updates back to a JSON file so sessions stay fresh across
// restarts. Safe for concurrent use.
type igCookieJar struct {
	mu          sync.Mutex
	cookies     map[string]string // name -> value
	persistPath string
	dirty       bool
	lastSave    time.Time
}

// loadIGCookieJar reads a browser-exported cookie JSON array from path.
// If path is empty, an empty (anonymous-mode) jar is returned.
func loadIGCookieJar(path string) *igCookieJar {
	jar := &igCookieJar{
		cookies:     make(map[string]string),
		persistPath: path,
	}
	if path == "" {
		return jar
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Instagram cookies: could not read %q: %v (running unauthenticated)", path, err)
		return jar
	}
	var entries []IGCookieEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Printf("Instagram cookies: could not parse %q: %v (running unauthenticated)", path, err)
		return jar
	}
	for _, e := range entries {
		if e.Name == "" || e.Value == "" {
			continue
		}
		jar.cookies[e.Name] = e.Value
	}
	log.Printf("Instagram cookies: %d cookies loaded from %s", len(jar.cookies), path)
	return jar
}

// HasCookie reports whether any session cookie is configured.
func (j *igCookieJar) HasCookie() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.cookies) > 0
}

// CookieHeader returns a "k=v; k=v" cookie header string, or "" if empty.
func (j *igCookieJar) CookieHeader() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.cookies) == 0 {
		return ""
	}
	parts := make([]string, 0, len(j.cookies))
	for k, v := range j.cookies {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

// CSRFToken returns the csrftoken cookie value (or "" if absent).
func (j *igCookieJar) CSRFToken() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cookies["csrftoken"]
}

// Update parses Set-Cookie response headers and merges them into the jar.
// Cookies with empty values or past Expires dates are removed. The jar is
// marked dirty and persisted on a throttled basis (>= 60s since last save).
func (j *igCookieJar) Update(setCookieHeaders []string) {
	if j == nil || len(setCookieHeaders) == 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	changed := false
	for _, raw := range setCookieHeaders {
		// Each header may contain multiple comma-separated cookies;
		// we only need the first "name=value" pair per Set-Cookie.
		// Use a permissive split: find the first ';' to strip attributes.
		rest := raw
		// Strip leading whitespace
		rest = strings.TrimSpace(rest)
		// Some servers glue attributes; trim at first ';'
		if i := strings.Index(rest, ";"); i >= 0 {
			rest = rest[:i]
		}
		// Some servers send multiple cookies in one header separated by ", "
		// (this is malformed per RFC but happens in practice). To be safe,
		// we only treat up to the first '=' as the name/value.
		eq := strings.Index(rest, "=")
		if eq <= 0 {
			continue
		}
		name := strings.TrimSpace(rest[:eq])
		val := strings.TrimSpace(rest[eq+1:])
		if name == "" {
			continue
		}
		if val == "" {
			if _, ok := j.cookies[name]; ok {
				delete(j.cookies, name)
				changed = true
			}
			continue
		}
		if j.cookies[name] != val {
			j.cookies[name] = val
			changed = true
		}
	}
	if !changed || j.persistPath == "" {
		return
	}
	j.dirty = true
	if time.Since(j.lastSave) >= 60*time.Second {
		j.saveLocked()
	}
}

// Save flushes the jar to disk immediately (no-op if no persistPath or not dirty).
func (j *igCookieJar) Save() {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.persistPath == "" || !j.dirty {
		return
	}
	j.saveLocked()
}

func (j *igCookieJar) saveLocked() {
	entries := make([]IGCookieEntry, 0, len(j.cookies))
	for k, v := range j.cookies {
		entries = append(entries, IGCookieEntry{
			Name:     k,
			Value:    v,
			Domain:   ".instagram.com",
			Path:     "/",
			Secure:   true,
			HTTPOnly: true,
		})
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		log.Printf("Instagram cookies: marshal failed: %v", err)
		return
	}
	if err := os.WriteFile(j.persistPath, data, 0o600); err != nil {
		log.Printf("Instagram cookies: write %s failed: %v", j.persistPath, err)
		return
	}
	j.dirty = false
	j.lastSave = time.Now()
}

// igUpdateJarFromResponse is a convenience helper that pulls Set-Cookie
// headers from an *http.Response and feeds them to the jar.
func igUpdateJarFromResponse(resp *http.Response) {
	if igCookies == nil || resp == nil {
		return
	}
	igCookies.Update(resp.Header.Values("Set-Cookie"))
}

// igCookies is the package-level cookie jar, initialized in main().
var igCookies *igCookieJar

// helper to format an error when a cookie-authenticated request still fails
// with 401/403 — indicates the stored cookie is likely expired.
func igCookieExpiredHint(usedCookie bool) string {
	if !usedCookie {
		return ""
	}
	return " Instagram session cookie may be expired — please refresh INSTAGRAM_COOKIES_FILE."
}

