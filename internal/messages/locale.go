package messages

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"noraegaori/locales"
)

var guildLangResolver func(guildID string) (string, error)

func SetGuildLangResolver(fn func(guildID string) (string, error)) {
	guildLangResolver = fn
}

var (
	localeCacheWrite sync.Mutex
	localeCache      atomic.Pointer[map[string]*Locale]
)

type localeState struct {
	locale *Locale
	lang   string
}

var activeLocale atomic.Pointer[localeState]

func init() {
	activeLocale.Store(&localeState{locale: &Locale{}, lang: "en"})
	localeCache.Store(&map[string]*Locale{})
}

func setActiveLocale(loc *Locale, lang string) {
	activeLocale.Store(&localeState{locale: loc, lang: lang})
}

func T(guildID ...string) *Locale {
	active := activeLocale.Load()
	if len(guildID) == 0 || guildID[0] == "" || guildLangResolver == nil {
		return active.locale
	}
	lang, err := guildLangResolver(guildID[0])
	if err != nil || lang == "" || lang == active.lang {
		return active.locale
	}
	if loc := getCachedLocale(lang); loc != nil {
		return loc
	}
	return active.locale
}

func Lang(guildID ...string) string {
	active := activeLocale.Load()
	if len(guildID) == 0 || guildID[0] == "" || guildLangResolver == nil {
		return active.lang
	}
	lang, err := guildLangResolver(guildID[0])
	if err != nil || lang == "" {
		return active.lang
	}
	return lang
}

func ForLang(lang string) *Locale {
	if loc := getCachedLocale(lang); loc != nil {
		return loc
	}
	return activeLocale.Load().locale
}

func AvailableLocales() []string {
	dir := localesDir()
	entries, err := os.ReadDir(dir)
	seen := map[string]struct{}{"en": {}}
	out := []string{"en"}
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			code := strings.TrimSuffix(name, ".json")
			if _, ok := seen[code]; ok {
				continue
			}
			seen[code] = struct{}{}
			out = append(out, code)
		}
	}
	return out
}

func localesDir() string {
	const dir = "locales"
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", dir)
}

func getCachedLocale(lang string) *Locale {
	loc, ok := (*localeCache.Load())[lang]
	if ok {
		return loc
	}
	loc, err := createLocale(lang)
	if err != nil {
		return nil
	}
	storeCachedLocale(lang, loc)
	return loc
}

func createLocale(lang string) (*Locale, error) {
	enData, err := readLocaleFile(filepath.Join(localesDir(), "en.json"))
	if err != nil {
		enData = locales.EnglishLocale
	}
	var base Locale
	if err = json.Unmarshal(enData, &base); err != nil {
		return nil, fmt.Errorf("failed to parse English fallback locale: %w", err)
	}
	if lang == "en" {
		return &base, nil
	}
	langData, err := readLocaleFile(filepath.Join(localesDir(), lang+".json"))
	if err != nil {
		return nil, err
	}
	var overlay Locale
	if err := json.Unmarshal(langData, &overlay); err != nil {
		return nil, fmt.Errorf("locale %q has invalid JSON: %w", lang, err)
	}
	mergeLocale(&base, &overlay)
	return &base, nil
}

func InvalidateLocales() {
	localeCacheWrite.Lock()
	localeCache.Store(&map[string]*Locale{})
	localeCacheWrite.Unlock()
}

func LoadLocale(lang string) error {
	loc, err := createLocale(lang)
	if err != nil {

		var base Locale
		if jerr := json.Unmarshal(locales.EnglishLocale, &base); jerr != nil {
			return fmt.Errorf("failed to parse embedded English fallback: %w", jerr)
		}
		setActiveLocale(&base, lang)

		storeCachedLocale("en", &base)
		return fmt.Errorf("failed to load locale %q, falling back to English: %w", lang, err)
	}

	setActiveLocale(loc, lang)

	storeCachedLocale(lang, loc)
	return nil
}

func readLocaleFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path) // #nosec G304
	if err == nil {
		return data, nil
	}

	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	altPath := filepath.Join(dir, "..", "..", path)
	return os.ReadFile(altPath) // #nosec G304
}

func mergeLocale(base, overlay *Locale) {
	mergeStruct(reflect.ValueOf(base).Elem(), reflect.ValueOf(overlay).Elem())
}

func mergeStruct(base, overlay reflect.Value) {
	for i := 0; i < base.NumField(); i++ {
		baseField := base.Field(i)
		overlayField := overlay.Field(i)

		switch baseField.Kind() {
		case reflect.String:
			if overlayField.String() != "" {
				baseField.SetString(overlayField.String())
			}
		case reflect.Struct:
			mergeStruct(baseField, overlayField)
		case reflect.Map:
			mergeMap(baseField, overlayField)
		case reflect.Slice:
			if overlayField.Len() > 0 {
				baseField.Set(overlayField)
			}
		}
	}
}

func mergeMap(base, overlay reflect.Value) {
	if overlay.IsNil() {
		return
	}
	if base.IsNil() {
		base.Set(overlay)
		return
	}
	for _, key := range overlay.MapKeys() {
		overlayVal := overlay.MapIndex(key)
		baseVal := base.MapIndex(key)

		mapElemType := base.Type().Elem()
		if mapElemType.Kind() == reflect.Struct {

			merged := reflect.New(mapElemType).Elem()
			if baseVal.IsValid() {
				merged.Set(baseVal)
			}
			mergeStruct(merged, overlayVal)
			base.SetMapIndex(key, merged)
		} else {

			base.SetMapIndex(key, overlayVal)
		}
	}
}

func storeCachedLocale(lang string, loc *Locale) {
	localeCacheWrite.Lock()
	defer localeCacheWrite.Unlock()

	current := *localeCache.Load()
	rebuilt := make(map[string]*Locale, len(current)+1)
	for key, value := range current {
		rebuilt[key] = value
	}
	rebuilt[lang] = loc
	localeCache.Store(&rebuilt)
}
