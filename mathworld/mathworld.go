// Package mathworld is the library behind the mw command line:
// the HTTP client, request shaping, and the typed data models for
// Wolfram MathWorld (https://mathworld.wolfram.com).
//
// The Client sets a real User-Agent, paces requests so a busy session stays
// polite, and retries the transient failures (429 and 5xx) that any public
// site may throw under load.
package mathworld

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultUserAgent identifies the client to MathWorld.
const DefaultUserAgent = "mw/dev (+https://github.com/tamnd/mathworld-cli)"

// Config holds constructor parameters for Client.
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
		BaseURL:   "https://mathworld.wolfram.com",
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Retries:   5,
		Timeout:   30 * time.Second,
	}
}

// Article is one search result from MathWorld.
type Article struct {
	Rank     int    `json:"rank"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Summary  string `json:"summary"`
	URL      string `json:"url"`
}

// Client talks to MathWorld over HTTP.
type Client struct {
	http      *http.Client
	baseURL   string
	userAgent string
	rate      time.Duration
	retries   int
	last      time.Time
}

// NewClient returns a Client built from cfg.
func NewClient(cfg Config) *Client {
	base := strings.TrimRight(cfg.BaseURL, "/")
	return &Client{
		http:      &http.Client{Timeout: cfg.Timeout},
		baseURL:   base,
		userAgent: cfg.UserAgent,
		rate:      cfg.Rate,
		retries:   cfg.Retries,
	}
}

// Search searches MathWorld for query and returns up to limit articles.
// It fetches GET /search/?query=QUERY and parses the result HTML.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Article, error) {
	if limit <= 0 {
		limit = 10
	}
	rawURL := c.baseURL + "/search/?query=" + url.QueryEscape(query)
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	return parseSearchResults(string(body), limit), nil
}

// parseSearchResults extracts Article records from the MathWorld search HTML.
// It uses the pattern of alternating search-result-title / search-result-summary divs.
//
// The live HTML splits the class attribute across lines:
//
//	<div class=
//	    "search-result-title"
//	><a href="...">TITLE</a></div>
//
// so we search for the quoted string "search-result-title" anywhere in the HTML
// rather than a full class="..." match.
func parseSearchResults(html string, limit int) []Article {
	var out []Article
	rank := 1

	rest := html
	for rank <= limit {
		// Find next title div — MathWorld wraps the class value on a new line,
		// so just match the quoted class string itself.
		titleMarker := `"search-result-title"`
		titleStart := strings.Index(rest, titleMarker)
		if titleStart < 0 {
			break
		}
		// Advance past the marker and then past the closing > of the div tag.
		rest = rest[titleStart+len(titleMarker):]
		closeBracket := strings.Index(rest, ">")
		if closeBracket < 0 {
			break
		}
		rest = rest[closeBracket+1:]

		// Now we are right at the <a href="..."> opener.
		hrefIdx := strings.Index(rest, `href="`)
		if hrefIdx < 0 {
			break
		}
		hrefIdx += len(`href="`)
		hrefEnd := strings.Index(rest[hrefIdx:], `"`)
		if hrefEnd < 0 {
			break
		}
		articleURL := rest[hrefIdx : hrefIdx+hrefEnd]

		// Move past the href value and find the end of the opening <a> tag.
		afterHref := hrefIdx + hrefEnd
		anchorClose := strings.Index(rest[afterHref:], `>`)
		if anchorClose < 0 {
			break
		}
		afterOpen := afterHref + anchorClose + 1
		anchorEnd := strings.Index(rest[afterOpen:], `</a>`)
		if anchorEnd < 0 {
			break
		}
		rawTitle := rest[afterOpen : afterOpen+anchorEnd]
		title := stripTags(rawTitle)
		// Remove " -- from Wolfram MathWorld" / " -- from Wolfram ..." suffix
		if idx := strings.Index(title, " -- from Wolfram"); idx >= 0 {
			title = strings.TrimSpace(title[:idx])
		}

		// Advance past this title block.
		rest = rest[afterOpen+anchorEnd:]

		// Find the very next summary div.
		summaryMarker := `"search-result-summary"`
		summaryIdx := strings.Index(rest, summaryMarker)
		if summaryIdx < 0 {
			break
		}
		rest = rest[summaryIdx+len(summaryMarker):]

		// Move past the closing > of the summary div tag.
		summaryBracket := strings.Index(rest, ">")
		if summaryBracket < 0 {
			break
		}
		summaryContent := rest[summaryBracket+1:]

		// Extract until </div>.
		summaryEnd := strings.Index(summaryContent, "</div>")
		if summaryEnd < 0 {
			break
		}
		rawSummary := summaryContent[:summaryEnd]
		summary := strings.TrimSpace(stripTags(rawSummary))

		// Category derived from the article URL slug.
		category := categoryFromURL(articleURL)

		out = append(out, Article{
			Rank:     rank,
			Title:    title,
			Category: category,
			Summary:  summary,
			URL:      articleURL,
		})
		rank++
		rest = summaryContent[summaryEnd:]
	}
	return out
}

// categoryFromURL derives a human-readable category from a MathWorld article URL.
// e.g. "https://mathworld.wolfram.com/PrimeNumber.html" -> "Number Theory"
// Since MathWorld URLs don't include the category path, we use the article
// slug itself as the category hint (space-separated from CamelCase).
func categoryFromURL(rawURL string) string {
	// Pull the last path segment without extension
	path := rawURL
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		path = path[idx+1:]
	}
	if dot := strings.LastIndex(path, "."); dot >= 0 {
		path = path[:dot]
	}
	// Split CamelCase into words
	if path == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range path {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// stripTags removes HTML tags and decodes common HTML entities.
func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	out := b.String()
	out = strings.ReplaceAll(out, "&amp;", "&")
	out = strings.ReplaceAll(out, "&lt;", "<")
	out = strings.ReplaceAll(out, "&gt;", ">")
	out = strings.ReplaceAll(out, "&quot;", `"`)
	out = strings.ReplaceAll(out, "&#39;", "'")
	out = strings.ReplaceAll(out, "&apos;", "'")
	out = strings.ReplaceAll(out, "&nbsp;", " ")
	return strings.TrimSpace(out)
}

// ─── HTTP internals ──────────────────────────────────────────────────────────

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
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

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
