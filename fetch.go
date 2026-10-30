package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// sink receives progress messages (console or the panel window).
var (
	sink   = func(s string) { fmt.Println(s) }
	sinkMu sync.Mutex
)

func sayTr(key string, a ...any) { sink(strings.TrimRight(ltr(key, a...), "\n")) }

var client = &http.Client{Timeout: 40 * time.Second}

func get(u string) (*http.Response, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "wp2static/0.1 (narzędzie do migracji)")
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode < 500 {
			return resp, nil
		}
		if err == nil {
			resp.Body.Close()
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		lastErr = err
		time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
	}
	return nil, lastErr
}

func getBody(u string) ([]byte, http.Header, int, error) {
	resp, err := get(u)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return b, resp.Header, resp.StatusCode, err
}

func download(src, dst string) error {
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		return nil // resume: file already exists
	}
	resp, err := get(src)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func normalizeBase(site string) (*url.URL, error) {
	site = strings.TrimSpace(site)
	if !strings.HasPrefix(site, "http://") && !strings.HasPrefix(site, "https://") {
		site = "https://" + site
	}
	u, err := url.Parse(site)
	if err != nil || u.Host == "" {
		return nil, errors.New(Tf(currentLogLang(), "badAddress", site))
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

func writeJSON(p string, v any) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

func readJSON(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// relativize turns https://domain.example/xyz into /xyz (internal links).
func relativize(s string, base *url.URL) string {
	host := strings.TrimPrefix(base.Hostname(), "www.")
	re := regexp.MustCompile(`https?://(?:www\.)?` + regexp.QuoteMeta(host) + `(?::\d+)?`)
	var sb strings.Builder
	last := 0
	for _, loc := range re.FindAllStringIndex(s, -1) {
		next := byte('"')
		if loc[1] < len(s) {
			next = s[loc[1]]
		}
		if isHostChar(next) { // e.g. example.com.au – not our domain
			continue
		}
		sb.WriteString(s[last:loc[0]])
		if next != '/' {
			sb.WriteString("/")
		}
		last = loc[1]
	}
	sb.WriteString(s[last:])
	return sb.String()
}

func isHostChar(c byte) bool {
	return c == '.' || c == '-' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// ---------- Mirror: download assets preserving their paths ----------

var assetExts = map[string]bool{
	".css": true, ".js": true, ".mjs": true, ".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".svg": true, ".webp": true, ".avif": true, ".ico": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".mp4": true, ".webm": true, ".mp3": true, ".pdf": true, ".zip": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true, ".odt": true,
}

var (
	reCSSURL    = regexp.MustCompile(`url\(\s*['"]?([^'")]+?)['"]?\s*\)`)
	reCSSImport = regexp.MustCompile(`@import\s+['"]([^'"]+)['"]`)
	// map of webpack chunk id->hash, e.g. {662:"bc5035e...",783:"ab728..."}.
	reJSChunkMap = regexp.MustCompile(`\b(\d+):"([0-9a-f]{8,32})"`)
)

type Mirror struct {
	Base   *url.URL
	Root   string // katalog docelowy (webroot statycznej strony)
	mu     sync.Mutex
	seen   map[string]bool
	Count  int
	Failed []string
}

func NewMirror(base *url.URL, root string) *Mirror {
	return &Mirror{Base: base, Root: root, seen: map[string]bool{}}
}

func (m *Mirror) sameSite(u *url.URL) bool {
	return strings.TrimPrefix(u.Hostname(), "www.") == strings.TrimPrefix(m.Base.Hostname(), "www.")
}

func (m *Mirror) Get(raw string) {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" || strings.HasPrefix(raw, "data:") {
		return
	}
	u, err := m.Base.Parse(raw)
	if err != nil {
		return
	}
	m.getURL(u)
}

func (m *Mirror) getURL(u *url.URL) {
	if (u.Scheme != "http" && u.Scheme != "https") || !m.sameSite(u) {
		return
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if !assetExts[ext] {
		return
	}
	m.mu.Lock()
	if m.seen[u.Path] {
		m.mu.Unlock()
		return
	}
	m.seen[u.Path] = true
	m.mu.Unlock()

	clean := path.Clean("/" + u.Path)
	dst := filepath.Join(m.Root, filepath.FromSlash(clean))
	if err := download(u.String(), dst); err != nil {
		m.mu.Lock()
		m.Failed = append(m.Failed, fmt.Sprintf("%s (%v)", u.Path, err))
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	m.Count++
	m.mu.Unlock()

	if ext == ".css" { // CSS resources: fonts, backgrounds, @import
		data, err := os.ReadFile(dst)
		if err != nil {
			return
		}
		for _, ref := range extractCSSRefs(string(data)) {
			ref = strings.TrimSpace(ref)
			if ref == "" || strings.HasPrefix(ref, "data:") {
				continue
			}
			if r, err := u.Parse(ref); err == nil {
				m.getURL(r)
			}
		}
	}
	if ext == ".js" { // lazily loaded webpack chunks
		data, err := os.ReadFile(dst)
		if err != nil {
			return
		}
		for _, ref := range extractJSChunkRefs(string(data)) {
			if r, err := u.Parse(ref); err == nil { // same directory as the source file
				m.getURL(r)
			}
		}
	}
}

func extractCSSRefs(css string) []string {
	var out []string
	for _, m := range reCSSURL.FindAllStringSubmatch(css, -1) {
		out = append(out, m[1])
	}
	for _, m := range reCSSImport.FindAllStringSubmatch(css, -1) {
		out = append(out, m[1])
	}
	return out
}

// extractJSChunkRefs recreates "{id}.{hash}.js" file names from the id->hash
// map that webpack embeds in the main file to know where to fetch a chunk that
// is loaded dynamically (code splitting) – otherwise those files never reach
// the mirror, because they do not appear as plain <script src="...">.
func extractJSChunkRefs(js string) []string {
	var out []string
	for _, m := range reJSChunkMap.FindAllStringSubmatch(js, -1) {
		out = append(out, m[1]+"."+m[2]+".js")
	}
	return out
}
