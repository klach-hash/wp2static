// wp2static – WordPress to static site migration with a local news panel.
package main

import (
	"flag"
	"fmt"
	"os"
)

const usage = `wp2static – migracja WordPress -> statyczna strona

Najprościej: uruchom bez argumentów (podwójne kliknięcie na wp2static.exe) –
otworzy się kreator w przeglądarce: podajesz nazwę projektu, adres strony
WordPress i folder na dane, a narzędzie samo pobiera treść i generuje stronę.
Można mieć wiele projektów (różnych stron) na raz.

Migracja z linii poleceń (dla zaawansowanych, zamiast kreatora):
  wp2static import   <adres-strony> [-out katalog]   treści + media
  wp2static template <adres-strony> [-out katalog]   szablon + CSS/JS/fonty
  wp2static all      <adres-strony> [-out katalog]   oba kroki

Codzienna praca:
  wp2static panel   [-out katalog]   panel w przeglądarce (http://127.0.0.1:8090) + podgląd (:8091)
  wp2static build   [-out katalog]   wygeneruj stronę do <katalog>/dist
  wp2static preview [-out katalog]   sam podgląd dist
  wp2static publish [-out katalog]   build + wysyłka zmian przez FTP/FTPS
`

func main() {
	// No arguments (double-click in Explorer) – if something goes wrong,
	// the console window waits for Enter instead of closing immediately.
	interactive := len(os.Args) < 2
	cmd, args := "panel", []string{} // no arguments: open the panel in the browser
	if len(os.Args) >= 2 {
		cmd, args = os.Args[1], os.Args[2:]
	}
	site := ""
	switch cmd {
	case "import", "template", "all":
		if len(args) < 1 {
			fmt.Print(usage)
			os.Exit(2)
		}
		site, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	out := fs.String("out", "./site-export", "katalog projektu")
	addr := fs.String("addr", "127.0.0.1:8090", "adres panelu")
	fs.Parse(args)
	outSet := false
	fs.Visit(func(f *flag.Flag) { outSet = outSet || f.Name == "out" })
	if cmd == "panel" && !outSet {
		*out = lastProject()
	}

	logf := func(f string, a ...any) { fmt.Printf(f+"\n", a...) }
	var err error
	switch cmd {
	case "import":
		err = runImport(site, *out)
	case "template":
		err = runTemplate(site, *out)
	case "all":
		if err = runImport(site, *out); err == nil {
			err = runTemplate(site, *out)
		}
	case "build":
		err = buildSite(*out, logf)
	case "preview":
		err = runPreview(*out, "127.0.0.1:8091")
	case "panel":
		err = runPanel(*out, *addr)
	case "publish":
		if err = buildSite(*out, logf); err == nil {
			err = publishSite(*out, logf)
		}
	default:
		fmt.Print(usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "BŁĄD:", err)
		if interactive {
			fmt.Println("\nNaciśnij Enter, aby zamknąć to okno...")
			fmt.Scanln()
		}
		os.Exit(1)
	}
}
