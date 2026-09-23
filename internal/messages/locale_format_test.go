package messages

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

var formatVerbPattern = regexp.MustCompile(`%(?:\[(\d+)\])?[-+# 0]*\d*(?:\.\d+)?([a-zA-Z%])`)

func formatVerbs(format string) map[int]string {
	verbs := map[int]string{}
	next := 1
	for _, match := range formatVerbPattern.FindAllStringSubmatch(format, -1) {
		if match[2] == "%" {
			continue
		}
		if match[1] != "" {
			next, _ = strconv.Atoi(match[1])
		}
		verbs[next] = match[2]
		next++
	}
	return verbs
}

func flattenLocale(prefix string, value any, into map[string]string) {
	switch typed := value.(type) {
	case string:
		into[prefix] = typed
	case map[string]any:
		for key, child := range typed {
			flattenLocale(prefix+"."+key, child, into)
		}
	case []any:
		for index, child := range typed {
			flattenLocale(prefix+"."+strconv.Itoa(index), child, into)
		}
	}
}

func localeStrings(t *testing.T, code string) map[string]string {
	t.Helper()

	data, err := readLocaleFile(filepath.Join(localesDir(), code+".json"))
	if err != nil {
		t.Fatalf("failed to read locale %s: %v", code, err)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("locale %s has invalid JSON: %v", code, err)
	}
	flattened := map[string]string{}
	flattenLocale("", decoded, flattened)
	return flattened
}

func TestFormatVerbsReadPositionsAndIndexes(t *testing.T) {
	for format, want := range map[string]map[int]string{
		"%d songs by %s":           {1: "d", 2: "s"},
		"%[2]d songs by %[1]s":     {1: "s", 2: "d"},
		"%.1f%% at %5s":            {1: "f", 2: "s"},
		"range %[2]d-%[3]d: %[1]d": {1: "d", 2: "d", 3: "d"},
		"no verbs, 100%%":          {},
	} {
		if got := formatVerbs(format); !maps.Equal(got, want) {
			t.Errorf("formatVerbs(%q) = %v, want %v", format, got, want)
		}
	}
}

func TestEveryLocaleUsesTheEnglishFormatVerbs(t *testing.T) {
	english := localeStrings(t, "en")

	for _, code := range AvailableLocales() {
		if code == "en" {
			continue
		}
		for key, translated := range localeStrings(t, code) {
			original, isShared := english[key]
			if !isShared {
				continue
			}
			if want, got := formatVerbs(original), formatVerbs(translated); !maps.Equal(got, want) {
				t.Errorf("%s%s uses verbs %v, English uses %v", code, key, got, want)
			}
		}
	}
}
