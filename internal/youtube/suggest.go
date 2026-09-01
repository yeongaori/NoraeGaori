package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	suggestQueriesEndpoint = "https://suggestqueries-clients6.youtube.com/complete/search"
	suggestUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	suggestMaxResponseSize = 64 << 10
)

var suggestHTTPClient = &http.Client{Timeout: 3 * time.Second}

func SuggestTerms(ctx context.Context, query string, language string) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	if language == "" {
		language = "en"
	}

	values := url.Values{}
	values.Set("client", "firefox")
	values.Set("ds", "yt")
	values.Set("hl", language)
	values.Set("q", query)
	requestURL := suggestQueriesEndpoint + "?" + values.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build suggest request: %w", err)
	}
	request.Header.Set("User-Agent", suggestUserAgent)
	request.Header.Set("Accept-Language", language)

	response, err := suggestHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("suggest request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("suggest request failed with status %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, suggestMaxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read suggest response: %w", err)
	}

	var payload []json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to decode suggest response: %w", err)
	}

	if len(payload) < 2 {
		return nil, fmt.Errorf("unexpected suggest payload shape")
	}

	var rawTerms []string
	if err := json.Unmarshal(payload[1], &rawTerms); err != nil {
		return nil, fmt.Errorf("failed to decode suggest terms: %w", err)
	}

	seen := make(map[string]bool, len(rawTerms))
	terms := make([]string, 0, len(rawTerms))
	for _, term := range rawTerms {
		term = strings.TrimSpace(term)
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}

	return terms, nil
}
