package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	handler "neomovies-tg-bot/api"
)

func main() {
	_ = loadDotEnv(".env")

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "7955"
	}

	if strings.TrimSpace(os.Getenv("LOCAL_POLLING")) == "1" {
		go startPolling()
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve from frontend/dist first
		staticPath := filepath.Join(".", "frontend", "dist", r.URL.Path)
		if _, err := os.Stat(staticPath); err == nil {
			http.ServeFile(w, r, staticPath)
			return
		}
		// Fallback to index.html for SPA routing
		http.ServeFile(w, r, filepath.Join(".", "frontend", "dist", "index.html"))
	})

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		handler.Handler(w, r)
	})

	addr := ":" + port
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func startPolling() {
	token := strings.TrimSpace(os.Getenv("BOT_TOKEN"))
	if token == "" {
		log.Printf("LOCAL_POLLING=1 but BOT_TOKEN is empty")
		return
	}

	if len(token) > 8 {
		log.Printf("polling started (token=%s...)", token[:8])
	} else {
		log.Printf("polling started")
	}

	base := fmt.Sprintf("https://api.telegram.org/bot%s", token)
	deleteWebhook(base)

	offset := 0
	client := &http.Client{Timeout: 45 * time.Second}

	for {
		u := fmt.Sprintf("%s/getUpdates?timeout=30&allowed_updates=%s&offset=%d", base, urlQueryAllowedUpdates(), offset)
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("polling error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("polling status %d: %s", resp.StatusCode, string(b))
			time.Sleep(2 * time.Second)
			continue
		}

		var out struct {
			OK     bool              `json:"ok"`
			Result []json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(b, &out); err != nil {
			log.Printf("polling decode error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if len(out.Result) > 0 {
			log.Printf("polling received %d update(s)", len(out.Result))
		}
		for _, raw := range out.Result {
			var upd struct {
				UpdateID int `json:"update_id"`
			}
			_ = json.Unmarshal(raw, &upd)
			if upd.UpdateID >= offset {
				offset = upd.UpdateID + 1
			}

			r := httptest.NewRequest(http.MethodPost, "http://localhost/api/webhook", bytes.NewReader(raw))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.Handler(w, r)
			if w.Code != 200 {
				log.Printf("handler returned status=%d for update_id=%d", w.Code, upd.UpdateID)
			}
		}
	}
}

func deleteWebhook(base string) {
	client := &http.Client{Timeout: 15 * time.Second}
	u := base + "/deleteWebhook?drop_pending_updates=true"
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("deleteWebhook error: %v", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	log.Printf("deleteWebhook status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func urlQueryAllowedUpdates() string {
	// telegram expects json array as string; encode minimal set we use
	// message, callback_query, inline_query, chosen_inline_result
	return "%5B%22message%22%2C%22callback_query%22%2C%22inline_query%22%2C%22chosen_inline_result%22%5D"
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, "\"'")
		// allow "export KEY=..."
		if strings.HasPrefix(k, "export ") {
			k = strings.TrimSpace(strings.TrimPrefix(k, "export "))
		}
		// allow PORT=:8080 style
		if k == "PORT" && strings.HasPrefix(v, ":") {
			if p, err := strconv.Atoi(strings.TrimPrefix(v, ":")); err == nil {
				v = strconv.Itoa(p)
			}
		}
		if k == "" {
			continue
		}
		if os.Getenv(k) != "" {
			continue
		}
		_ = os.Setenv(k, v)
	}
	return scanner.Err()
}
