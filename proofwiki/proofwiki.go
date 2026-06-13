// Package proofwiki is the library behind the pw command line:
// the HTTP client, request shaping, and the typed data models for ProofWiki.
//
// ProofWiki exposes the standard MediaWiki API at https://proofwiki.org/w/api.php.
// All endpoints are open and require no API key.
package proofwiki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultUserAgent identifies the client to ProofWiki.
const DefaultUserAgent = "pw/dev (+https://github.com/tamnd/proofwiki-cli)"

// ErrNotFound is returned when a page does not exist.
var ErrNotFound = fmt.Errorf("page not found")

// Config holds constructor parameters.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   "https://proofwiki.org",
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Retries:   5,
		Timeout:   30 * time.Second,
	}
}

// Client talks to the ProofWiki MediaWiki API.
type Client struct {
	http      *http.Client
	userAgent string
	baseURL   string
	rate      time.Duration
	retries   int
	mu        sync.Mutex
	last      time.Time
}

// NewClient returns a Client with the given config.
func NewClient(cfg Config) *Client {
	return &Client{
		http:      &http.Client{Timeout: cfg.Timeout},
		userAgent: cfg.UserAgent,
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		rate:      cfg.Rate,
		retries:   cfg.Retries,
	}
}

// apiURL builds a URL for the MediaWiki API.
func (c *Client) apiURL() string {
	return c.baseURL + "/w/api.php"
}

// pageURL builds a canonical ProofWiki page URL from a title.
func (c *Client) pageURL(title string) string {
	return c.baseURL + "/wiki/" + url.PathEscape(strings.ReplaceAll(title, " ", "_"))
}

// get fetches a URL with pacing and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rate <= 0 {
		return
	}
	if wait := c.rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

func (c *Client) getJSON(ctx context.Context, rawURL string, v any) error {
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

// ─── Search ──────────────────────────────────────────────────────────────────

type mwSearchResp struct {
	Query struct {
		Search []mwSearchResult `json:"search"`
	} `json:"query"`
}

type mwSearchResult struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// Search searches ProofWiki for the given query and returns up to limit results.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	params := url.Values{}
	params.Set("action", "query")
	params.Set("list", "search")
	params.Set("srsearch", query)
	params.Set("srlimit", fmt.Sprint(limit))
	params.Set("format", "json")

	rawURL := c.apiURL() + "?" + params.Encode()
	var resp mwSearchResp
	if err := c.getJSON(ctx, rawURL, &resp); err != nil {
		return nil, err
	}

	out := make([]SearchResult, 0, len(resp.Query.Search))
	for _, r := range resp.Query.Search {
		out = append(out, SearchResult{
			Title:   r.Title,
			Snippet: stripWikiMarkup(r.Snippet),
			URL:     c.pageURL(r.Title),
		})
	}
	return out, nil
}

// ─── Theorem ─────────────────────────────────────────────────────────────────

type mwRevResp struct {
	Query struct {
		Pages map[string]mwPage `json:"pages"`
	} `json:"query"`
}

type mwPage struct {
	PageID    int    `json:"pageid"`
	Title     string `json:"title"`
	Missing   string `json:"missing"`
	Revisions []struct {
		Content string `json:"*"`
	} `json:"revisions"`
}

// Theorem fetches a single ProofWiki page by title and returns a Theorem record.
func (c *Client) Theorem(ctx context.Context, title string) (Theorem, error) {
	params := url.Values{}
	params.Set("action", "query")
	params.Set("prop", "revisions")
	params.Set("rvprop", "content")
	params.Set("titles", title)
	params.Set("format", "json")

	rawURL := c.apiURL() + "?" + params.Encode()
	var resp mwRevResp
	if err := c.getJSON(ctx, rawURL, &resp); err != nil {
		return Theorem{}, err
	}

	for _, page := range resp.Query.Pages {
		if page.Missing == "" && page.PageID > 0 {
			content := ""
			if len(page.Revisions) > 0 {
				content = page.Revisions[0].Content
			}
			snippet := extractSnippet(content, 200)
			return Theorem{
				Title:   page.Title,
				URL:     c.pageURL(page.Title),
				Snippet: snippet,
				Content: stripWikiMarkup(content),
			}, nil
		}
		return Theorem{}, ErrNotFound
	}
	return Theorem{}, ErrNotFound
}

// ─── Random ──────────────────────────────────────────────────────────────────

type mwRandomResp struct {
	Query struct {
		Random []mwRandomPage `json:"random"`
	} `json:"query"`
}

type mwRandomPage struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// Random returns up to limit random ProofWiki pages.
func (c *Client) Random(ctx context.Context, limit int) ([]Theorem, error) {
	if limit <= 0 {
		limit = 5
	}
	params := url.Values{}
	params.Set("action", "query")
	params.Set("list", "random")
	params.Set("rnnamespace", "0")
	params.Set("rnlimit", fmt.Sprint(limit))
	params.Set("format", "json")

	rawURL := c.apiURL() + "?" + params.Encode()
	var resp mwRandomResp
	if err := c.getJSON(ctx, rawURL, &resp); err != nil {
		return nil, err
	}

	out := make([]Theorem, 0, len(resp.Query.Random))
	for _, r := range resp.Query.Random {
		out = append(out, Theorem{
			Title:   r.Title,
			URL:     c.pageURL(r.Title),
			Snippet: "",
		})
	}
	return out, nil
}

// ─── List ────────────────────────────────────────────────────────────────────

type mwCategoryResp struct {
	Query struct {
		CategoryMembers []mwCategoryMember `json:"categorymembers"`
	} `json:"query"`
}

type mwCategoryMember struct {
	Title string `json:"title"`
}

// ListCategory returns up to limit pages in the given category.
func (c *Client) ListCategory(ctx context.Context, category string, limit int) ([]CategoryPage, error) {
	if limit <= 0 {
		limit = 20
	}
	cmTitle := category
	if !strings.HasPrefix(strings.ToLower(cmTitle), "category:") {
		cmTitle = "Category:" + cmTitle
	}

	params := url.Values{}
	params.Set("action", "query")
	params.Set("list", "categorymembers")
	params.Set("cmtitle", cmTitle)
	params.Set("cmlimit", fmt.Sprint(limit))
	params.Set("format", "json")

	rawURL := c.apiURL() + "?" + params.Encode()
	var resp mwCategoryResp
	if err := c.getJSON(ctx, rawURL, &resp); err != nil {
		return nil, err
	}

	out := make([]CategoryPage, 0, len(resp.Query.CategoryMembers))
	for _, m := range resp.Query.CategoryMembers {
		out = append(out, CategoryPage{
			Title: m.Title,
			URL:   c.pageURL(m.Title),
		})
	}
	return out, nil
}

// ─── Wiki markup stripping ───────────────────────────────────────────────────

var (
	reCurly   = regexp.MustCompile(`\{\{[^}]*\}\}`)
	reLinkFmt = regexp.MustCompile(`\[\[[^\]]*\|([^\]]*)\]\]`)
	reLinkBare = regexp.MustCompile(`\[\[([^\]]*)\]\]`)
	reHeading  = regexp.MustCompile(`(?m)^=+\s*(.*?)\s*=+\s*$`)
	reHTMLTag  = regexp.MustCompile(`<[^>]+>`)
	reMultiNL  = regexp.MustCompile(`\n{3,}`)
)

// stripWikiMarkup removes MediaWiki markup and returns readable plain text.
func stripWikiMarkup(s string) string {
	// Remove template invocations: {{...}}
	for reCurly.MatchString(s) {
		s = reCurly.ReplaceAllString(s, "")
	}
	// [[link|display text]] → display text
	s = reLinkFmt.ReplaceAllString(s, "$1")
	// [[link]] → link
	s = reLinkBare.ReplaceAllString(s, "$1")
	// == Heading == → Heading
	s = reHeading.ReplaceAllString(s, "$1")
	// Remove HTML tags
	s = reHTMLTag.ReplaceAllString(s, "")
	// Collapse HTML entities
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	// Collapse excessive blank lines
	s = reMultiNL.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// extractSnippet returns the first n characters of the plaintext version of content.
func extractSnippet(content string, n int) string {
	plain := stripWikiMarkup(content)
	plain = strings.TrimSpace(plain)
	rs := []rune(plain)
	if len(rs) <= n {
		return plain
	}
	return string(rs[:n]) + "..."
}
