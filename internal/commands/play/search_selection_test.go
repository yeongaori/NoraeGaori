package play

import "testing"

func TestParseSearchSelectionAcceptsValidValues(t *testing.T) {
	cases := map[string]struct {
		searchID string
		index    int
	}{
		"msg123:0":  {"msg123", 0},
		"msg123:4":  {"msg123", 4},
		"abc:11":    {"abc", 11},
		"msg123:-1": {"msg123", -1},
	}

	for value, want := range cases {
		searchID, index, err := parseSearchSelection(value)
		if err != nil {
			t.Errorf("parseSearchSelection(%q) returned %v, want nil", value, err)
			continue
		}
		if searchID != want.searchID || index != want.index {
			t.Errorf("parseSearchSelection(%q) = (%q, %d), want (%q, %d)", value, searchID, index, want.searchID, want.index)
		}
	}
}

func TestParseSearchSelectionRejectsMalformedValues(t *testing.T) {
	cases := []string{
		"msg123",
		"msg123:",
		"msg123:abc",
		"msg123:1:2",
		"msg123: 1",
		"msg123:1.5",
		"",
	}

	for _, value := range cases {
		searchID, index, err := parseSearchSelection(value)
		if err == nil {
			t.Errorf("parseSearchSelection(%q) returned (%q, %d, nil), want a rejection", value, searchID, index)
		}
		if index != 0 || searchID != "" {
			t.Errorf("parseSearchSelection(%q) returned (%q, %d) alongside its error, want zero values", value, searchID, index)
		}
	}
}

func TestParseSearchSelectionDoesNotDefaultToFirstResult(t *testing.T) {
	_, index, err := parseSearchSelection("msg123:notanumber")
	if err == nil {
		t.Fatal("a non-numeric index was accepted, so the caller would queue result 0")
	}
	if index != 0 {
		t.Errorf("got index %d alongside the error, want 0", index)
	}
}
