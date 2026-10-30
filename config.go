package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type FTPConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	Pass string `json:"pass"`
	Dir  string `json:"dir"` // directory on the hosting server, e.g. /public_html
	TLS  bool   `json:"tls"` // FTPS (AUTH TLS) – recommended
}

type Config struct {
	NewsPath string    `json:"news_path"`
	PerPage  int       `json:"per_page"`
	FTP      FTPConfig `json:"ftp"`
}

func loadConfig(out string) Config {
	c := Config{NewsPath: "/aktualnosci/", PerPage: 10, FTP: FTPConfig{Port: 21, TLS: true, Dir: "/"}}
	_ = readJSON(filepath.Join(out, "config.json"), &c)
	if c.PerPage <= 0 {
		c.PerPage = 10
	}
	if c.NewsPath == "" {
		c.NewsPath = "/aktualnosci/"
	}
	if c.FTP.Port == 0 {
		c.FTP.Port = 21
	}
	return c
}

func saveConfig(out string, c Config) error {
	if !strings.HasPrefix(c.NewsPath, "/") {
		c.NewsPath = "/" + c.NewsPath
	}
	if !strings.HasSuffix(c.NewsPath, "/") {
		c.NewsPath += "/"
	}
	return writeJSON(filepath.Join(out, "config.json"), c)
}

// ---------- data store (JSON files) ----------

var storeMu sync.Mutex

func loadList(out, name string) []Post {
	var l []Post
	_ = readJSON(filepath.Join(out, "data", name), &l)
	return l
}

func saveList(out, name string, l []Post) error {
	if l == nil {
		l = []Post{}
	}
	return writeJSON(filepath.Join(out, "data", name), l)
}

func nextID(lists ...[]Post) int {
	max := 0
	for _, l := range lists {
		for _, p := range l {
			if p.ID > max {
				max = p.ID
			}
		}
	}
	return max + 1
}

var plMap = strings.NewReplacer("ą", "a", "ć", "c", "ę", "e", "ł", "l", "ń", "n", "ó", "o", "ś", "s", "ź", "z", "ż", "z")
var reNonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = reNonSlug.ReplaceAllString(plMap.Replace(strings.ToLower(s)), "-")
	if s = strings.Trim(s, "-"); s == "" {
		return "wpis"
	}
	return s
}

func uniqueSlug(slug string, lists ...[]Post) string {
	used := map[string]bool{}
	for _, l := range lists {
		for _, p := range l {
			used[p.Slug] = true
		}
	}
	s := slug
	for i := 2; used[s]; i++ {
		s = fmt.Sprintf("%s-%d", slug, i)
	}
	return s
}

// newPostPath recreates the URL structure of existing posts
// (e.g. /2024/05/slug/ or /slug/) so new posts match the old ones.
func newPostPath(posts []Post, cfg Config, slug string, date time.Time) string {
	for _, ref := range posts {
		if ref.Path == "" || ref.Path == "/" || len(ref.Date) < 7 {
			continue
		}
		segs := strings.Split(strings.Trim(ref.Path, "/"), "/")
		year, month := ref.Date[0:4], ref.Date[5:7]
		for i := 0; i < len(segs)-1; i++ {
			switch segs[i] {
			case year:
				segs[i] = date.Format("2006")
			case month:
				segs[i] = date.Format("01")
			}
		}
		segs[len(segs)-1] = slug
		return "/" + strings.Join(segs, "/") + "/"
	}
	return cfg.NewsPath + slug + "/"
}

var plMonths = []string{"stycznia", "lutego", "marca", "kwietnia", "maja", "czerwca", "lipca", "sierpnia", "września", "października", "listopada", "grudnia"}

func parseDate(s string) time.Time {
	for _, l := range []string{"2006-01-02T15:04:05", "2006-01-02"} {
		if len(s) >= len(l) {
			if t, err := time.Parse(l, s[:len(l)]); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

func plDate(s string) string {
	t := parseDate(s)
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d %s %d", t.Day(), plMonths[t.Month()-1], t.Year())
}

var reTags = regexp.MustCompile(`(?s)<[^>]*>`)

func excerptOf(htmlStr string) string {
	t := strings.Join(strings.Fields(reTags.ReplaceAllString(htmlStr, " ")), " ")
	if r := []rune(t); len(r) > 220 {
		t = string(r[:220]) + "…"
	}
	return "<p>" + t + "</p>"
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
