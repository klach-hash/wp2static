package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Every key used in templates as {{.T.key}} must exist in the dictionary –
// a missing key yields empty text, so this is enforced here.
func TestTemplateKeysExist(t *testing.T) {
	re := regexp.MustCompile(`\$?\.T\.([A-Za-z0-9_]+)`)
	for _, tpl := range []string{panelTemplates, wizardTemplates} {
		for _, m := range re.FindAllStringSubmatch(tpl, -1) {
			if _, ok := msg[m[1]]; !ok {
				t.Errorf("brak tłumaczenia dla klucza %q", m[1])
			}
		}
	}
}

func testPanel() *panel {
	p := &panel{port: 8090, pvPort: 8091}
	p.tpl = parseTemplates()
	return p
}

func TestTemplatesRender(t *testing.T) {
	p := testPanel()
	post := Post{ID: 1, Date: "2024-05-12T12:00:00", Path: "/aktualnosci/x/", Title: Rendered{"Tytuł"}}
	data := map[string]map[string]any{
		"index": {"Dir": "/tmp/proj", "ProjectName": "Proj", "Posts": []Post{post}, "Pages": []Post{post}},
		"edit":  {"P": post, "Kind": "post", "New": true},
		"log":   {"Lines": []string{"linia"}, "Failed": false},
		"settings": {"NewsPath": "/aktualnosci/", "PerPage": 10,
			"FTP": FTPConfig{Host: "h", Port: 21, Dir: "/", TLS: true}},
		"start": {"Err": "", "Base": "/tmp", "Projects": []Project{{Name: "P", Folder: "/tmp", Site: "https://x"}}},
		"job":   nil,
	}
	checks := map[string][2]string{ // [English text, Polish text]
		"index":    {"Open preview", "Otwórz podgląd"},
		"edit":     {"New post", "Nowa aktualność"},
		"log":      {"← Back", "← Wróć"},
		"settings": {"Posts per page", "Wpisów na stronie"},
		"start":    {"Create project and migrate the site", "Utwórz projekt i przenieś stronę"},
		"job":      {"Migrating the site…", "Trwa przenoszenie strony…"},
	}
	for name, d := range data {
		for i, lang := range []string{langEN, langPL} {
			r := httptest.NewRequest("GET", "http://127.0.0.1:8090/", nil)
			r.AddCookie(&http.Cookie{Name: langCookie, Value: lang})
			var b strings.Builder
			if err := p.tpl.ExecuteTemplate(&b, name, p.viewData(r, d)); err != nil {
				t.Fatalf("%s/%s: %v", name, lang, err)
			}
			out := b.String()
			if !strings.Contains(out, checks[name][i]) {
				t.Errorf("%s/%s: brak tekstu %q", name, lang, checks[name][i])
			}
			if !strings.Contains(out, `lang="`+lang+`"`) {
				t.Errorf("%s/%s: brak atrybutu lang=%q w <html>", name, lang, lang)
			}
			if strings.Contains(out, "<no value>") {
				t.Errorf("%s/%s: szablon zawiera <no value>", name, lang)
			}
		}
	}
}

func TestPreviewURL(t *testing.T) {
	p := &panel{port: 8087, pvPort: 8088} // tak jak w kontenerze
	cases := []struct{ host, want string }{
		{"127.0.0.1:8090", "http://127.0.0.1:8091"}, // porty zmapowane (Docker)
		{"127.0.0.1:8087", "http://127.0.0.1:8088"}, // bez mapowania (exe)
		{"localhost:9000", "http://localhost:9001"}, //
		{"192.168.1.5:8090", "http://192.168.1.5:8091"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "http://"+c.host+"/", nil)
		r.Host = c.host
		if got := p.previewURL(r); got != c.want {
			t.Errorf("Host %s: oczekiwano %s, jest %s", c.host, c.want, got)
		}
	}
	q := &panel{port: 8090, pvPort: 8092} // preview on a different port than panel+1
	r := httptest.NewRequest("GET", "http://127.0.0.1:8090/", nil)
	if got := q.previewURL(r); got != "http://127.0.0.1:8092" {
		t.Errorf("oczekiwano http://127.0.0.1:8092, jest %s", got)
	}
}
