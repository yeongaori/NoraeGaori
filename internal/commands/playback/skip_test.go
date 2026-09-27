package playback

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/vote"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/queuetest"
)

func TestSkipResultEmbedSwitchesOnAnEmptiedQueue(t *testing.T) {
	song := &queue.Song{Title: "Song", URL: "https://example.com/song", Thumbnail: "https://example.com/thumb.jpg"}

	skipped := skipResultEmbed("g1", song, false, false)
	ended := skipResultEmbed("g1", song, true, false)

	if skipped.Title == ended.Title {
		t.Errorf("both embeds titled %q, want the queue-ended variant to differ", skipped.Title)
	}
	if ended.Title != messages.T("g1").Music.PlaybackEndedTitle {
		t.Errorf("queue-ended title = %q, want the playback-ended title", ended.Title)
	}
	if skipped.Thumbnail == nil || skipped.Thumbnail.URL != song.Thumbnail {
		t.Error("the skip embed lost the song thumbnail")
	}
}

func TestSkipResultEmbedOnlyMentionsLeavingWhenTheBotLeaves(t *testing.T) {
	song := &queue.Song{Title: "Song", URL: "https://example.com/song"}
	link := messages.FormatMaskedLink(song.Title, song.URL)
	music := messages.T("g1").Music

	if got := skipResultEmbed("g1", song, true, false).Description; got != fmt.Sprintf(music.PlaybackEndedSkip, link) {
		t.Errorf("leaving description = %q, want the leaving text", got)
	}
	if got := skipResultEmbed("g1", song, true, true).Description; got != fmt.Sprintf(music.PlaybackEndedSkipStay, link) {
		t.Errorf("staying description = %q, want the staying text", got)
	}
	if got := skipResultEmbed("g1", song, false, true).Description; got != fmt.Sprintf(messages.T("g1").Descriptions.Skipped, link) {
		t.Errorf("mid-queue description = %q, want the plain skip text", got)
	}
}

func TestSkipStaysOnlyWhenTheQueueEndedWithAutoLeaveOff(t *testing.T) {
	dbtest.Setup(t)

	if isStayingAfterSkip("g1", true) {
		t.Error("an emptied queue stays by default, want it to leave")
	}

	if err := queue.SetAutoLeave("g1", false); err != nil {
		t.Fatalf("failed to turn auto-leave off: %v", err)
	}
	if !isStayingAfterSkip("g1", true) {
		t.Error("an emptied queue leaves with auto-leave off, want it to stay")
	}
	if isStayingAfterSkip("g1", false) {
		t.Error("a mid-queue skip reports staying, want the plain skip")
	}
}

func TestSkippingTheLastSongEndsThePendingStopVote(t *testing.T) {
	const guildID = "skip-ends-stop-vote"
	song := queuetest.Song("Last song", "user")
	queuetest.Seed(t, guildID, song)
	t.Cleanup(func() { player.DeletePlayer(guildID) })
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	t.Cleanup(func() { vote.CancelForEndedPlayback(guildID) })

	stopVote := vote.Request{Kind: vote.KindStop, Title: "Stop vote", Emoji: "⏹", VoiceChannelID: "voice", RequiredVotes: 2}
	if err := vote.Start(session, discordtest.SlashInteraction(guildID, "stop", discordtest.Member(guildID, "voter")), stopVote); err != nil {
		t.Fatalf("failed to start the stop vote: %v", err)
	}

	if _, _, err := skipCurrentSong(session, guildID, song); err != nil {
		t.Fatalf("skipCurrentSong returned %v, want nil", err)
	}

	queueEnded := messages.T(guildID).Votes.QueueEnded
	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, request := range requests() {
			if strings.Contains(string(request.RawBody), queueEnded) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stop vote was never ended with %q after the queue emptied", queueEnded)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func runPassedVoteWithAutoLeave(t *testing.T, applyVote func(*discordgo.Session, *vote.Session, vote.Tally), songs ...*queue.Song) {
	for _, isEnabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("autoleave=%t", isEnabled), func(t *testing.T) {
			queuetest.Seed(t, commandtest.GuildID, songs...)
			prepare, checkPlayer := commandtest.AutoLeave(isEnabled)
			prepare(t)
			session, _ := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

			applyVote(session, &vote.Session{}, vote.Tally{})

			checkPlayer(t, nil)
			if q, err := queue.GetQueue(commandtest.GuildID, true); err != nil || q != nil {
				t.Errorf("the queue is %+v (err %v) after the vote passed, want it stopped and cleared", q, err)
			}
		})
	}
}

func TestAPassedSkipVoteOnTheLastSongFollowsAutoLeave(t *testing.T) {
	song := queuetest.Song("Last song", "user")
	runPassedVoteWithAutoLeave(t, applySkipVote(commandtest.GuildID, song), song)
}

func TestAPassedStopVoteFollowsAutoLeave(t *testing.T) {
	runPassedVoteWithAutoLeave(t, applyStopVote(commandtest.GuildID), queuetest.Song("Playing song", "user"))
}

func TestSkippingTheLastSongDescribesWhetherTheBotStays(t *testing.T) {
	for _, isLeaving := range []bool{true, false} {
		t.Run(fmt.Sprintf("autoleave=%t", isLeaving), func(t *testing.T) {
			guildID := fmt.Sprintf("skip-last-%t", isLeaving)
			song := queuetest.Song("Last song", "user")
			queuetest.Seed(t, guildID, song)
			t.Cleanup(func() { player.DeletePlayer(guildID) })
			if err := queue.SetAutoLeave(guildID, isLeaving); err != nil {
				t.Fatalf("failed to set auto-leave: %v", err)
			}

			embed, queueEnded, err := skipCurrentSong(nil, guildID, song)
			if err != nil {
				t.Fatalf("skipCurrentSong returned %v, want nil", err)
			}

			template := messages.T(guildID).Music.PlaybackEndedSkipStay
			if isLeaving {
				template = messages.T(guildID).Music.PlaybackEndedSkip
			}
			if !queueEnded {
				t.Error("skipping the only song did not end the queue")
			}
			if want := fmt.Sprintf(template, messages.FormatMaskedLink(song.Title, song.URL)); embed.Description != want {
				t.Errorf("description = %q, want %q", embed.Description, want)
			}
		})
	}
}
