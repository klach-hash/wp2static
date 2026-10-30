package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Placeholders in templates (the step-3 generator replaces them with data):
//   <!--WP2S:TITLE--> <!--WP2S:CONTENT--> <!--WP2S:DATE_ISO--> <!--WP2S:DATE_TEXT-->
//   <!--WP2S:URL--> <!--WP2S:ID--> <!--WP2S:BODYCLASS-->

var (
	reBody     = regexp.MustCompile(`(?is)<body([^>]*)>(.*)</body>`)
	reHead     = regexp.MustCompile(`(?is)<head[^>]*>(.*?)</head>`)
	reHTMLTag  = regexp.MustCompile(`(?is)<html([^>]*)>`)
	reComment  = regexp.MustCompile(`(?s)<!--.*?-->`)
	reCurrent  = regexp.MustCompile(`\s*\bcurrent[-_](?:menu|page)[-_](?:item|ancestor|parent)\b`)
	reAriaCur  = regexp.MustCompile(`\s+aria-current=["']page["']`)
	reTitle    = regexp.MustCompile(`(?is)<title[^>]*>.*?</title>`)
	reHeadJunk = regexp.MustCompile(`(?is)<link[^>]+rel=["'](?:canonical|shortlink|alternate|https://api\.w\.org/|EditURI|wlwmanifest|prev|next)["'][^>]*>|<meta[^>]+(?:name|property)=["'](?:description|generator|robots|og:[^"']*|twitter:[^"']*)["'][^>]*>|<script[^>]+application/ld\+json[^>]*>.*?</script>`)
	reTime     = regexp.MustCompile(`(?is)(<time[^>]*datetime=")[^"]*("[^>]*>)(.*?)(</time>)`)
	reClass    = regexp.MustCompile(`(?is)\bclass=["']([^"']*)["']`)
	reAttrURL  = regexp.MustCompile(`(?i)(?:src|href|poster|data-src)=["']([^"'#]+)["']`)
	reSrcset   = regexp.MustCompile(`(?i)srcset=["']([^"']+)["']`)
	// asset paths hidden in quotes outside HTML attributes, e.g. in the theme's
	// config JSON (wp_localize_script) that points at lazily loaded CSS/JS.
	reQuotedAsset = regexp.MustCompile(`(?i)["']([^"'<>\s]+\.(?:css|js))(\?[^"'<>\s]*)?["']`)
)

type fetched struct {
	url   string
	html  string
	head  string
	attrs string // <body> attributes
	body  string // normalized <body> content
	toks  []string
}

func normalize(s string) string {
	s = reComment.ReplaceAllString(s, "")
	s = reCurrent.ReplaceAllString(s, "") // the active menu item must not end up in the template
	s = reAriaCur.ReplaceAllString(s, "")
	return s
}

func tokens(s string) []string {
	var out []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '>')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

var reVoidTag = regexp.MustCompile(`(?i)^<\s*(area|base|br|col|embed|hr|img|input|link|meta|param|source|track|wbr)\b`)

// tagDepthDelta returns the nesting-depth change introduced by the tag inside
// the token (+1 for opening, -1 for closing, 0 for self-closing/void elements
// or plain text without a tag).
func tagDepthDelta(tok string) int {
	i := strings.LastIndexByte(tok, '<')
	if i < 0 {
		return 0
	}
	tag := tok[i:]
	switch {
	case strings.HasPrefix(tag, "<!"):
		return 0
	case strings.HasPrefix(tag, "</"):
		return -1
	case strings.HasSuffix(strings.TrimRight(tag, " \t\n>"), "/"):
		return 0
	case reVoidTag.MatchString(tag):
		return 0
	default:
		return 1
	}
}

// balanced checks whether a token sequence is a self-contained, correctly
// nested HTML fragment (no tag is opened or closed "outside" the sequence).
// Used to ensure the header/footer split never cuts HTML in the middle of an
// element.
func balanced(toks []string) bool {
	depth := 0
	for _, t := range toks {
		depth += tagDepthDelta(t)
		if depth < 0 {
			return false
		}
	}
	return depth == 0
}

func fetchPage(u string) (*fetched, error) {
	b, _, status, err := getBody(u)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("%s: HTTP %d", u, status)
	}
	f := &fetched{url: u, html: string(b)}
	if m := reHead.FindStringSubmatch(f.html); m != nil {
		f.head = m[1]
	}
	m := reBody.FindStringSubmatch(f.html)
	if m == nil {
		return nil, fmt.Errorf("%s: brak <body>", u)
	}
	f.attrs, f.body = m[1], normalize(m[2])
	f.toks = tokens(f.body)
	return f, nil
}

type split struct{ pre, mid, suf string }

// Method 1: common prefix of all pages (header) and common suffix (footer).
func diffSplit(pages []*fetched) []split {
	min := len(pages[0].toks)
	for _, p := range pages {
		if len(p.toks) < min {
			min = len(p.toks)
		}
	}
	pre := 0
loopP:
	for ; pre < min; pre++ {
		for _, p := range pages[1:] {
			if p.toks[pre] != pages[0].toks[pre] {
				break loopP
			}
		}
	}
	suf := 0
loopS:
	for ; suf < min-pre-1; suf++ {
		for _, p := range pages[1:] {
			if p.toks[len(p.toks)-1-suf] != pages[0].toks[len(pages[0].toks)-1-suf] {
				break loopS
			}
		}
	}
	// Move the boundaries back to the nearest place where the header/footer are
	// themselves correctly nested HTML – otherwise the split could cut an element
	// in the middle (e.g. put page content inside a hidden search panel).
	for pre > 0 && !balanced(pages[0].toks[:pre]) {
		pre--
	}
	n0 := len(pages[0].toks)
	for suf > 0 && !balanced(pages[0].toks[n0-suf:]) {
		suf--
	}
	out := make([]split, len(pages))
	for i, p := range pages {
		n := len(p.toks)
		out[i] = split{
			pre: strings.Join(p.toks[:pre], ""),
			mid: strings.Join(p.toks[pre:n-suf], ""),
			suf: strings.Join(p.toks[n-suf:], ""),
		}
	}
	return out
}

// Method 2 (fallback): <header>...</header> and <footer>...</footer>.
func semanticSplit(body string) (split, bool) {
	low := strings.ToLower(body)
	h := strings.Index(low, "</header>")
	f := strings.LastIndex(low, "<footer")
	if h < 0 || f < 0 || f <= h {
		return split{}, false
	}
	h += len("</header>")
	return split{pre: body[:h], mid: body[h:f], suf: body[f:]}, true
}

// findLoopWrapper locates the post-list fragment inside mid (anchored at a
// unique link, e.g. the first post's address) and moves the boundaries back to
// the nearest BOUNDARIES BETWEEN TAGS where the "before" and "after" parts are
// themselves correctly nested HTML (see balanced). This way, e.g. a banner with
// a background above the news list is preserved, while the post list itself
// (different on every migration) is dropped. The cut always lands exactly on a
// token boundary (never inside a tag/attribute) – depth is computed once for
// the whole mid, not for the trimmed fragments (which could end mid-tag and
// produce a falsely "balanced" result).
func findLoopWrapper(mid, anchor string) (before, after string, ok bool) {
	if anchor == "" {
		return "", "", false
	}
	i := strings.Index(mid, anchor)
	if i < 0 {
		return "", "", false
	}
	toks := tokens(mid)
	pos := make([]int, len(toks)+1)
	depth := make([]int, len(toks)+1)
	for k, t := range toks {
		pos[k+1] = pos[k] + len(t)
		depth[k+1] = depth[k] + tagDepthDelta(t)
	}
	// token containing the anchor position
	ti := 0
	for ti < len(toks) && pos[ti+1] <= i {
		ti++
	}
	// The nesting level of mid can be >0 (e.g. when the whole page is still
	// wrapped in an open <main>/<div> inherited from "pre") – so we look for
	// the shallowest level reached on the way to the anchor, not a hardcoded
	// depth of 0.
	minDepth := depth[ti]
	for k := 1; k <= ti; k++ {
		if depth[k] < minDepth {
			minDepth = depth[k]
		}
	}
	cutTok := -1
	for k := ti; k >= 0; k-- {
		if depth[k] == minDepth {
			cutTok = k
			break
		}
	}
	tailTok := -1
	for k := ti + 1; k <= len(toks); k++ {
		if depth[k] == minDepth {
			tailTok = k
			break
		}
	}
	if cutTok <= 0 || tailTok < 0 || tailTok >= len(toks) {
		return "", "", false
	}
	return mid[:pos[cutTok]], mid[pos[tailTok]:], true
}

// findWrapper searches mid for the API post content and returns what surrounds
// it (e.g. <article><h1>title</h1> ... </article>).
func findWrapper(mid string, p *Post) (before, after string, ok bool) {
	c := strings.TrimSpace(normalize(p.Content.Rendered))
	if len(c) < 60 {
		return
	}
	a := 120
	if a > len(c)/2 {
		a = len(c) / 2
	}
	head, tail := c[:a], c[len(c)-a:]
	s, e := strings.Index(mid, head), strings.LastIndex(mid, tail)
	if s < 0 || e < 0 || e+len(tail) <= s {
		return
	}
	return placeholders(mid[:s], p), placeholders(mid[e+len(tail):], p), true
}

func placeholders(s string, p *Post) string {
	if p.Link != "" {
		s = strings.ReplaceAll(s, p.Link, "<!--WP2S:URL-->")
	}
	if t := strings.TrimSpace(p.Title.Rendered); t != "" {
		s = strings.ReplaceAll(s, t, "<!--WP2S:TITLE-->")
	}
	id := regexp.MustCompile(fmt.Sprintf(`((?:post|postid|page-id)[-_])%d\b`, p.ID))
	s = id.ReplaceAllString(s, "${1}<!--WP2S:ID-->")
	return reTime.ReplaceAllString(s, "${1}<!--WP2S:DATE_ISO-->${2}<!--WP2S:DATE_TEXT-->${4}")
}

func bodyClass(attrs string) string {
	if m := reClass.FindStringSubmatch(attrs); m != nil {
		return m[1]
	}
	return ""
}

var reClassAttr = regexp.MustCompile(`(?is)\s*\bclass=["'][^"']*["']`)

// bodyExtraAttrs returns the original page's <body> attributes other than
// "class" (e.g. data-prefix, itemscope, itemtype). Some theme CSS (e.g.
// Blocksy) targets exactly these attributes (e.g. [data-prefix="blog"] {...}),
// so losing them would stop a large part of the styles from applying at all.
func bodyExtraAttrs(attrs string) string {
	a := strings.TrimSpace(reClassAttr.ReplaceAllString(attrs, ""))
	if a == "" {
		return ""
	}
	return " " + a
}

// idToken replaces the ID number in classes (postid-11) with {ID}.
func idToken(c string, id int) string {
	re := regexp.MustCompile(fmt.Sprintf(`((?:post|postid|page-id)[-_])%d\b`, id))
	return re.ReplaceAllString(c, "${1}{ID}")
}

func cleanHead(h string) string {
	h = reHeadJunk.ReplaceAllString(h, "")
	if reTitle.MatchString(h) {
		return reTitle.ReplaceAllString(h, "<title><!--WP2S:DOCTITLE--></title>")
	}
	return h + "\n<title><!--WP2S:DOCTITLE--></title>"
}

func extractRefs(s string) []string {
	var out []string
	for _, m := range reAttrURL.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	for _, m := range reSrcset.FindAllStringSubmatch(s, -1) {
		for _, part := range strings.Split(m[1], ",") {
			if f := strings.Fields(part); len(f) > 0 {
				out = append(out, f[0])
			}
		}
	}
	for _, m := range reQuotedAsset.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1]+m[2])
	}
	return append(out, extractCSSRefs(s)...) // url() from <style> and style=""
}

// ---------- Step 2: template ----------

func runTemplate(site, out string) error {
	base, err := normalizeBase(site)
	if err != nil {
		return err
	}
	var posts, pages []Post
	_ = readJSON(filepath.Join(out, "data", "posts.json"), &posts)
	_ = readJSON(filepath.Join(out, "data", "pages.json"), &pages)

	// samples: home page + post + static page
	urls := []string{base.String() + "/"}
	var post, page *Post
	if len(posts) > 0 {
		post = &posts[0]
		urls = append(urls, post.Link)
	}
	for i := range pages {
		if pages[i].Path != "/" && len(pages[i].Content.Rendered) > 60 {
			page = &pages[i]
			urls = append(urls, page.Link)
			break
		}
	}
	if len(urls) < 2 {
		sayTr("noImportData")
	}
	sayTr("fetchingPages", len(urls))
	var pg []*fetched
	for _, u := range urls {
		f, err := fetchPage(u)
		if err != nil {
			return err
		}
		pg = append(pg, f)
	}

	var sp []split
	method := T(currentLogLang(), "methodDiff")
	if len(pg) >= 2 {
		sp = diffSplit(pg)
	}
	if sp == nil || len(sp[0].pre) < 100 || len(sp[0].suf) < 100 {
		method = T(currentLogLang(), "methodSemantic")
		sp = make([]split, len(pg))
		for i, p := range pg {
			s, ok := semanticSplit(p.body)
			if !ok {
				return errors.New(Tf(currentLogLang(), "noHeaderFooter", p.url))
			}
			sp[i] = s
		}
	}
	sayTr("method", method, len(sp[0].pre), len(sp[0].suf))

	htmlTag := "<html>"
	if m := reHTMLTag.FindStringSubmatch(pg[0].html); m != nil {
		htmlTag = "<html" + m[1] + ">"
	}
	head := cleanHead(pg[0].head)
	compose := func(before, after string) string {
		return "<!DOCTYPE html>\n" + htmlTag + "\n<head>" + head + "</head>\n" +
			`<body<!--WP2S:BODYATTRS--> class="<!--WP2S:BODYCLASS-->">` + sp[0].pre + before +
			"<!--WP2S:CONTENT-->" + after + sp[0].suf + "\n</body>\n</html>\n"
	}

	layouts := map[string]string{"layout.html": compose("", "")}
	classes := map[string]string{"home": bodyClass(pg[0].attrs)}
	attrs := map[string]string{"home": bodyExtraAttrs(pg[0].attrs)}
	if post != nil {
		if b, a, ok := findLoopWrapper(sp[0].mid, post.Link); ok {
			layouts["home.html"] = compose(b, a)
		} else {
			sayTr("noPostList")
		}
	}
	idx := 1
	if post != nil {
		classes["post"] = idToken(bodyClass(pg[idx].attrs), post.ID)
		attrs["post"] = bodyExtraAttrs(pg[idx].attrs)
		if b, a, ok := findWrapper(sp[idx].mid, post); ok {
			layouts["single-post.html"] = compose(b, a)
		} else {
			sayTr("noPostContent")
		}
		idx++
	}
	if page != nil {
		classes["page"] = idToken(bodyClass(pg[idx].attrs), page.ID)
		attrs["page"] = bodyExtraAttrs(pg[idx].attrs)
		if b, a, ok := findWrapper(sp[idx].mid, page); ok {
			layouts["single-page.html"] = compose(b, a)
		} else {
			sayTr("noPageContent")
		}
	}

	// save + download theme CSS/JS/fonts/images
	tplDir := filepath.Join(out, "template")
	if err := os.MkdirAll(tplDir, 0o755); err != nil {
		return err
	}
	var all strings.Builder
	for name, content := range layouts {
		content = relativize(content, base)
		all.WriteString(content)
		if err := os.WriteFile(filepath.Join(tplDir, name), []byte(content), 0o644); err != nil {
			return err
		}
		sayTr("templateSaved", name)
	}
	if err := writeJSON(filepath.Join(tplDir, "bodyclasses.json"), classes); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(tplDir, "bodyattrs.json"), attrs); err != nil {
		return err
	}

	m := NewMirror(base, filepath.Join(out, "site"))
	for _, r := range extractRefs(all.String()) {
		m.Get(r)
	}
	sayTr("templateDone", m.Count, len(m.Failed))
	for _, f := range m.Failed {
		sayTr("notDownloaded", f)
	}
	return nil
}
