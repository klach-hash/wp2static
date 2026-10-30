package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ---------- migration wizard ----------

// jobState tracks the progress of a migration running in the background,
// so the /job page can poll it from the browser.
type jobState struct {
	mu    sync.Mutex
	lines []string
	done  bool
	fail  bool
}

// log appends a line to the journal; matches the sink variable signature.
func (j *jobState) log(s string) {
	j.mu.Lock()
	j.lines = append(j.lines, s)
	j.mu.Unlock()
}

func (j *jobState) finish(err error) {
	j.mu.Lock()
	j.done = true
	j.fail = err != nil
	if err != nil {
		j.lines = append(j.lines, ltr("errPrefix", err))
	} else {
		j.lines = append(j.lines, ltr("jobDone"))
	}
	j.mu.Unlock()
}

func (j *jobState) snapshot() (lines []string, done, failed bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.lines...), j.done, j.fail
}

// isProject checks whether the given path is an existing project folder.
func isProject(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func lastProjectFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wp2static", "last-project.txt"), nil
}

// lastProject returns the most recently opened project folder (if it still exists).
func lastProject() string {
	f, err := lastProjectFile()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(b))
	if !isProject(dir) {
		return ""
	}
	return dir
}

func saveLastProject(dir string) {
	f, err := lastProjectFile()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(f, []byte(dir), 0o644)
}

// ---------- named project list ----------

// Project is one entry on the wizard's project list: a user-visible name,
// the data folder and the address of the original WordPress site.
type Project struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Site   string `json:"site"`
}

func projectsFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wp2static", "projects.json"), nil
}

// loadProjects returns the saved projects (only those whose folder still
// exists), sorted by most recently used.
func loadProjects() []Project {
	var list []Project
	f, err := projectsFile()
	if err != nil {
		return list
	}
	_ = readJSON(f, &list)
	out := make([]Project, 0, len(list))
	for _, p := range list {
		if isProject(p.Folder) {
			out = append(out, p)
		}
	}
	return out
}

func saveProjects(list []Project) {
	f, err := projectsFile()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return
	}
	_ = writeJSON(f, list)
}

// upsertProject adds a new project or updates an existing one (by folder)
// and moves it to the front of the list (most recently used).
func upsertProject(p Project) {
	list := loadProjects()
	out := make([]Project, 0, len(list)+1)
	out = append(out, p)
	for _, x := range list {
		if x.Folder != p.Folder {
			out = append(out, x)
		}
	}
	saveProjects(out)
}

// findProject returns the saved project for the given folder, if any.
func findProject(folder string) (Project, bool) {
	for _, p := range loadProjects() {
		if p.Folder == folder {
			return p, true
		}
	}
	return Project{}, false
}

// openBrowser opens the address in the system's default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
