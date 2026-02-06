package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var iframeSrcRe = regexp.MustCompile(`(?i)src=\"([^\"]+)\"`)
var iframeDataSrcRe = regexp.MustCompile(`(?i)data-src=\"([^\"]+)\"`)

func playerHandler(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	idType := strings.TrimSpace(r.URL.Query().Get("idType"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if provider == "" || idType == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
	if apiBase == "" {
		apiBase = "https://api.neomovies.ru"
	}

	u := fmt.Sprintf("%s/api/v1/players/%s/%s/%s", apiBase, url.PathEscape(provider), url.PathEscape(idType), url.PathEscape(id))

	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	content := strings.TrimSpace(string(b))
	if strings.HasPrefix(content, "<") {
		if m := iframeSrcRe.FindStringSubmatch(content); len(m) == 2 {
			http.Redirect(w, r, m[1], http.StatusFound)
			return
		}
		if m := iframeDataSrcRe.FindStringSubmatch(content); len(m) == 2 {
			http.Redirect(w, r, m[1], http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
		return
	}

	http.Redirect(w, r, content, http.StatusFound)
}
