package locales_test

import (
	"encoding/json"
	"testing"

	"noraegaori/locales"
)

func TestEnglishLocaleIsEmbedded(t *testing.T) {
	if len(locales.EnglishLocale) == 0 {
		t.Fatal("EnglishLocale is empty, so the //go:embed directive above it was lost")
	}
}

func TestEnglishLocaleParses(t *testing.T) {
	var locale map[string]any
	if err := json.Unmarshal(locales.EnglishLocale, &locale); err != nil {
		t.Fatalf("the embedded locale is not valid JSON: %v", err)
	}

	rpc, ok := locale["rpc"].(map[string]any)
	if !ok {
		t.Fatal("the embedded locale has no rpc section")
	}

	activity, ok := rpc["activity_default_1"].(string)
	if !ok || activity == "" {
		t.Errorf("rpc.activity_default_1 is %v, want a non-empty string", rpc["activity_default_1"])
	}
}
