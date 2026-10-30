package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type panel struct {
	mu     sync.RWMutex
	port   int // panel port (internal)
	pvPort int // preview port (internal)
	out    string
	tpl    *template.Template
	host   string
	job    *jobState
}

// viewData adds the UI language, dictionary and preview URL to the template data.
func (p *panel) viewData(r *http.Request, d map[string]any) map[string]any {
	if d == nil {
		d = map[string]any{}
	}
	lang := langOf(r)
	d["Lang"], d["T"], d["PreviewURL"] = lang, texts(lang), p.previewURL(r)
	return d
}

// previewURL returns the preview address as seen from the browser. The panel
// and the preview listen on consecutive ports, and in Docker the ports are
// additionally mapped to other ones – so we take the port from the Host header
// and shift it by the same difference. The preview port can also be given
// explicitly via the WP2STATIC_PREVIEW_PORT variable.
func (p *panel) previewURL(r *http.Request) string {
	host, port := "127.0.0.1", p.pvPort
	if h, pt, err := net.SplitHostPort(r.Host); err == nil {
		host = h
		if ext, err := strconv.Atoi(pt); err == nil && p.port > 0 {
			port = ext + (p.pvPort - p.port)
		}
	} else if h := strings.Trim(r.Host, "[]"); h != "" && h != "0.0.0.0" && h != "::" {
		host = h
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if v, err := strconv.Atoi(os.Getenv("WP2STATIC_PREVIEW_PORT")); err == nil && v > 0 && v < 65536 {
		port = v
	}
	if port <= 0 || port > 65535 {
		port = p.pvPort
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// listenNear tries to listen on the start port and, if it is taken/reserved,
// falls back to a dozen or so subsequent ports.
func listenNear(host string, start, tries int) (net.Listener, int, error) {
	var lastErr error
	for i := 0; i <= tries; i++ {
		port := start + i
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			return ln, port, nil
		}
		lastErr = err
	}
	return nil, 0, lastErr
}

func runPanel(out, addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	basePort, _ := strconv.Atoi(port)
	// The start port is sometimes taken or reserved by the system (this happens
	// on Windows – see "netsh interface ipv4 show excludedportrange") –
	// so we try subsequent ports instead of giving up right away.
	ln, boundPort, err := listenNear(host, basePort, 20)
	if err != nil {
		return fmt.Errorf("nie udało się znaleźć wolnego portu w zakresie %d–%d (czy panel już działa? sprawdź zaporę sieciową): %w", basePort, basePort+20, err)
	}
	port = strconv.Itoa(boundPort)
	addr = net.JoinHostPort(host, port)
	// the preview listens on the next free port (usually panel+1)
	pvPort := boundPort + 1
	if pln, pport, err := listenNear(host, boundPort+1, 20); err == nil {
		pln.Close() // release – ListenAndServe below will bind the same port again
		pvPort = pport
	}
	pv := net.JoinHostPort(host, strconv.Itoa(pvPort))
	p := &panel{host: addr, port: boundPort, pvPort: pvPort}
	if out != "" {
		if abs, err := filepath.Abs(out); err == nil && isProject(abs) {
			p.out = abs
		}
	}
	p.tpl = parseTemplates()
	go func() {
		log.Println("podgląd:", http.ListenAndServe(pv, previewHandler(func() string {
			if d := p.dir(); d != "" {
				return filepath.Join(d, "dist")
			}
			return ""
		})))
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.index)
	mux.HandleFunc("/edit", p.edit)
	mux.HandleFunc("/save", p.save)
	mux.HandleFunc("/delete", p.del)
	mux.HandleFunc("/upload", p.upload)
	mux.HandleFunc("/build", p.action(false))
	mux.HandleFunc("/publish", p.action(true))
	mux.HandleFunc("/settings", p.settings)
	mux.HandleFunc("/lang", p.setLang)
	mux.HandleFunc("/start", p.start)
	mux.HandleFunc("/migrate", p.migrate)
	mux.HandleFunc("/progress", p.progress)
	mux.HandleFunc("/job", p.jobJSON)
	mux.HandleFunc("/open", p.openProject)
	mux.Handle("/wp-content/", p.assets())
	url := "http://" + addr
	if host == "0.0.0.0" {
		url = "http://127.0.0.1:" + port
	}
	fmt.Printf("Panel: %s\nPodgląd: http://%s\nCtrl+C kończy.\n", url, pv)
	openBrowser(url)
	return http.Serve(ln, p.guard(mux))
}

// parseTemplates prepares the panel and wizard template set.
func parseTemplates() *template.Template {
	return template.Must(template.New("").Funcs(template.FuncMap{
		"unesc": html.UnescapeString, "date": fmtDate,
		"safe": func(s string) template.HTML { return template.HTML(s) },
		"day": func(s string) string {
			if len(s) >= 10 {
				return s[:10]
			}
			return time.Now().Format("2006-01-02")
		},
	}).Parse(panelTemplates + wizardTemplates))
}

// guard: localhost only + block cross-site requests (CSRF / DNS rebinding).
// Note: we do not compare the port with p.port – when running in Docker, the
// port visible in the Host header (e.g. mapped by docker-compose) usually
// differs from the port the process listens on inside the container.
func (p *panel) guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hn, _, err := net.SplitHostPort(r.Host); err != nil || (hn != "127.0.0.1" && hn != "localhost") {
			http.Error(w, "zły host", 403)
			return
		}
		if r.Method == "POST" {
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					http.Error(w, "zły origin", 403)
					return
				}
			}
		}
		switch r.URL.Path {
		case "/start", "/migrate", "/progress", "/job", "/open", "/lang":
		default:
			if p.dir() == "" {
				http.Redirect(w, r, "/start", 303)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

func (p *panel) render(w http.ResponseWriter, name string, data any) {
	var b bytes.Buffer
	if err := p.tpl.ExecuteTemplate(&b, name, data); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b.Bytes())
}

func (p *panel) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	storeMu.Lock()
	posts, pages := loadList(p.dir(), "posts.json"), loadList(p.dir(), "pages.json")
	storeMu.Unlock()
	sort.SliceStable(posts, func(i, j int) bool { return posts[i].Date > posts[j].Date })
	name := p.dir()
	if proj, ok := findProject(p.dir()); ok && proj.Name != "" {
		name = proj.Name
	}
	p.render(w, "index", p.viewData(r, map[string]any{"Dir": p.dir(), "ProjectName": name, "Posts": posts, "Pages": pages, "Msg": r.URL.Query().Get("msg")}))
}

func (p *panel) find(kind string, id int) (Post, bool) {
	name := map[string]string{"page": "pages.json"}[kind]
	if name == "" {
		name = "posts.json"
	}
	for _, x := range loadList(p.dir(), name) {
		if x.ID == id {
			return x, true
		}
	}
	return Post{}, false
}

func (p *panel) edit(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "page" {
		kind = "post"
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	storeMu.Lock()
	post, ok := p.find(kind, id)
	storeMu.Unlock()
	if !ok {
		post = Post{Date: time.Now().Format("2006-01-02T15:04:05")}
	}
	p.render(w, "edit", p.viewData(r, map[string]any{"P": post, "Kind": kind, "New": !ok}))
}

func (p *panel) save(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	kind := r.FormValue("kind")
	file := "posts.json"
	if kind == "page" {
		file = "pages.json"
	} else {
		kind = "post"
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Redirect(w, r, "/?msg="+url.QueryEscape(T(langOf(r), "msgTitle")), 303)
		return
	}
	content := r.FormValue("content")
	day := r.FormValue("date")
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		d = time.Now()
	}
	id, _ := strconv.Atoi(r.FormValue("id"))

	storeMu.Lock()
	defer storeMu.Unlock()
	posts, pages := loadList(p.dir(), "posts.json"), loadList(p.dir(), "pages.json")
	list := posts
	if kind == "page" {
		list = pages
	}
	idx := -1
	for i := range list {
		if list[i].ID == id && id != 0 {
			idx = i
		}
	}
	var item Post
	if idx >= 0 {
		item = list[idx]
	} else {
		item = Post{ID: nextID(posts, pages), Status: "publish"}
		slug := uniqueSlug(slugify(firstNonEmpty(r.FormValue("slug"), title)), posts, pages)
		item.Slug = slug
		if kind == "page" {
			item.Path = "/" + slug + "/"
		} else {
			item.Path = newPostPath(posts, loadConfig(p.dir()), slug, d)
		}
		item.Link = "" // absolute address is not needed
	}
	if len(item.Date) < 19 || item.Date[:10] != day { // date changed
		item.Date = d.Format("2006-01-02") + "T12:00:00"
	}
	item.Modified = time.Now().Format("2006-01-02T15:04:05")
	item.Title = Rendered{html.EscapeString(title)}
	item.Content = Rendered{content}
	item.ContentLocal = content
	if item.Excerpt.Rendered == "" || idx < 0 || r.FormValue("regen") == "1" {
		item.Excerpt = Rendered{excerptOf(content)}
	}
	if idx >= 0 {
		list[idx] = item
	} else {
		list = append(list, item)
	}
	if err := saveList(p.dir(), file, list); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/?msg="+url.QueryEscape(T(langOf(r), "msgSaved")), 303)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (p *panel) del(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	id, _ := strconv.Atoi(r.FormValue("id"))
	file := "posts.json"
	if r.FormValue("kind") == "page" {
		file = "pages.json"
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	var keep []Post
	for _, x := range loadList(p.dir(), file) {
		if x.ID != id {
			keep = append(keep, x)
		}
	}
	_ = saveList(p.dir(), file, keep)
	http.Redirect(w, r, "/?msg="+url.QueryEscape(T(langOf(r), "msgDeleted")), 303)
}

var reSafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
var okUpload = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".pdf": true}

func (p *panel) upload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(m string) { json.NewEncoder(w).Encode(map[string]any{"error": m}) }
	if r.Method != "POST" {
		fail("POST")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		fail(T(langOf(r), "errUploadRead"))
		return
	}
	defer f.Close()
	name := reSafeName.ReplaceAllString(plMap.Replace(strings.ToLower(hdr.Filename)), "-")
	ext := filepath.Ext(name)
	if !okUpload[ext] {
		fail(T(langOf(r), "errUploadType"))
		return
	}
	now := time.Now()
	rel := fmt.Sprintf("/wp-content/uploads/%s/%s", now.Format("2006/01"), name)
	dst := filepath.Join(p.dir(), "site", filepath.FromSlash(rel))
	for i := 2; fileExists(dst); i++ {
		rel = fmt.Sprintf("/wp-content/uploads/%s/%s-%d%s", now.Format("2006/01"), strings.TrimSuffix(name, ext), i, ext)
		dst = filepath.Join(p.dir(), "site", filepath.FromSlash(rel))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		fail(err.Error())
		return
	}
	o, err := os.Create(dst)
	if err != nil {
		fail(err.Error())
		return
	}
	defer o.Close()
	if _, err := io.Copy(o, f); err != nil {
		fail(err.Error())
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"url": rel, "name": hdr.Filename, "image": ext != ".pdf"})
}

func (p *panel) action(publish bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST", 405)
			return
		}
		var lines []string
		logf := func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }
		setLogLang(langOf(r))
		storeMu.Lock()
		err := buildSite(p.dir(), logf)
		storeMu.Unlock()
		if err == nil && publish {
			err = publishSite(p.dir(), logf)
		}
		if err != nil {
			logf(ltr("errPrefix", err))
		}
		p.render(w, "log", p.viewData(r, map[string]any{"Lines": lines, "Failed": err != nil}))
	}
}

func (p *panel) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		c := loadConfig(p.dir())
		c.NewsPath = r.FormValue("news_path")
		c.PerPage, _ = strconv.Atoi(r.FormValue("per_page"))
		c.FTP.Host = strings.TrimSpace(r.FormValue("host"))
		c.FTP.Port, _ = strconv.Atoi(r.FormValue("port"))
		c.FTP.User = strings.TrimSpace(r.FormValue("user"))
		if pw := r.FormValue("pass"); pw != "" {
			c.FTP.Pass = pw
		}
		c.FTP.Dir = r.FormValue("dir")
		c.FTP.TLS = r.FormValue("tls") == "1"
		if err := saveConfig(p.dir(), c); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/?msg="+url.QueryEscape(T(langOf(r), "msgSettings")), 303)
		return
	}
	c := loadConfig(p.dir())
	p.render(w, "settings", p.viewData(r, map[string]any{"NewsPath": c.NewsPath, "PerPage": c.PerPage, "FTP": c.FTP}))
}

func (p *panel) dir() string        { p.mu.RLock(); defer p.mu.RUnlock(); return p.out }
func (p *panel) setDir(d string)    { p.mu.Lock(); p.out = d; p.mu.Unlock() }
func (p *panel) setJob(j *jobState) { p.mu.Lock(); p.job = j; p.mu.Unlock() }
func (p *panel) getJob() *jobState  { p.mu.RLock(); defer p.mu.RUnlock(); return p.job }

// assets serves files from <project>/site/wp-content (theme + uploaded media),
// so the preview in the editor works right away instead of only after "Build".
func (p *panel) assets() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dir := p.dir()
		if dir == "" {
			http.NotFound(w, r)
			return
		}
		http.StripPrefix("/wp-content/", http.FileServer(http.Dir(filepath.Join(dir, "site", "wp-content")))).ServeHTTP(w, r)
	})
}

// ---------- migration wizard ----------

// start shows the welcome page: project list + new site migration.
func (p *panel) start(w http.ResponseWriter, r *http.Request) {
	base := ""
	if home, err := os.UserHomeDir(); err == nil {
		base = filepath.Join(home, "Dokumenty WP2Static")
	} else {
		base, _ = os.Getwd()
	}
	p.render(w, "start", p.viewData(r, map[string]any{
		"Err":      r.URL.Query().Get("err"),
		"Base":     base,
		"Projects": loadProjects(),
	}))
}

// migrate receives the project name, the WordPress site address and the target
// folder, then runs import + template in the background and redirects to the
// progress page.
func (p *panel) migrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	site := strings.TrimSpace(r.FormValue("url"))
	folder := strings.TrimSpace(r.FormValue("folder"))
	if name == "" || site == "" || folder == "" {
		http.Redirect(w, r, "/start?err="+url.QueryEscape(T(langOf(r), "errFillAll")), 303)
		return
	}
	abs, err := filepath.Abs(folder)
	if err == nil {
		err = os.MkdirAll(abs, 0o755)
	}
	if err != nil {
		http.Redirect(w, r, "/start?err="+url.QueryEscape(err.Error()), 303)
		return
	}
	j := &jobState{}
	p.setJob(j)
	p.setDir(abs)
	saveLastProject(abs)
	upsertProject(Project{Name: name, Folder: abs, Site: site})
	setLogLang(langOf(r))
	go func() {
		sinkMu.Lock()
		prev := sink
		sink = j.log
		sinkMu.Unlock()
		err := runImport(site, abs)
		if err == nil {
			err = runTemplate(site, abs)
		}
		sinkMu.Lock()
		sink = prev
		sinkMu.Unlock()
		j.finish(err)
	}()
	http.Redirect(w, r, "/progress", 303)
}

// progress shows the progress page that polls /job.
func (p *panel) progress(w http.ResponseWriter, r *http.Request) {
	if p.getJob() == nil {
		http.Redirect(w, r, "/start", 303)
		return
	}
	p.render(w, "job", p.viewData(r, nil))
}

// jobJSON returns the current migration state (for JS polling).
func (p *panel) jobJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	j := p.getJob()
	if j == nil {
		json.NewEncoder(w).Encode(map[string]any{"lines": []string{}, "done": true, "error": true})
		return
	}
	lines, done, failed := j.snapshot()
	json.NewEncoder(w).Encode(map[string]any{"lines": lines, "done": done, "error": failed})
}

// openProject opens an existing, already migrated project.
func (p *panel) openProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	abs, err := filepath.Abs(strings.TrimSpace(r.FormValue("folder")))
	if err != nil || !isProject(abs) {
		http.Redirect(w, r, "/start?err="+url.QueryEscape(T(langOf(r), "errNoProject")), 303)
		return
	}
	p.setDir(abs)
	saveLastProject(abs)
	if proj, ok := findProject(abs); ok {
		upsertProject(proj) // move to the top of the list (most recently used)
	}
	http.Redirect(w, r, "/", 303)
}
