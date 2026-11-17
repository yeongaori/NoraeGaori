package youtube_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"noraegaori/internal/messages"
	"noraegaori/internal/youtube"
	"noraegaori/tests/testutil"
)

const restrictedVideoURL = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

type innertubeTransport func(*http.Request) (*http.Response, error)

func (transport innertubeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func stubInnertube(t *testing.T, status, reason string) {
	t.Helper()

	body := fmt.Sprintf(`{"playabilityStatus":{"status":%q,"reason":%q}}`, status, reason)
	client := &http.Client{Transport: innertubeTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	initialized := &sync.Once{}
	initialized.Do(func() {})
	testutil.Swap(t, youtube.HookInnertubeOnce, initialized)
	testutil.Swap(t, youtube.HookInnertubeClient, youtube.HookCreateInnertubeClient(&youtube.HookInnertubeClientFields{ClientName: "TEST", HTTPClient: client}))
}

func useEachLocale(t *testing.T, check func(lang string)) {
	t.Helper()

	t.Cleanup(func() {
		if err := messages.LoadLocale("en"); err != nil {
			t.Errorf("failed to restore the English locale: %v", err)
		}
	})
	for _, lang := range messages.AvailableLocales() {
		if err := messages.LoadLocale(lang); err != nil {
			t.Fatalf("failed to load the %s locale: %v", lang, err)
		}
		youtube.HookResetCache()
		check(lang)
	}
}

func TestParseRestriction(t *testing.T) {
	cases := []struct {
		status string
		reason string
		msgs   []string
		want   youtube.Restriction
	}{
		{status: "LOGIN_REQUIRED", reason: "This video is private", want: youtube.RestrictionPrivateVideo},
		{status: "LOGIN_REQUIRED", msgs: []string{"This is a private video"}, want: youtube.RestrictionPrivateVideo},
		{status: "LOGIN_REQUIRED", reason: "Sign in to confirm your age", want: youtube.RestrictionAgeRestricted},
		{status: "UNPLAYABLE", reason: "The uploader has not made this video available in your country", want: youtube.RestrictionGeoRestricted},
		{status: "UNPLAYABLE", reason: "This video is not available in your region", want: youtube.RestrictionGeoRestricted},
		{status: "UNPLAYABLE", reason: "Join this channel to get access to members-only content", want: youtube.RestrictionMembersOnly},
		{status: "UNPLAYABLE", reason: "This video requires payment to watch. YouTube Premium", want: youtube.RestrictionPremiumOnly},
		{status: "UNPLAYABLE", reason: "This video contains content blocked on copyright grounds", want: youtube.RestrictionCopyright},
		{status: "UNPLAYABLE", reason: "Playback on other websites has been disabled", want: youtube.RestrictionUnplayable},
		{status: "UNPLAYABLE", want: youtube.RestrictionUnplayable},
		{status: "ERROR", reason: "This video is unavailable", want: youtube.RestrictionDeletedVideo},
		{status: "ERROR", reason: "Something went wrong", want: youtube.RestrictionUnavailable},
		{status: "ERROR", want: youtube.RestrictionUnavailable},
		{status: "CONTENT_CHECK_REQUIRED", want: youtube.RestrictionContentCheck},
		{status: "LIVE_STREAM_OFFLINE", reason: "This live event will begin soon", want: youtube.RestrictionUnplayable},
		{status: "LIVE_STREAM_OFFLINE", want: youtube.RestrictionUnavailable},
	}

	for i := range cases {
		testCase := &cases[i]
		t.Run(testCase.status+" "+testCase.reason, func(t *testing.T) {
			if got := youtube.HookParseRestriction(testCase.status, testCase.reason, testCase.msgs); got != testCase.want {
				t.Errorf("Restriction = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestRestrictionMessages(t *testing.T) {
	useEachLocale(t, func(lang string) {
		for restriction := youtube.RestrictionNone; restriction <= youtube.RestrictionContentCheck; restriction++ {
			for _, reason := range []string{"", "Something went wrong"} {
				if got := restriction.Message("", reason); got == "" || strings.Contains(got, "%!") {
					t.Errorf("%s: Message(%v) = %q, want non-empty text", lang, restriction, got)
				}
			}
		}
		for _, restriction := range []youtube.Restriction{youtube.RestrictionUnplayable, youtube.RestrictionUnavailable} {
			if got := restriction.Message("", "Something went wrong"); !strings.Contains(got, "Something went wrong") {
				t.Errorf("%s: Message(%v, %q) = %q, want the reason included", lang, restriction, "Something went wrong", got)
			}
		}
	})
}

func TestGetVideoInfoRechecksAgeGate(t *testing.T) {
	resetAvailability(t)
	stubInnertube(t, "LOGIN_REQUIRED", "Sign in to confirm your age")
	hasRechecked := false
	*youtube.HookCheckAvailabilityViaInnertube = func(guildID, url string) (*youtube.AvailabilityResult, error) {
		hasRechecked = true
		return &youtube.AvailabilityResult{Available: false}, nil
	}

	useEachLocale(t, func(lang string) {
		hasRechecked = false

		_, _ = youtube.GetVideoInfo("", restrictedVideoURL, "requester", "requesterID")

		if !hasRechecked {
			t.Error("the age-gated video was not re-checked")
		}
	})
}

func TestCheckAvailabilitySkipsFallback(t *testing.T) {
	resetAvailability(t)
	stubInnertube(t, "ERROR", "This video is unavailable")

	useEachLocale(t, func(lang string) {
		_, _, err := youtube.CheckAvailability(restrictedVideoURL)

		var videoErr *youtube.VideoError
		if !errors.As(err, &videoErr) {
			t.Fatalf("err = %v, want a VideoError", err)
		}
		if videoErr.Restriction != youtube.RestrictionDeletedVideo {
			t.Errorf("Restriction = %v, want %v", videoErr.Restriction, youtube.RestrictionDeletedVideo)
		}
	})
}

func TestGetVideoInfoKeepsRestriction(t *testing.T) {
	resetAvailability(t)
	stubInnertube(t, "UNPLAYABLE", "Join this channel to get access to members-only content like this video, and other exclusive perks.")

	useEachLocale(t, func(lang string) {
		_, err := youtube.GetVideoInfo("", restrictedVideoURL, "requester", "requesterID")

		var videoErr *youtube.VideoError
		if !errors.As(err, &videoErr) {
			t.Fatalf("err = %v, want a VideoError", err)
		}
		if want := messages.T().YouTube.ErrorMembersOnly; videoErr.Message != want {
			t.Errorf("Message = %q, want %q", videoErr.Message, want)
		}
	})
}
