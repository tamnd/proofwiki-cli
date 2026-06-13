package proofwiki

// SearchResult is the record emitted for search results.
type SearchResult struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	URL     string `json:"url"`
}

// Theorem is the record emitted for a single theorem page.
type Theorem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Content string `json:"content,omitempty"`
}

// CategoryPage is the record emitted when listing a category.
type CategoryPage struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}
