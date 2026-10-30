package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Panel UI languages – English by default, the choice is stored in a cookie.
const (
	langEN = "en"
	langPL = "pl"

	langCookie = "wp2s_lang"
)

// msg is the message dictionary: {English text, Polish text}.
var msg = map[string][2]string{
	// ---------- panel skeleton ----------
	"docTitle":      {"Panel", "Panel"},
	"panelTitle":    {"Site panel", "Panel strony"},
	"project":       {"Project:", "Projekt:"},
	"switchProject": {"switch project / new migration", "zmień projekt / nowa migracja"},
	"news":          {"Posts", "Aktualności"},
	"pages":         {"Pages", "Strony"},
	"edit":          {"Edit", "Edytuj"},
	"del":           {"Delete", "Usuń"},
	"confirmDelete": {"Delete this item?", "Usunąć?"},
	"save":          {"Save", "Zapisz"},
	"cancel":        {"Cancel", "Anuluj"},
	"back":          {"← Back", "← Wróć"},
	"done":          {"Done", "Gotowe"},
	"failed":        {"Failed", "Nie udało się"},
	"title":         {"Title", "Tytuł"},
	"date":          {"Date", "Data"},
	"content":       {"Content", "Treść"},
	"heading":       {"Heading", "Nagłówek"},
	"paragraph":     {"Paragraph", "Akapit"},
	"list":          {"List", "Lista"},
	"link":          {"Link", "Link"},
	"imagePdf":      {"Image / PDF", "Zdjęcie / PDF"},
	"linkPrompt":    {"Link address (https://…)", "Adres linku (https://…)"},
	"newPostTitle":  {"New post", "Nowa aktualność"},
	"editPostTitle": {"Edit post", "Edycja aktualności"},
	"newPageTitle":  {"New page", "Nowa strona"},
	"editPageTitle": {"Edit page", "Edycja strony"},

	// ---------- panel: buttons ----------
	"newPost":        {"+ New post", "+ Nowa aktualność"},
	"newPage":        {"+ New page", "+ Nowa strona"},
	"build":          {"Build preview", "Zbuduj podgląd"},
	"openPreview":    {"Open preview", "Otwórz podgląd"},
	"publish":        {"Publish", "Opublikuj"},
	"confirmPublish": {"Send changes to the host?", "Wysłać zmiany na hosting?"},
	"settings":       {"Settings", "Ustawienia"},

	// ---------- panel: settings ----------
	"newsPath":   {"News list address", "Adres listy aktualności"},
	"perPage":    {"Posts per page", "Wpisów na stronie"},
	"ftpSection": {"Hosting (FTP)", "Hosting (FTP)"},
	"server":     {"Server", "Serwer"},
	"port":       {"Port", "Port"},
	"user":       {"Login", "Login"},
	"pass":       {"Password (empty = no change)", "Hasło (puste = bez zmian)"},
	"serverDir":  {"Directory on the server", "Katalog na serwerze"},
	"tls":        {"FTPS encryption (recommended)", "Szyfrowanie FTPS (zalecane)"},

	// ---------- wizard ----------
	"yourProjects":  {"Your projects", "Twoje projekty"},
	"openBtn":       {"Open", "Otwórz"},
	"newProject":    {"New project", "Nowy projekt"},
	"projectName":   {"Project name", "Nazwa projektu"},
	"projectNamePh": {"e.g. My company website", "np. Moja strona firmowa"},
	"wpUrl":         {"Address of the WordPress site to migrate", "Adres strony WordPress do przeniesienia"},
	"wpUrlPh":       {"https://example.com", "https://przyklad.pl"},
	"folderLabel":   {"Folder on this computer where the project data will be stored", "Folder na tym komputerze, gdzie zapisać dane projektu"},
	"folderHint":    {"The folder will be created. It will hold all project data – it is worth making backups.", "Folder zostanie utworzony. Znajdziesz w nim wszystkie dane projektu – warto robić jego kopię zapasową."},
	"createProject": {"Create project and migrate the site", "Utwórz projekt i przenieś stronę"},
	"migrating":     {"Migrating the site…", "Trwa przenoszenie strony…"},
	"migratingHint": {"This can take a few minutes (depending on the number of images). Do not close this window.", "To może potrwać kilka minut (zależy od liczby zdjęć). Nie zamykaj tego okna."},
	"starting":      {"Starting…", "Start…"},
	"doneExcl":      {"Done!", "Gotowe!"},
	"openPanel":     {"Open panel", "Otwórz panel"},

	// ---------- panel messages ----------
	"msgTitle":      {"Please provide a title.", "Podaj tytuł."},
	"msgSaved":      {"Saved. Click “Build preview” to see the result, or “Publish” to send it.", "Zapisano. Kliknij „Zbuduj podgląd”, aby zobaczyć efekt, albo „Opublikuj”."},
	"msgDeleted":    {"Deleted.", "Usunięto."},
	"msgSettings":   {"Settings saved.", "Ustawienia zapisane."},
	"errUploadRead": {"Could not read the file (25 MB limit).", "Nie udało się odczytać pliku (limit 25 MB)."},
	"errUploadType": {"Allowed: jpg, png, gif, webp, pdf.", "Dozwolone: jpg, png, gif, webp, pdf."},
	"errFillAll":    {"Provide the project name, site address and folder.", "Podaj nazwę projektu, adres strony i folder."},
	"errNoProject":  {"No project found in the given folder.", "Nie znaleziono projektu w podanym folderze."},

	// ---------- migration / build / publish log ----------
	"errPrefix":      {"ERROR: %v", "BŁĄD: %v"},
	"jobDone":        {"Done.", "Gotowe."},
	"checkApi":       {"→ Checking REST API: %s", "→ Sprawdzam REST API: %s"},
	"postsCount":     {"  posts: %d", "  wpisy: %d"},
	"pagesCount":     {"  pages: %d", "  strony: %d"},
	"skipped":        {"  ! %s: skipped (%v)", "  ! %s: pominięto (%v)"},
	"taxCounts":      {"  categories: %d, tags: %d, media: %d", "  kategorie: %d, tagi: %d, media: %d"},
	"catCategories":  {"categories", "kategorie"},
	"catTags":        {"tags", "tagi"},
	"catAuthors":     {"authors", "autorzy"},
	"catMedia":       {"media", "media"},
	"downloading":    {"→ Downloading media library files: %d", "→ Pobieram pliki z biblioteki mediów: %d"},
	"importDone":     {"✓ Done. Files downloaded: %d, errors: %d\n  data: %s\n  media: %s", "✓ Gotowe. Pobrano plików: %d, błędów: %d\n  dane: %s\n  media: %s"},
	"notDownloaded":  {"  ! not downloaded: %s", "  ! nie pobrano: %s"},
	"apiUnavailable": {"REST API not available at %s (disabled or blocked) – a WXR export import is required", "REST API niedostępne pod %s (wyłączone lub zablokowane) – potrzebny import z pliku eksportu WXR"},
	"badResponse":    {"%s: bad JSON response", "%s: zła odpowiedź JSON"},
	"badAddress":     {"invalid address: %q", "niepoprawny adres: %q"},
	"noDist":         {"no dist/ – build the site first", "brak dist/ – najpierw zbuduj stronę"},
	"noSiteJSON":     {"missing data/site.json – run: wp2static import", "brak data/site.json – najpierw uruchom: wp2static import"},
	"noLayout":       {"missing template/layout.html – run: wp2static template", "brak template/layout.html – najpierw uruchom: wp2static template"},
	"uploadFailed":   {"sending %s", "wysyłanie %s"},
	"ftpNoHost":      {"no FTP server address (Settings in the panel)", "brak adresu serwera FTP (Ustawienia w panelu)"},
	"ftpNoTLS":       {"the server does not support FTPS (disable TLS in settings?)", "serwer nie obsługuje FTPS (wyłącz TLS w ustawieniach?)"},
	"ftpLogin":       {"login failed", "logowanie nieudane"},
	"ftpLoginCode":   {"login failed (code %d)", "logowanie nieudane (kod %d)"},
	"ftpBadResp":     {"unexpected server response: %q", "zła odpowiedź serwera: %q"},
	"ftpBadEPSV":     {"unexpected EPSV response: %s", "zła odpowiedź EPSV: %s"},
	"ftpBadPASV":     {"unexpected PASV response: %s", "zła odpowiedź PASV: %s"},
	"noHeaderFooter": {"could not detect the header and footer in %s", "nie udało się wykryć nagłówka i stopki w %s"},
	"methodDiff":     {"page comparison", "porównanie stron"},
	"methodSemantic": {"<header>/<footer> tags", "znaczniki <header>/<footer>"},
	"noImportData":   {"  ! no data from step 1 – template from the home page only (run: import first)", "  ! brak danych z kroku 1 – szablon z samej strony głównej (uruchom najpierw: import)"},
	"fetchingPages":  {"→ Fetching reference pages: %d", "→ Pobieram strony wzorcowe: %d"},
	"method":         {"  method: %s (header %d B, footer %d B)", "  metoda: %s (nagłówek %d B, stopka %d B)"},
	"noPostList":     {"  ! no post list found on the home page – skipping home.html (layout.html will be used)", "  ! nie znalazłem listy wpisów na stronie głównej – pomijam home.html (użyj layout.html)"},
	"noPostContent":  {"  ! no post content found in the HTML – single-post.html skipped (layout.html will be used)", "  ! nie znalazłem treści wpisu w HTML – single-post.html pominięty (użyj layout.html)"},
	"noPageContent":  {"  ! no page content found in the HTML – single-page.html skipped (layout.html will be used)", "  ! nie znalazłem treści strony w HTML – single-page.html pominięty (użyj layout.html)"},
	"templateSaved":  {"  ✓ template/%s", "  ✓ template/%s"},
	"templateDone":   {"✓ Done. Theme assets downloaded: %d, errors: %d", "✓ Gotowe. Pobrano zasobów motywu: %d, błędów: %d"},
	"built":          {"✓ Built %d HTML files (posts: %d, pages: %d) in %s → %s", "✓ Zbudowano %d plików HTML (wpisów: %d, stron: %d) w %s → %s"},
	"nothingToSend":  {"✓ Nothing to send – the host is up to date.", "✓ Nic do wysłania – hosting jest aktualny."},
	"connecting":     {"→ Connecting to %s (FTP%s)…", "→ Łączę z %s (FTP%s)…"},
	"sendProgress":   {"  sent %d/%d", "  wysłano %d/%d"},
	"published":      {"✓ Published: sent %d, removed %d files.", "✓ Opublikowano: wysłano %d, usunięto %d plików."},
}

// normLang returns the language code if supported (unknown = English).
func normLang(l string) string {
	if strings.EqualFold(strings.TrimSpace(l), langPL) {
		return langPL
	}
	return langEN
}

// T returns the message in the given language (an unknown key is returned as-is).
func T(lang, key string) string {
	m, ok := msg[key]
	if !ok {
		return key
	}
	if normLang(lang) == langPL {
		return m[1]
	}
	return m[0]
}

// Tf is like T, but substitutes arguments (fmt.Sprintf).
func Tf(lang, key string, a ...any) string { return fmt.Sprintf(T(lang, key), a...) }

// texts builds the dictionary passed to templates as {{.T}}.
func texts(lang string) map[string]string {
	out := make(map[string]string, len(msg))
	for k := range msg {
		out[k] = T(lang, k)
	}
	return out
}

// langOf reads the UI language from the cookie (English by default).
func langOf(r *http.Request) string {
	if c, err := r.Cookie(langCookie); err == nil && c.Value != "" {
		return normLang(c.Value)
	}
	return langEN
}

// setLang stores the chosen language in a cookie and returns to the previous page.
func (p *panel) setLang(w http.ResponseWriter, r *http.Request) {
	l := normLang(r.URL.Query().Get("l"))
	http.SetCookie(w, &http.Cookie{
		Name: langCookie, Value: l, Path: "/",
		MaxAge: 365 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	back := "/"
	if u, err := url.Parse(r.Referer()); err == nil && u.Host == r.Host && strings.HasPrefix(u.Path, "/") {
		back = u.Path
		if back == "" || strings.HasPrefix(back, "//") {
			back = "/"
		}
		if u.RawQuery != "" {
			back += "?" + u.RawQuery
		}
	}
	http.Redirect(w, r, back, 303)
}

// Language of log messages (import, build, publish). The CLI speaks Polish;
// the panel sets the language from the cookie before each operation.
var (
	logLangMu sync.RWMutex
	logLang   = langPL
)

func setLogLang(l string) { logLangMu.Lock(); logLang = normLang(l); logLangMu.Unlock() }

func currentLogLang() string {
	logLangMu.RLock()
	defer logLangMu.RUnlock()
	return logLang
}

// ltr formats a log message in the current language.
func ltr(key string, a ...any) string { return Tf(currentLogLang(), key, a...) }

var enMonths = []string{"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December"}

// fmtDate returns the date in the selected language's format (e.g. "12 May 2024" / "12 maja 2024").
func fmtDate(lang, s string) string {
	if normLang(lang) == langPL {
		return plDate(s)
	}
	t := parseDate(s)
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s %d, %d", enMonths[t.Month()-1], t.Day(), t.Year())
}
