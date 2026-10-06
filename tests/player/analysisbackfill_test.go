package player_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/queuetest"
)

type segmentRead struct {
	url     string
	start   float64
	seconds int
}

type backfillFake struct {
	mu      sync.Mutex
	reads   []segmentRead
	analyze func(ctx context.Context, read segmentRead) (*analysis.TrackAnalysis, error)
}

func (fake *backfillFake) snapshot() []segmentRead {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]segmentRead(nil), fake.reads...)
}

func installBackfillFake(t *testing.T) *backfillFake {
	t.Helper()
	fake := &backfillFake{
		analyze: func(context.Context, segmentRead) (*analysis.TrackAnalysis, error) {
			return &analysis.TrackAnalysis{BPM: 120, PeriodSec: 0.5, Duration: 90}, nil
		},
	}
	testutil.Swap(t, player.HookFetchBackfillStreamURL, func(_ context.Context, url string, _ bool, _ int) (string, error) {
		return url, nil
	})
	testutil.Swap(t, player.HookFetchStreamURL, func(url string, _ bool, _ int) (string, error) {
		return url, nil
	})
	testutil.Swap(t, player.HookAnalyzeSegment, func(ctx context.Context, streamURL string, startSec float64, seconds int) (*analysis.TrackAnalysis, error) {
		read := segmentRead{url: streamURL, start: startSec, seconds: seconds}
		fake.mu.Lock()
		fake.reads = append(fake.reads, read)
		analyze := fake.analyze
		fake.mu.Unlock()
		return analyze(ctx, read)
	})
	testutil.Swap(t, player.HookAnalysisBackfillGap, 0)
	return fake
}

func seedBackfillQueue(t *testing.T, guildID string, durations ...string) []*queue.Song {
	t.Helper()
	songs := make([]*queue.Song, 0, len(durations))
	for index, duration := range durations {
		songs = append(songs, &queue.Song{
			URL:            fmt.Sprintf("https://youtube.com/watch?v=%s%d", guildID, index),
			Title:          fmt.Sprintf("Song %d", index),
			Duration:       duration,
			RequestedByID:  "user1",
			RequestedByTag: "User#1234",
		})
	}
	return queuetest.SeedWithVoice(t, guildID, "voice", songs...).Songs
}

func registerIdlePlayer(t *testing.T, guildID string) *player.GuildPlayer {
	t.Helper()
	guildPlayer := player.HookBuildGuildPlayer(player.HookGuildPlayerFields{GuildID: guildID, Volume: 1})
	player.HookPlayersMu.Lock()
	(*player.HookPlayers)[guildID] = guildPlayer
	player.HookPlayersMu.Unlock()
	t.Cleanup(func() {
		player.HookPlayersMu.Lock()
		delete(*player.HookPlayers, guildID)
		player.HookPlayersMu.Unlock()
	})
	return guildPlayer
}

func runBackfillPass(t *testing.T, guildID string) int {
	t.Helper()
	analyzed, err := player.HookRunAnalysisBackfillPass(context.Background(), guildID, 128000)
	if err != nil {
		t.Fatalf("the pass returned %v", err)
	}
	return analyzed
}

func TestBackfillAnalyzesThePlayingSongsEndingFirst(t *testing.T) {
	const guildID = "backfill-tail-first"
	songs := seedBackfillQueue(t, guildID, "3:00", "1:00", "4:00")
	registerIdlePlayer(t, guildID)
	fake := installBackfillFake(t)

	if analyzed := runBackfillPass(t, guildID); analyzed != 3 {
		t.Fatalf("analyzed %d songs, want the playing ending and two heads", analyzed)
	}
	reads := fake.snapshot()
	want := []segmentRead{
		{url: songs[0].URL, start: 90, seconds: 90},
		{url: songs[1].URL, start: 0, seconds: player.HookAnalysisHeadSecs},
		{url: songs[2].URL, start: 0, seconds: player.HookAnalysisHeadSecs},
	}
	if len(reads) != len(want) {
		t.Fatalf("read %d segments, want %d: %+v", len(reads), len(want), reads)
	}
	for index := range want {
		if reads[index] != want[index] {
			t.Errorf("read %d = %+v, want %+v", index, reads[index], want[index])
		}
	}
	if analysis.LoadTrackAnalysis(songs[0].URL, analysis.SegmentTail) == nil {
		t.Error("the playing song's ending was not stored")
	}
	if analysis.LoadTrackAnalysis(songs[0].URL, analysis.SegmentHead) != nil {
		t.Error("the playing song's start was analyzed, want only its ending")
	}
	if analysis.LoadTrackAnalysis(songs[1].URL, analysis.SegmentTail) != nil {
		t.Error("a queued song's ending was analyzed, want only its start")
	}
}

func TestBackfillReadsAShortPlayingSongFromTheStart(t *testing.T) {
	const guildID = "backfill-short-tail"
	songs := seedBackfillQueue(t, guildID, "1:00")
	items := player.HookPlanBackfillWork(guildID, songs, true)
	if len(items) != 1 {
		t.Fatalf("planned %d items, want the playing song's ending", len(items))
	}
	if start, seconds := items[0].HookWindow(); start != 0 || seconds != 90 {
		t.Errorf("a 60 s song reads %d s from %.1f, want 90 s from 0", seconds, start)
	}
}

func TestBackfillPlansNoEndingItCannotUseOrAlreadyHas(t *testing.T) {
	const guildID = "backfill-tail-skips"
	songs := seedBackfillQueue(t, guildID, "3:00", "")
	if err := analysis.SaveTrackAnalysis(songs[0].URL, analysis.SegmentTail, &analysis.TrackAnalysis{BPM: 120, Duration: 90}); err != nil {
		t.Fatalf("saving the stored tail: %v", err)
	}

	stored := player.HookPlanBackfillWork(guildID, songs[:1], true)
	if len(stored) != 0 {
		t.Errorf("planned %d items for a playing song whose ending is stored, want none", len(stored))
	}
	unknown := player.HookPlanBackfillWork(guildID, songs[1:], true)
	if len(unknown) != 0 {
		t.Errorf("planned %d items for a playing song of unknown length, want none", len(unknown))
	}
	live := &queue.Song{ID: 99, URL: "https://youtube.com/watch?v=live-tail", Duration: "3:00", IsLive: true}
	if items := player.HookPlanBackfillWork(guildID, []*queue.Song{live}, true); len(items) != 0 {
		t.Errorf("planned %d items for a playing live stream, want none", len(items))
	}

	idle := player.HookPlanBackfillWork(guildID, songs[1:], false)
	if len(idle) != 1 || idle[0].HookSegment() != analysis.SegmentHead {
		t.Errorf("an idle first song planned %+v, want its start", idle)
	}
}

func TestFailedEndingLeavesTheStartToAnalyze(t *testing.T) {
	const guildID = "backfill-failed-tail"
	songs := seedBackfillQueue(t, guildID, "3:00")
	player.HookMarkAnalysisFailed(songs[0].URL, analysis.SegmentTail)

	if items := player.HookPlanBackfillWork(guildID, songs, true); len(items) != 0 {
		t.Errorf("planned %d items for an ending that just failed, want none", len(items))
	}
	if player.HookAnalysisFailed(songs[0].URL, analysis.SegmentHead) {
		t.Error("a failed ending marked the start as failed too")
	}
	items := player.HookPlanBackfillWork(guildID, songs, false)
	if len(items) != 1 || items[0].HookSegment() != analysis.SegmentHead {
		t.Errorf("planned %+v once the song is no longer playing, want its start", items)
	}
}

func TestBackfillFailureMarksOnlyThatSegment(t *testing.T) {
	const guildID = "backfill-failure-mark"
	songs := seedBackfillQueue(t, guildID, "3:00")
	registerIdlePlayer(t, guildID)
	fake := installBackfillFake(t)
	fake.analyze = func(context.Context, segmentRead) (*analysis.TrackAnalysis, error) {
		return nil, errors.New("decode failed")
	}

	if analyzed := runBackfillPass(t, guildID); analyzed != 0 {
		t.Fatalf("analyzed %d songs from a failing decoder, want 0", analyzed)
	}
	if !player.HookAnalysisFailed(songs[0].URL, analysis.SegmentTail) {
		t.Error("the failed ending was not marked")
	}
	if player.HookAnalysisFailed(songs[0].URL, analysis.SegmentHead) {
		t.Error("the failed ending marked the start as failed")
	}
}

func TestBackfillKeepsAnEndingStoredMeanwhile(t *testing.T) {
	const guildID = "backfill-tail-race"
	songs := seedBackfillQueue(t, guildID, "3:00")
	registerIdlePlayer(t, guildID)
	fake := installBackfillFake(t)
	fake.analyze = func(context.Context, segmentRead) (*analysis.TrackAnalysis, error) {
		live := &analysis.TrackAnalysis{BPM: 128, Duration: 77}
		if err := analysis.SaveTrackAnalysis(songs[0].URL, analysis.SegmentTail, live); err != nil {
			t.Errorf("saving the live tail: %v", err)
		}
		return &analysis.TrackAnalysis{BPM: 120, Duration: 90}, nil
	}

	if analyzed := runBackfillPass(t, guildID); analyzed != 0 {
		t.Errorf("counted %d analyzed songs, want 0 when the playback stream stored its own ending first", analyzed)
	}
	stored := analysis.LoadTrackAnalysis(songs[0].URL, analysis.SegmentTail)
	if stored == nil || stored.Duration != 77 {
		t.Errorf("stored tail = %+v, want the one the playback stream saved", stored)
	}
}

func waitForReads(t *testing.T, fake *backfillFake, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(fake.snapshot()) < count {
		if time.Now().After(deadline) {
			t.Fatalf("waited for %d reads, saw %d", count, len(fake.snapshot()))
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForNoPending(t *testing.T, guildID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(player.PendingAnalyses(guildID)) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the backfill pass never finished: %v still pending", player.PendingAnalyses(guildID))
		}
		time.Sleep(time.Millisecond)
	}
}

func pendingSongIDs(guildID string) map[int]bool {
	songIDs := make(map[int]bool)
	for songID := range player.PendingAnalyses(guildID) {
		songIDs[songID] = true
	}
	return songIDs
}

func TestPendingFollowsTheBackfillPass(t *testing.T) {
	const guildID = "backfill-pending"
	songs := seedBackfillQueue(t, guildID, "3:00", "3:00", "3:00", "3:00")
	registerIdlePlayer(t, guildID)
	player.HookMarkAnalysisFailed(songs[3].URL, analysis.SegmentHead)
	fake := installBackfillFake(t)
	release := make(chan struct{})
	fake.analyze = func(ctx context.Context, read segmentRead) (*analysis.TrackAnalysis, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &analysis.TrackAnalysis{BPM: 120, Duration: 90}, nil
	}

	done := make(chan int, 1)
	go func() {
		analyzed, _ := player.HookRunAnalysisBackfillPass(context.Background(), guildID, 128000)
		done <- analyzed
	}()

	waitForReads(t, fake, 1)
	if got := pendingSongIDs(guildID); len(got) != 3 || !got[songs[0].ID] || !got[songs[1].ID] || !got[songs[2].ID] {
		t.Errorf("pending while the first read runs = %v, want the playing song and the two unfailed heads", got)
	}
	release <- struct{}{}
	waitForReads(t, fake, 2)
	if got := pendingSongIDs(guildID); got[songs[0].ID] || !got[songs[1].ID] {
		t.Errorf("pending during the second read = %v, want the first song cleared and the second still marked", got)
	}
	release <- struct{}{}
	release <- struct{}{}
	if analyzed := <-done; analyzed != 3 {
		t.Errorf("analyzed %d songs, want 3", analyzed)
	}
	if got := player.PendingAnalyses(guildID); len(got) != 0 {
		t.Errorf("pending after the pass = %v, want none", got)
	}
}

func TestCancelledPassClearsItsPendingSongs(t *testing.T) {
	const guildID = "backfill-cancel"
	seedBackfillQueue(t, guildID, "3:00", "3:00", "3:00")
	fake := installBackfillFake(t)
	fake.analyze = func(ctx context.Context, read segmentRead) (*analysis.TrackAnalysis, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := player.HookRunAnalysisBackfillPass(ctx, guildID, 128000)
		done <- err
	}()
	waitForReads(t, fake, 1)
	if got := player.PendingAnalyses(guildID); len(got) != 3 {
		t.Errorf("pending before the cancel = %v, want all three songs", got)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("the cancelled pass returned %v, want context.Canceled", err)
	}
	if got := player.PendingAnalyses(guildID); len(got) != 0 {
		t.Errorf("pending after the cancel = %v, want none", got)
	}
}

func TestDeferredPassClearsItsPendingSongs(t *testing.T) {
	const guildID = "backfill-deferred"
	seedBackfillQueue(t, guildID, "3:00", "3:00")
	guildPlayer := registerIdlePlayer(t, guildID)
	guildPlayer.HookTransitionArmed().Store(true)
	fake := installBackfillFake(t)

	if analyzed := runBackfillPass(t, guildID); analyzed != 0 {
		t.Errorf("analyzed %d songs with a transition pending, want 0", analyzed)
	}
	if reads := fake.snapshot(); len(reads) != 0 {
		t.Errorf("read %d segments with a transition pending, want none", len(reads))
	}
	if got := player.PendingAnalyses(guildID); len(got) != 0 {
		t.Errorf("pending after the deferred pass = %v, want none", got)
	}
}

func TestBackfillPlansNothingPastTheLimit(t *testing.T) {
	songs := make([]*queue.Song, 0, player.HookAnalysisBackfillLimit+3)
	for index := 0; index < cap(songs); index++ {
		songs = append(songs, &queue.Song{ID: index + 1, URL: fmt.Sprintf("https://youtube.com/watch?v=limit%d", index), Duration: "3:00"})
	}
	items := player.HookPlanBackfillWork("backfill-limit", songs, false)
	if len(items) != player.HookAnalysisBackfillLimit {
		t.Fatalf("planned %d items, want the limit of %d", len(items), player.HookAnalysisBackfillLimit)
	}
	for _, item := range items {
		if item.HookSong().ID > player.HookAnalysisBackfillLimit {
			t.Errorf("planned song %d past the limit", item.HookSong().ID)
		}
	}
}

func enableAutoMix(t *testing.T, guildID string) {
	t.Helper()
	if err := queue.SetAutoMix(guildID, true); err != nil {
		t.Fatalf("turning AutoMix on: %v", err)
	}
}

func TestPreCacheMarksItsSongWhileAnalyzing(t *testing.T) {
	const guildID = "precache-pending"
	songs := seedBackfillQueue(t, guildID, "3:00", "3:00")
	enableAutoMix(t, guildID)
	fake := installBackfillFake(t)
	release := make(chan struct{})
	fake.analyze = func(context.Context, segmentRead) (*analysis.TrackAnalysis, error) {
		<-release
		return &analysis.TrackAnalysis{BPM: 120, Duration: 75}, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- player.HookPreCacheSong(context.Background(), guildID, songs[1], false, 128000)
	}()
	waitForReads(t, fake, 1)
	if got := player.PendingAnalyses(guildID); got[songs[1].ID] != 1 || len(got) != 1 {
		t.Errorf("pending while pre-cache analyzes = %v, want only the next song", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("pre-cache returned %v", err)
	}
	if got := player.PendingAnalyses(guildID); len(got) != 0 {
		t.Errorf("pending after pre-cache = %v, want none", got)
	}
	if read := fake.snapshot()[0]; read.start != 0 || read.seconds != player.HookAnalysisHeadSecs {
		t.Errorf("pre-cache read %+v, want the start of the song", read)
	}
	if player.GetCachedAnalysis(guildID, songs[1].ID) == nil {
		t.Error("pre-cache did not keep the analysis it made")
	}
}

func TestPreCacheSkippingTheNextSongStillAnalyzesThePlayingEnding(t *testing.T) {
	cases := []struct {
		name string
		next *queue.Song
	}{
		{"live next", &queue.Song{URL: "https://youtube.com/watch?v=precache-live-next", Title: "Live", IsLive: true}},
		{"seeked next", &queue.Song{URL: "https://youtube.com/watch?v=precache-seek-next", Title: "Seeked", Duration: "3:00", SeekTime: 30000}},
	}
	for index, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			guildID := fmt.Sprintf("precache-skip-%d", index)
			songs := seedBackfillQueue(t, guildID, "3:00")
			c.next.RequestedByID, c.next.RequestedByTag = "user1", "User#1234"
			if err := queue.AddSong(guildID, c.next, -1); err != nil {
				t.Fatalf("adding the next song: %v", err)
			}
			if c.next.SeekTime > 0 {
				seeded, err := queue.GetQueue(guildID, true)
				if err != nil || len(seeded.Songs) != 2 {
					t.Fatalf("reloading the queue: %v", err)
				}
				if err := queue.UpdateSongSeekTime(guildID, seeded.Songs[1].ID, c.next.SeekTime); err != nil {
					t.Fatalf("setting the seek time: %v", err)
				}
			}
			enableAutoMix(t, guildID)
			registerIdlePlayer(t, guildID)
			fake := installBackfillFake(t)
			fake.analyze = func(ctx context.Context, read segmentRead) (*analysis.TrackAnalysis, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			t.Cleanup(func() {
				player.StopAnalysisBackfill(guildID)
				waitForNoPending(t, guildID)
			})

			player.PreCacheNext(guildID, 128000)
			waitForReads(t, fake, 1)
			if read := fake.snapshot()[0]; read.url != songs[0].URL || read.start != 90 {
				t.Errorf("first read = %+v, want the playing song's ending from 90 s", read)
			}
		})
	}
}

func TestPendingCountsEachMark(t *testing.T) {
	const guildID = "pending-counts"
	player.HookMarkAnalysisPending(guildID, 7)
	player.HookMarkAnalysisPending(guildID, 7, 8)
	published := player.PendingAnalyses(guildID)

	player.HookClearAnalysisPending(guildID, 7, 8)
	if got := player.PendingAnalyses(guildID); got[7] != 1 || got[8] != 0 {
		t.Errorf("after one clear = %v, want song 7 still marked once and song 8 gone", got)
	}
	if published[7] != 2 || published[8] != 1 {
		t.Errorf("an earlier published map changed to %v, want it left as it was", published)
	}
	player.HookClearAnalysisPending(guildID, 7)
	if got := player.PendingAnalyses(guildID); got != nil {
		t.Errorf("after the last clear = %v, want nothing for the guild", got)
	}
}

func TestSettleKeepsAndSavesAFullLiveEnding(t *testing.T) {
	songs := seedBackfillQueue(t, "settle-live", "3:00")
	if err := analysis.SaveTrackAnalysis(songs[0].URL, analysis.SegmentTail, &analysis.TrackAnalysis{BPM: 120, Duration: 90}); err != nil {
		t.Fatalf("saving the early tail: %v", err)
	}
	live := &analysis.TrackAnalysis{BPM: 128, Duration: 88}
	es := &ffmpeg.EndState{TotalFrames: 9000, TailStartFrame: 4500, Analysis: live}

	settled := player.HookSettleTailAnalysis(songs[0], es, 2000)
	if settled != es || settled.Analysis != live {
		t.Fatal("a full live ending was replaced, want the stream's own end state kept")
	}
	if live.Offset != 92 {
		t.Errorf("live offset = %.2f, want 2 s base plus 90 s of frames", live.Offset)
	}
	if stored := analysis.LoadTrackAnalysis(songs[0].URL, analysis.SegmentTail); stored == nil || stored.BPM != 128 || stored.Offset != 92 {
		t.Errorf("stored tail = %+v, want the live ending saved over the early one", stored)
	}
}

func TestSettleUsesTheEarlyEndingAfterALateSeek(t *testing.T) {
	songs := seedBackfillQueue(t, "settle-stored", "3:00")
	early := &analysis.TrackAnalysis{BPM: 120, Duration: 90, Offset: 90}
	if err := analysis.SaveTrackAnalysis(songs[0].URL, analysis.SegmentTail, early); err != nil {
		t.Fatalf("saving the early tail: %v", err)
	}
	live := &analysis.TrackAnalysis{BPM: 131, Duration: 12}
	es := &ffmpeg.EndState{TotalFrames: 750, TailStartFrame: 0, SilentTailFrames: 3, Analysis: live}

	settled := player.HookSettleTailAnalysis(songs[0], es, 165000)
	if settled == es {
		t.Fatal("the stream's end state was changed in place, want a copy carrying the stored tail")
	}
	if es.Analysis != live {
		t.Error("the stream's own end state lost its live analysis")
	}
	if settled.Analysis == nil || settled.Analysis.BPM != 120 || settled.Analysis.Offset != 90 {
		t.Errorf("settled analysis = %+v, want the early 90 s ending", settled.Analysis)
	}
	if settled.TotalFrames != 750 || settled.SilentTailFrames != 3 {
		t.Errorf("settled frames = %d/%d, want the stream's counts carried over", settled.TotalFrames, settled.SilentTailFrames)
	}
	if stored := analysis.LoadTrackAnalysis(songs[0].URL, analysis.SegmentTail); stored == nil || stored.BPM != 120 {
		t.Errorf("stored tail = %+v, want the early ending kept, not the 12 s live one", stored)
	}
}

func TestSettleLeavesAnEndWithoutAnyAnalysis(t *testing.T) {
	songs := seedBackfillQueue(t, "settle-none", "3:00")
	es := &ffmpeg.EndState{TotalFrames: 100}
	if settled := player.HookSettleTailAnalysis(songs[0], es, 0); settled != es || settled.Analysis != nil {
		t.Errorf("settled = %+v, want the same end state with no analysis", settled)
	}
}

func TestChooseTailPrefersTheLiveEndingUnlessTheStoredOneCoversMore(t *testing.T) {
	live := &analysis.TrackAnalysis{Duration: 60}
	cases := []struct {
		name       string
		live       *analysis.TrackAnalysis
		stored     *analysis.TrackAnalysis
		want       *analysis.TrackAnalysis
		wantIsLive bool
	}{
		{"no live ending", nil, &analysis.TrackAnalysis{Duration: 90}, nil, false},
		{"nothing at all", nil, nil, nil, false},
		{"nothing stored", live, nil, live, true},
		{"stored covers much more", live, &analysis.TrackAnalysis{Duration: 60 + player.HookStoredTailMarginSec + 1}, nil, false},
		{"stored covers barely more", live, &analysis.TrackAnalysis{Duration: 60 + player.HookStoredTailMarginSec}, live, true},
		{"live covers more", live, &analysis.TrackAnalysis{Duration: 30}, live, true},
	}
	for _, c := range cases {
		want := c.want
		if want == nil {
			want = c.stored
		}
		chosen, isLive := player.HookChooseTailAnalysis(c.live, c.stored)
		if chosen != want || isLive != c.wantIsLive {
			t.Errorf("%s: chose %p (live %t), want %p (live %t)", c.name, chosen, isLive, want, c.wantIsLive)
		}
	}
}
