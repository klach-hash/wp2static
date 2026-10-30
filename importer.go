package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"
)

type Rendered struct {
	Rendered string `json:"rendered"`
}

type Post struct {
	ID            int      `json:"id"`
	Date          string   `json:"date"`
	Modified      string   `json:"modified"`
	Slug          string   `json:"slug"`
	Status        string   `json:"status"`
	Link          string   `json:"link"`
	Path          string   `json:"path"` // URL path, preserved in the static version (SEO)
	Title         Rendered `json:"title"`
	Content       Rendered `json:"content"`
	Excerpt       Rendered `json:"excerpt"`
	ContentLocal  string   `json:"content_local"` // content with relative links
	Author        int      `json:"author"`
	FeaturedMedia int      `json:"featured_media"`
	Categories    []int    `json:"categories,omitempty"`
	Tags          []int    `json:"tags,omitempty"`
	Parent        int      `json:"parent,omitempty"`
}

type Term struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Link   string `json:"link"`
	Count  int    `json:"count"`
	Parent int    `json:"parent"`
}

type Media struct {
	ID           int             `json:"id"`
	Slug         string          `json:"slug"`
	SourceURL    string          `json:"source_url"`
	AltText      string          `json:"alt_text"`
	Title        Rendered        `json:"title"`
	MediaDetails json.RawMessage `json:"media_details"`
}

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// ---------- REST API ----------

type API struct {
	base   string
	pretty bool // /wp-json/... vs ?rest_route=...
}

func (a *API) url(ep, params string) string {
	if a.pretty {
		return fmt.Sprintf("%s/wp-json/wp/v2/%s?%s", a.base, ep, params)
	}
	return fmt.Sprintf("%s/?rest_route=/wp/v2/%s&%s", a.base, ep, params)
}

func (a *API) probe() bool {
	body, _, status, err := getBody(a.url("posts", "per_page=1"))
	return err == nil && status == 200 && len(body) > 0 && body[0] == '['
}

func detectAPI(base string) (*API, error) {
	a := &API{base: base, pretty: true}
	if a.probe() {
		return a, nil
	}
	a.pretty = false
	if a.probe() {
		return a, nil
	}
	return nil, errors.New(Tf(currentLogLang(), "apiUnavailable", base))
}

func fetchAll[T any](a *API, ep string) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		body, hdr, status, err := getBody(a.url(ep, fmt.Sprintf("per_page=100&page=%d", page)))
		if err != nil {
			return nil, err
		}
		if status == 400 && page > 1 { // page out of range
			break
		}
		if status != 200 {
			return nil, fmt.Errorf("%s: HTTP %d", ep, status)
		}
		var items []T
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, fmt.Errorf("%s: %w", Tf(currentLogLang(), "badResponse", ep), err)
		}
		all = append(all, items...)
		total, _ := strconv.Atoi(hdr.Get("X-WP-TotalPages"))
		if len(items) == 0 || (total > 0 && page >= total) {
			break
		}
	}
	return all, nil
}

var reUploads = regexp.MustCompile(`(?:https?://[^\s"'<>,)]+)?/wp-content/uploads/[^\s"'<>,)]+`)

func urlPath(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Path == "" {
		return "/"
	}
	return u.Path
}

// ---------- Step 1: import ----------

func runImport(site, out string) error {
	base, err := normalizeBase(site)
	if err != nil {
		return err
	}
	sayTr("checkApi", base)
	api, err := detectAPI(base.String())
	if err != nil {
		return err
	}

	posts, err := fetchAll[Post](api, "posts")
	if err != nil {
		return err
	}
	sayTr("postsCount", len(posts))
	pages, err := fetchAll[Post](api, "pages")
	if err != nil {
		return err
	}
	sayTr("pagesCount", len(pages))

	cats, e1 := fetchAll[Term](api, "categories")
	tags, e2 := fetchAll[Term](api, "tags")
	users, e3 := fetchAll[User](api, "users")
	media, e4 := fetchAll[Media](api, "media")
	for _, x := range []struct {
		key string
		err error
	}{{"catCategories", e1}, {"catTags", e2}, {"catAuthors", e3}, {"catMedia", e4}} {
		if x.err != nil {
			sayTr("skipped", T(currentLogLang(), x.key), x.err)
		}
	}
	sayTr("taxCounts", len(cats), len(tags), len(media))

	// relative links + paths + list of files to download
	files := map[string]bool{}
	process := func(list []Post) {
		for i := range list {
			p := &list[i]
			p.Path = urlPath(p.Link)
			p.ContentLocal = relativize(p.Content.Rendered, base)
			for _, s := range []string{p.Content.Rendered, p.Excerpt.Rendered} {
				for _, f := range reUploads.FindAllString(s, -1) {
					files[f] = true
				}
			}
		}
	}
	process(posts)
	process(pages)
	for _, m := range media {
		if m.SourceURL != "" {
			files[m.SourceURL] = true
		}
		var d struct {
			Sizes map[string]struct {
				SourceURL string `json:"source_url"`
			} `json:"sizes"`
		}
		if json.Unmarshal(m.MediaDetails, &d) == nil { // sometimes [] instead of {}
			for _, s := range d.Sizes {
				files[s.SourceURL] = true
			}
		}
	}

	dataDir := filepath.Join(out, "data")
	for name, v := range map[string]any{
		"posts.json": posts, "pages.json": pages, "categories.json": cats,
		"tags.json": tags, "users.json": users, "media.json": media,
		"site.json": map[string]string{"url": base.String(), "name": siteName(api), "imported_at": time.Now().Format(time.RFC3339)},
	} {
		if err := writeJSON(filepath.Join(dataDir, name), v); err != nil {
			return err
		}
	}

	// download media (6 in parallel, paths as in WordPress)
	list := make([]string, 0, len(files))
	for f := range files {
		list = append(list, f)
	}
	sort.Strings(list)
	sayTr("downloading", len(list))
	m := NewMirror(base, filepath.Join(out, "site"))
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				m.Get(f)
			}
		}()
	}
	for _, f := range list {
		jobs <- f
	}
	close(jobs)
	wg.Wait()

	sayTr("importDone", m.Count, len(m.Failed), dataDir, filepath.Join(out, "site", "wp-content", "uploads"))
	for _, f := range m.Failed {
		sayTr("notDownloaded", f)
	}
	return nil
}

// siteName fetches the site name from the REST API root (best effort).
func siteName(a *API) string {
	u := a.base + "/wp-json/"
	if !a.pretty {
		u = a.base + "/?rest_route=/"
	}
	b, _, st, err := getBody(u)
	var d struct {
		Name string `json:"name"`
	}
	if err != nil || st != 200 || json.Unmarshal(b, &d) != nil || d.Name == "" {
		return ""
	}
	return d.Name
}
