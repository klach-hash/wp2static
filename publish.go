package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type manifest struct {
	Target string            `json:"target"`
	Files  map[string]string `json:"files"`
}

func hashDist(dist string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dist, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		rel, _ := filepath.Rel(dist, p)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	return out, err
}

// publishSite uploads only changed files to the host and removes those that no longer exist.
func publishSite(out string, logf func(string, ...any)) error {
	cfg := loadConfig(out)
	dist := filepath.Join(out, "dist")
	if !fileExists(filepath.Join(dist, "index.html")) {
		return errors.New(T(currentLogLang(), "noDist"))
	}
	local, err := hashDist(dist)
	if err != nil {
		return err
	}
	target := cfg.FTP.Host + "|" + cfg.FTP.Dir
	mfPath := filepath.Join(out, "publish-manifest.json")
	old := manifest{}
	_ = readJSON(mfPath, &old)
	if old.Target != target || old.Files == nil {
		old = manifest{Target: target, Files: map[string]string{}}
	}

	var up, del []string
	for rel, h := range local {
		if old.Files[rel] != h {
			up = append(up, rel)
		}
	}
	for rel := range old.Files {
		if _, ok := local[rel]; !ok {
			del = append(del, rel)
		}
	}
	// HTML last: pages must not point to assets that have not been uploaded yet
	sort.Slice(up, func(i, j int) bool {
		hi, hj := strings.HasSuffix(up[i], ".html"), strings.HasSuffix(up[j], ".html")
		if hi != hj {
			return !hi
		}
		return up[i] < up[j]
	})
	if len(up) == 0 && len(del) == 0 {
		logf(ltr("nothingToSend"))
		return nil
	}
	logf(ltr("connecting", cfg.FTP.Host, map[bool]string{true: "S", false: ""}[cfg.FTP.TLS]))
	c, err := ftpDial(cfg.FTP)
	if err != nil {
		return err
	}
	defer c.Close()

	// target directory (e.g. /public_html) – create it if missing
	base := ""
	for _, seg := range strings.Split(cfg.FTP.Dir, "/") {
		if seg != "" {
			base += "/" + seg
			c.Mkdir(base)
		}
	}
	remote := func(rel string) string { return path.Join("/", cfg.FTP.Dir, rel) }
	made := map[string]bool{}
	mkdirs := func(rel string) {
		dir := ""
		for _, seg := range strings.Split(path.Dir(rel), "/") {
			if seg == "." || seg == "" {
				continue
			}
			dir = path.Join(dir, seg)
			if !made[dir] {
				c.Mkdir(remote(dir))
				made[dir] = true
			}
		}
	}
	save := func() { _ = writeJSON(mfPath, old) }
	for i, rel := range up {
		mkdirs(rel)
		f, err := os.Open(filepath.Join(dist, filepath.FromSlash(rel)))
		if err != nil {
			save()
			return err
		}
		err = c.Store(remote(rel), f)
		f.Close()
		if err != nil {
			save()
			return fmt.Errorf("%s: %w", Tf(currentLogLang(), "uploadFailed", rel), err)
		}
		old.Files[rel] = local[rel]
		if (i+1)%25 == 0 {
			logf(ltr("sendProgress", i+1, len(up)))
		}
	}
	for _, rel := range del {
		_ = c.Delete(remote(rel))
		delete(old.Files, rel)
		for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) { // clean up empty directories
			c.Rmdir(remote(d))
		}
	}
	save()
	logf(ltr("published", len(up), len(del)))
	return nil
}
