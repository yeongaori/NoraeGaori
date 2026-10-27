package player_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/player"
)

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
