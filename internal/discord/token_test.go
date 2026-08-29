package discord

import "testing"

func TestComponentTokensAreUnique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for range 1000 {
		token := NewComponentToken()
		if token == "" {
			t.Fatal("an empty component token was generated")
		}
		if seen[token] {
			t.Fatalf("component token %q was generated twice", token)
		}
		seen[token] = true
	}
}
