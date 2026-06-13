package proofwiki_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/proofwiki-cli/proofwiki"
)

func newTestClient(baseURL string) *proofwiki.Client {
	cfg := proofwiki.DefaultConfig()
	cfg.BaseURL = baseURL
	cfg.Rate = 0
	return proofwiki.NewClient(cfg)
}

func TestGetSendsUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		resp := map[string]any{
			"query": map[string]any{"search": []any{}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, _ = c.Search(context.Background(), "test", 1)
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		resp := map[string]any{
			"query": map[string]any{"search": []any{}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := proofwiki.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := proofwiki.NewClient(cfg)

	start := time.Now()
	_, err := c.Search(context.Background(), "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"query": map[string]any{
				"search": []any{
					map[string]any{"title": "Pythagorean Theorem", "snippet": "a^2 + b^2 = c^2"},
					map[string]any{"title": "Euclid's Theorem", "snippet": "Infinitely many primes"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	results, err := c.Search(context.Background(), "Pythagorean", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].Title != "Pythagorean Theorem" {
		t.Errorf("title = %q", results[0].Title)
	}
	if results[0].URL == "" {
		t.Error("URL should not be empty")
	}
}

func TestTheoremNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"query": map[string]any{
				"pages": map[string]any{
					"-1": map[string]any{
						"title":   "NonExistent",
						"missing": "",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Theorem(context.Background(), "NonExistent")
	if err == nil {
		t.Fatal("expected error for missing page")
	}
}

func TestTheoremFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"query": map[string]any{
				"pages": map[string]any{
					"1234": map[string]any{
						"pageid": 1234,
						"title":  "Pythagorean Theorem",
						"revisions": []any{
							map[string]any{"*": "== Theorem ==\nIn a right triangle, $a^2 + b^2 = c^2$.\n== Proof ==\n{{begin-proof}}\nBy geometry.\n{{end-proof}}"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	th, err := c.Theorem(context.Background(), "Pythagorean Theorem")
	if err != nil {
		t.Fatal(err)
	}
	if th.Title != "Pythagorean Theorem" {
		t.Errorf("title = %q", th.Title)
	}
	if th.URL == "" {
		t.Error("URL should not be empty")
	}
	if th.Content == "" {
		t.Error("content should not be empty after stripping")
	}
}

func TestRandom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"query": map[string]any{
				"random": []any{
					map[string]any{"id": 1, "title": "Fermat's Last Theorem"},
					map[string]any{"id": 2, "title": "Euler's Formula"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	pages, err := c.Random(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("want 2, got %d", len(pages))
	}
}

func TestListCategory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cmtitle := r.URL.Query().Get("cmtitle"); cmtitle != "Category:Theorems" {
			t.Errorf("cmtitle = %q, want Category:Theorems", cmtitle)
		}
		resp := map[string]any{
			"query": map[string]any{
				"categorymembers": []any{
					map[string]any{"title": "Pythagorean Theorem"},
					map[string]any{"title": "Fermat's Last Theorem"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	pages, err := c.ListCategory(context.Background(), "Theorems", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("want 2, got %d", len(pages))
	}
	if pages[0].URL == "" {
		t.Error("URL should not be empty")
	}
}
