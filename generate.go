package main

import (
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Site struct {
	Out, Base, Name string
	Cfg             Config
	Posts, Pages    []Post
	Layout          string
	Home            string
	SinglePost      string
	SinglePage      string
	Classes         map[string]string
	Attrs           map[string]string
	FrontID         int
}

var (
	reLeft   = regexp.MustCompile(`<!--WP2S:[A-Z_]+-->`)
	reFrontP = regexp.MustCompile(`page-id-(\d+)`)
)

func loadSite(out string) (*Site, error) {
	s := &Site{Out: out, Cfg: loadConfig(out), Classes: map[string]string{}, Attrs: map[string]string{}}
	meta := map[string]string{}
	if err := readJSON(filepath.Join(out, "data", "site.json"), &meta); err != nil {
		return nil, errors.New(ltr("noSiteJSON"))
	}
	s.Base, s.Name = strings.TrimRight(meta["url"], "/"), meta["name"]
	s.Posts, s.Pages = loadList(out, "posts.json"), loadList(out, "pages.json")
	rd := func(n string) string { b, _ := os.ReadFile(filepath.Join(out, "template", n)); return string(b) }
	if s.Layout = rd("layout.html"); s.Layout == "" {
		return nil, errors.New(ltr("noLayout"))
	}
	s.SinglePost, s.SinglePage = rd("single-post.html"), rd("single-page.html")
	if s.SinglePost == "" {
		s.SinglePost = wrap(s.Layout, `<article class="wp2s-entry"><h1><!--WP2S:TITLE--></h1><time datetime="<!--WP2S:DATE_ISO-->"><!--WP2S:DATE_TEXT--></time><div class="entry-content"><!--WP2S:CONTENT--></div></article>`)
	}
	if s.SinglePage == "" {
		s.SinglePage = wrap(s.Layout, `<article class="wp2s-entry"><h1><!--WP2S:TITLE--></h1><div class="entry-content"><!--WP2S:CONTENT--></div></article>`)
	}
	if s.Home = rd("home.html"); s.Home == "" {
		s.Home = s.Layout
	}
	_ = readJSON(filepath.Join(out, "template", "bodyclasses.json"), &s.Classes)
	_ = readJSON(filepath.Join(out, "template", "bodyattrs.json"), &s.Attrs)
	if m := reFrontP.FindStringSubmatch(s.Classes["home"]); m != nil {
		fmt.Sscan(m[1], &s.FrontID)
	}
	return s, nil
}

func wrap(layout, inner string) string {
	return strings.Replace(layout, "<!--WP2S:CONTENT-->", inner, 1)
}

func (s *Site) fill(tpl string, v map[string]string) string {
	content := v["CONTENT"]
	delete(v, "CONTENT")
	for k, val := range v {
		tpl = strings.ReplaceAll(tpl, "<!--WP2S:"+k+"-->", val)
	}
	tpl = reLeft.ReplaceAllStringFunc(tpl, func(m string) string {
		if m == "<!--WP2S:CONTENT-->" {
			return m
		}
		return ""
	})
	return strings.Replace(tpl, "<!--WP2S:CONTENT-->", content, 1) // content is placed last – it is no longer processed
}

func (s *Site) docTitle(title string) string {
	if s.Name == "" || title == "" {
		return title + s.Name
	}
	return title + " – " + html.EscapeString(s.Name)
}

func (s *Site) bodyClass(kind string, id int) string {
	return strings.ReplaceAll(s.Classes[kind], "{ID}", fmt.Sprint(id))
}

func (s *Site) render(tpl string, p Post, kind string) string {
	return s.fill(tpl, map[string]string{
		"TITLE": p.Title.Rendered, "DOCTITLE": s.docTitle(p.Title.Rendered),
		"CONTENT": p.ContentLocal, "DATE_ISO": p.Date, "DATE_TEXT": plDate(p.Date),
		"URL": p.Path, "ID": fmt.Sprint(p.ID), "BODYCLASS": s.bodyClass(kind, p.ID),
		"BODYATTRS": s.Attrs[kind],
	})
}

func (s *Site) renderPlain(tpl, title, content, bodyClass, attrsKind string) string {
	return s.fill(tpl, map[string]string{
		"TITLE": title, "DOCTITLE": s.docTitle(title), "CONTENT": content,
		"BODYCLASS": bodyClass, "BODYATTRS": s.Attrs[attrsKind],
	})
}

func (s *Site) published() []Post {
	var l []Post
	for _, p := range s.Posts {
		if p.Status == "" || p.Status == "publish" {
			l = append(l, p)
		}
	}
	sort.SliceStable(l, func(i, j int) bool { return l[i].Date > l[j].Date })
	return l
}

func (s *Site) newsURL(n int) string {
	if n <= 1 {
		return s.Cfg.NewsPath
	}
	return fmt.Sprintf("%sstrona/%d/", s.Cfg.NewsPath, n)
}

func (s *Site) newsList(items []Post, n, total int) string {
	var b strings.Builder
	b.WriteString(`<div class="wp2s-news">`)
	if s.Home == s.Layout { // no dedicated home banner – show the title here
		b.WriteString(`<h1>Aktualności</h1>`)
	}
	for _, p := range items {
		fmt.Fprintf(&b, `<article class="wp2s-item"><h2><a href="%s">%s</a></h2><time datetime="%s">%s</time><div class="wp2s-excerpt">%s</div></article>`,
			p.Path, p.Title.Rendered, p.Date, plDate(p.Date), p.Excerpt.Rendered)
	}
	if total > 1 {
		b.WriteString(`<nav class="wp2s-pager">`)
		if n > 1 {
			fmt.Fprintf(&b, `<a href="%s">← Nowsze</a> `, s.newsURL(n-1))
		}
		fmt.Fprintf(&b, `<span>%d / %d</span>`, n, total)
		if n < total {
			fmt.Fprintf(&b, ` <a href="%s">Starsze →</a>`, s.newsURL(n+1))
		}
		b.WriteString(`</nav>`)
	}
	return b.String() + `</div>`
}

func writePage(dist, urlPath, content string) error {
	var dst string
	if strings.HasSuffix(urlPath, "/") {
		dst = filepath.Join(dist, filepath.FromSlash(urlPath), "index.html")
	} else {
		dst = filepath.Join(dist, filepath.FromSlash(urlPath))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(content), 0o644)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if strings.HasSuffix(p, ".part") {
			return nil
		}
		if os.Link(p, target) == nil { // hard link = no copying of gigabytes
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

const htaccess = `DirectoryIndex index.html
ErrorDocument 404 /404.html
Options -Indexes
<IfModule mod_expires.c>
  ExpiresActive On
  ExpiresByType image/jpeg "access plus 1 year"
  ExpiresByType image/png "access plus 1 year"
  ExpiresByType image/webp "access plus 1 year"
  ExpiresByType text/css "access plus 1 month"
  ExpiresByType application/javascript "access plus 1 month"
</IfModule>
`

func buildSite(out string, logf func(string, ...any)) error {
	s, err := loadSite(out)
	if err != nil {
		return err
	}
	start := time.Now()
	dist := filepath.Join(out, "dist")
	if err := os.RemoveAll(dist); err != nil {
		return err
	}
	if err := copyTree(filepath.Join(out, "site"), dist); err != nil {
		return err
	}
	posts := s.published()
	count := 0
	emit := func(path, content string) error { count++; return writePage(dist, path, content) }

	for _, p := range posts {
		if err := emit(p.Path, s.render(s.SinglePost, p, "post")); err != nil {
			return err
		}
	}
	var front *Post
	for i := range s.Pages {
		p := s.Pages[i]
		if p.Status != "" && p.Status != "publish" {
			continue
		}
		if err := emit(p.Path, s.render(s.SinglePage, p, "page")); err != nil {
			return err
		}
		if p.ID == s.FrontID {
			front = &s.Pages[i]
		}
	}
	// news list
	per := s.Cfg.PerPage
	total := (len(posts) + per - 1) / per
	if total == 0 {
		total = 1
	}
	for n := 1; n <= total; n++ {
		lo, hi := (n-1)*per, n*per
		if hi > len(posts) {
			hi = len(posts)
		}
		var items []Post
		if lo < hi {
			items = posts[lo:hi]
		}
		page := s.renderPlain(s.Home, "Aktualności", s.newsList(items, n, total), "blog", "home")
		if err := emit(s.newsURL(n), page); err != nil {
			return err
		}
		if n == 1 && front == nil { // home page = post list
			if err := emit("/", page); err != nil {
				return err
			}
		}
	}
	if front != nil {
		if err := emit("/", s.render(s.SinglePage, *front, "home")); err != nil {
			return err
		}
	}
	notFound := s.renderPlain(s.Layout, "Nie znaleziono strony", `<h1>Nie znaleziono strony</h1><p><a href="/">Wróć na stronę główną</a></p>`, "error404", "")
	extra := map[string]string{
		"/404.html": notFound, "/.htaccess": htaccess,
		"/robots.txt":  "User-agent: *\nAllow: /\nSitemap: " + s.Base + "/sitemap.xml\n",
		"/sitemap.xml": s.sitemap(posts), "/feed.xml": s.feed(posts),
	}
	for p, c := range extra {
		if err := emit(p, c); err != nil {
			return err
		}
	}
	logf(ltr("built", count, len(posts), len(s.Pages), time.Since(start).Round(time.Millisecond), dist))
	return nil
}

func xmlEsc(s string) string { return html.EscapeString(s) }

func (s *Site) sitemap(posts []Post) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	add := func(path, date string) {
		fmt.Fprintf(&b, "<url><loc>%s%s</loc>", s.Base, xmlEsc(path))
		if len(date) >= 10 {
			fmt.Fprintf(&b, "<lastmod>%s</lastmod>", date[:10])
		}
		b.WriteString("</url>")
	}
	add("/", "")
	add(s.Cfg.NewsPath, "")
	for _, p := range s.Pages {
		if p.Path != "/" {
			add(p.Path, p.Modified)
		}
	}
	for _, p := range posts {
		add(p.Path, p.Modified+p.Date)
	}
	return b.String() + "</urlset>"
}

func (s *Site) feed(posts []Post) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>%s</title><link>%s/</link><description>Aktualności</description>`, xmlEsc(s.Name), s.Base)
	for i, p := range posts {
		if i >= 20 {
			break
		}
		fmt.Fprintf(&b, `<item><title>%s</title><link>%s%s</link><guid>%s%s</guid><pubDate>%s</pubDate><description>%s</description></item>`,
			xmlEsc(html.UnescapeString(p.Title.Rendered)), s.Base, xmlEsc(p.Path), s.Base, xmlEsc(p.Path),
			parseDate(p.Date).Format(time.RFC1123Z), xmlEsc(p.Excerpt.Rendered))
	}
	return b.String() + `</channel></rss>`
}

// ---------- preview ----------

func previewHandler(distFn func() string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dist := distFn()
		if dist == "" {
			http.Error(w, "Open a project in the panel first / Najpierw otwórz projekt w panelu.", 404)
			return
		}
		p := filepath.Join(dist, filepath.FromSlash(strings.TrimPrefix(pathClean(r.URL.Path), "/")))
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			p = filepath.Join(p, "index.html")
		}
		if !fileExists(p) {
			b, _ := os.ReadFile(filepath.Join(dist, "404.html"))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(404)
			w.Write(b)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, p)
	})
}

func pathClean(p string) string {
	return filepath.ToSlash(filepath.Clean("/" + p))
}

func runPreview(out, addr string) error {
	fmt.Println("Podgląd: http://" + addr + "  (Ctrl+C kończy)")
	return http.ListenAndServe(addr, previewHandler(func() string { return filepath.Join(out, "dist") }))
}
