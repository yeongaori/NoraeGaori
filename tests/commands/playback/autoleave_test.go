package playback_test

import (
	"fmt"
	"testing"

	"noraegaori/internal/commands/playback"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/queuetest"
)

func wantQueueCleared(t *testing.T) {
	t.Helper()
	if q, err := queue.GetQueue(commandtest.GuildID, true); err != nil || q != nil {
		t.Errorf("the queue is %+v (err %v), want it stopped and cleared", q, err)
	}
}

func TestStopFollowsAutoLeave(t *testing.T) {
	cases := make([]commandtest.Case, 0, 2)
	for _, isEnabled := range []bool{true, false} {
		prepare, checkPlayer := commandtest.AutoLeave(isEnabled)
		cases = append(cases, commandtest.Case{
			Name:       fmt.Sprintf("auto-leave %t", isEnabled),
			Songs:      queuetest.SongsBy(commandtest.CallerID, 1),
			VoiceUsers: []string{commandtest.CallerID},
			Prepare:    prepare,
			WantText:   func(locale *messages.Locale) string { return locale.Music.StopSuccessDesc },
			Check: func(t *testing.T, reply map[string]any) {
				checkPlayer(t, reply)
				wantQueueCleared(t)
			},
		})
	}

	commandtest.Run(t, "stop", playback.HandleStop, cases)
}

func TestSkippingTheLastSongFollowsAutoLeave(t *testing.T) {
	song := queuetest.Song("Last song", commandtest.CallerID)
	link := messages.FormatMaskedLink(song.Title, song.URL)

	cases := make([]commandtest.Case, 0, 2)
	for _, isEnabled := range []bool{true, false} {
		prepare, checkPlayer := commandtest.AutoLeave(isEnabled)
		cases = append(cases, commandtest.Case{
			Name:       fmt.Sprintf("auto-leave %t", isEnabled),
			Songs:      []*queue.Song{song},
			VoiceUsers: []string{commandtest.CallerID},
			Prepare:    prepare,
			WantText: func(locale *messages.Locale) string {
				if isEnabled {
					return fmt.Sprintf(locale.Music.PlaybackEndedSkip, link)
				}
				return fmt.Sprintf(locale.Music.PlaybackEndedSkipStay, link)
			},
			Check: func(t *testing.T, reply map[string]any) {
				checkPlayer(t, reply)
				wantQueueCleared(t)
			},
		})
	}

	commandtest.Run(t, "skip", playback.HandleSkip, cases)
}
