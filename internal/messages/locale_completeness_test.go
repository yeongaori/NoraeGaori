package messages

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func isListIndex(key string) bool {
	_, err := strconv.Atoi(key[strings.LastIndex(key, ".")+1:])
	return err == nil
}

func TestEveryLocaleTranslatesEveryEnglishKey(t *testing.T) {
	english := localeStrings(t, "en")

	for _, code := range AvailableLocales() {
		if code == "en" {
			continue
		}
		translated := localeStrings(t, code)
		for key := range english {
			if _, isTranslated := translated[key]; !isTranslated && !isListIndex(key) {
				t.Errorf("%s is missing %s", code, key)
			}
		}
	}
}

func emptyLocaleStrings(path string, value reflect.Value, into *[]string) {
	switch value.Kind() {
	case reflect.String:
		if value.String() == "" {
			*into = append(*into, path)
		}
	case reflect.Struct:
		for index := range value.NumField() {
			emptyLocaleStrings(path+"."+value.Type().Field(index).Name, value.Field(index), into)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			emptyLocaleStrings(path+"["+key.String()+"]", value.MapIndex(key), into)
		}
	}
}

func TestStayingTextsNeverSayTheBotLeaves(t *testing.T) {
	leavingWords := map[string][]string{
		"en": {"leav", "left"},
		"ko": {"나갑", "나갔"},
	}

	for code, words := range leavingWords {
		locale, err := buildLocale(code)
		if err != nil {
			t.Fatalf("failed to build locale %s: %v", code, err)
		}

		for name, text := range map[string]string{
			"descriptions.paused_stay":       locale.Descriptions.PausedStay,
			"music.playback_ended_skip_stay": locale.Music.PlaybackEndedSkipStay,
			"music.force_skipped_ended_stay": locale.Music.ForceSkippedEndedStay,
			"player.queue_finished_desc":     locale.Player.QueueFinishedDesc,
			"player.staying_footer":          locale.Player.StayingFooter,
		} {
			for _, word := range words {
				if strings.Contains(strings.ToLower(text), word) {
					t.Errorf("%s %s = %q mentions leaving", code, name, text)
				}
			}
		}
	}
}

func TestEveryLocaleStringIsFilled(t *testing.T) {
	for _, code := range AvailableLocales() {
		locale, err := buildLocale(code)
		if err != nil {
			t.Fatalf("failed to build locale %s: %v", code, err)
		}

		var empty []string
		emptyLocaleStrings(code, reflect.ValueOf(*locale), &empty)
		for _, path := range empty {
			t.Errorf("%s is empty", path)
		}
	}
}
