package mathworld_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tamnd/mathworld-cli/mathworld"
)

const searchHTML = `<!doctype html>
<html>
<body>
<div class="search-results">
<div class="search-result-title"><a href="https://mathworld.wolfram.com/PrimeNumber.html">Prime Number -- from Wolfram MathWorld</a></div>
<div class="search-result-summary">A prime number (or prime integer) is a positive integer p&gt;1 that has no positive integer divisors other than 1 and p itself. More...</div>
<div class="search-result-title"><a href="https://mathworld.wolfram.com/Prime.html">Prime -- from Wolfram MathWorld</a></div>
<div class="search-result-summary">A symbol used to distinguish one quantity x&apos; (&quot;x prime&quot;) from another related x. Prime marks are most commonly used to denote transformed coordinates.</div>
<div class="search-result-title"><a href="https://mathworld.wolfram.com/PrimeNumberTheorem.html">Prime Number Theorem -- from Wolfram MathWorld</a></div>
<div class="search-result-summary">The prime number theorem gives an asymptotic form for the prime counting function pi(n), which counts the number of primes less than some integer n.</div>
</div>
</body>
</html>`

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "search") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(searchHTML))
	}))
	defer srv.Close()

	cfg := mathworld.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	c := mathworld.NewClient(cfg)

	articles, err := c.Search(context.Background(), "prime number", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 3 {
		t.Fatalf("got %d articles, want 3", len(articles))
	}

	a := articles[0]
	if a.Rank != 1 {
		t.Errorf("Rank = %d, want 1", a.Rank)
	}
	if a.Title != "Prime Number" {
		t.Errorf("Title = %q, want %q", a.Title, "Prime Number")
	}
	if !strings.Contains(a.URL, "PrimeNumber.html") {
		t.Errorf("URL = %q, want contains PrimeNumber.html", a.URL)
	}
	if !strings.Contains(a.Summary, "prime number") {
		t.Errorf("Summary = %q, want it to contain 'prime number'", a.Summary)
	}
	// HTML entities should be decoded
	if strings.Contains(a.Summary, "&gt;") {
		t.Error("Summary still contains raw HTML entity &gt;")
	}

	// Second result
	b := articles[1]
	if b.Rank != 2 {
		t.Errorf("second result Rank = %d, want 2", b.Rank)
	}
	if b.Title != "Prime" {
		t.Errorf("second result Title = %q, want %q", b.Title, "Prime")
	}
}

func TestSearchLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(searchHTML))
	}))
	defer srv.Close()

	cfg := mathworld.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	c := mathworld.NewClient(cfg)

	articles, err := c.Search(context.Background(), "prime", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 2 {
		t.Fatalf("got %d articles, want 2 (limit=2)", len(articles))
	}
}

func TestSearchRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(searchHTML))
	}))
	defer srv.Close()

	cfg := mathworld.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := mathworld.NewClient(cfg)

	articles, err := c.Search(context.Background(), "prime", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) == 0 {
		t.Error("expected articles after retries, got none")
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := mathworld.DefaultConfig()
	if cfg.BaseURL != "https://mathworld.wolfram.com" {
		t.Errorf("BaseURL = %q, want https://mathworld.wolfram.com", cfg.BaseURL)
	}
	if cfg.UserAgent == "" {
		t.Error("UserAgent must not be empty")
	}
	if cfg.Retries <= 0 {
		t.Error("Retries must be positive")
	}
}
