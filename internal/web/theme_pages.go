package web

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// pageTheme задаёт отдельный каркас, контент, ресурсы и данные страницы.
// Path нужен только новым страницам; существующие маршруты не переопределяются.
type pageTheme struct {
	Path         string            `json:"path"`
	Template     string            `json:"template"`
	Layout       string            `json:"layout"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	RequireLogin bool              `json:"require_login"`
	Stylesheets  []string          `json:"stylesheets"`
	Scripts      []string          `json:"scripts"`
	Texts        map[string]string `json:"texts"`
	Data         map[string]any    `json:"data"`
	Blocks       []Block           `json:"blocks"`
}

var pageNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func (r *renderer) buildCustomPages() error {
	paths := map[string]bool{}
	for name, settings := range r.theme.Pages {
		if !pageNameRE.MatchString(name) {
			return fmt.Errorf("неверное имя страницы %q", name)
		}
		if _, builtin := r.pages[name+".html"]; builtin {
			if settings.Path != "" {
				return fmt.Errorf("path нельзя менять у встроенной страницы %s", name)
			}
			continue
		}
		path := settings.Path
		if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#{}\\") || strings.HasPrefix(path, "//") ||
			path == "/" || strings.HasSuffix(path, "/") || strings.Contains(path, "..") || reservedPagePath(path) || paths[path] {
			return fmt.Errorf("неверный или занятый путь страницы %q", path)
		}
		paths[path] = true
		t, err := r.build(name + ".html")
		if err != nil {
			return err
		}
		r.pages[name+".html"] = t
	}
	return nil
}

func reservedPagePath(path string) bool {
	for _, prefix := range []string{"/admin", "/account", "/auth", "/webhooks", "/static", "/support", "/partner", "/pay", "/login", "/logout", "/buy", "/trial", "/connect", "/terms", "/privacy", "/healthz", "/happ", "/favicon.ico"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func (r *renderer) componentFiles() ([]string, error) {
	if r.webDir == "" {
		return nil, nil
	}
	root := filepath.Join(r.webDir, "templates", "components")
	var names []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".html") {
			rel, err := filepath.Rel(filepath.Join(r.webDir, "templates"), path)
			if err != nil {
				return err
			}
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

func (r *renderer) text(key string, fallback ...string) string {
	if value, ok := r.theme.Texts[key]; ok {
		return value
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return key
}

func templateDict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict: нужны пары ключ-значение")
	}
	out := map[string]any{}
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: ключ должен быть строкой")
		}
		out[key] = values[i+1]
	}
	return out, nil
}

func (s *Server) customPage(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	for name, settings := range s.render.theme.Pages {
		if settings.Path != r.URL.Path {
			continue
		}
		if settings.RequireLogin {
			if _, ok := s.requireSubject(w, r, r.URL.Path); !ok {
				return true
			}
		}
		s.renderPage(w, r, name+".html", http.StatusOK, pageMeta{}, settings.Data)
		return true
	}
	return false
}
