package player

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"noraegaori/internal/database"
	"noraegaori/internal/queue"
)

func newTestPlayer(guildID string, handler func(PlayerCommand) error) *GuildPlayer {
	p := &GuildPlayer{
		GuildID:          guildID,
		Volume:           1.0,
		StopChan:         make(chan struct{}),
		PlaybackDone:     make(chan struct{}, 1),
		CommandChan:      make(chan PlayerCommand, 10),
		QuitChan:         make(chan struct{}),
		processorRunning: true,
		dispatch:         handler,
	}
	go p.processCommands()
	return p
}

func (p *GuildPlayer) stopTestProcessor() {
	close(p.QuitChan)
}

func sendTestCommand(p *GuildPlayer, cmdType string) chan error {
	done := make(chan error, 1)
	p.CommandChan <- PlayerCommand{Type: cmdType, GuildID: p.GuildID, Done: done}
	return done
}

func TestDispatchSerialization(t *testing.T) {
	var mu sync.Mutex
	var order []string

	p := newTestPlayer("serialguild", func(cmd PlayerCommand) error {
		time.Sleep(2 * time.Millisecond)
		mu.Lock()
		order = append(order, cmd.Type)
		mu.Unlock()
		return nil
	})
	defer p.stopTestProcessor()

	dones := make([]chan error, 5)
	for i := 0; i < 5; i++ {
		dones[i] = sendTestCommand(p, fmt.Sprintf("cmd%d", i))
	}
	for i, d := range dones {
		select {
		case err := <-d:
			if err != nil {
				t.Errorf("cmd%d returned error: %v", i, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("cmd%d never completed", i)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 5 {
		t.Fatalf("expected 5 processed, got %d", len(order))
	}
	for i := 0; i < 5; i++ {
		if order[i] != fmt.Sprintf("cmd%d", i) {
			t.Errorf("out of order at %d: got %s", i, order[i])
		}
	}
}

func TestDispatchPanicRecovery(t *testing.T) {
	p := newTestPlayer("panicguild", func(cmd PlayerCommand) error {
		if cmd.Type == "boom" {
			panic("simulated command panic")
		}
		return nil
	})
	defer p.stopTestProcessor()

	okDone := sendTestCommand(p, "ok")
	if err := <-okDone; err != nil {
		t.Errorf("ok command errored: %v", err)
	}

	boomDone := sendTestCommand(p, "boom")
	select {
	case err := <-boomDone:
		if err == nil {
			t.Error("panicking command should report an error via Done")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("boom command never reported")
	}

	afterDone := sendTestCommand(p, "ok")
	select {
	case err := <-afterDone:
		if err != nil {
			t.Errorf("command after panic errored: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("processor died after panic - next command never ran")
	}

	p.mu.Lock()
	running := p.processorRunning
	p.mu.Unlock()
	if !running {
		t.Error("processor should still be running after a recovered panic")
	}
}

func TestDispatchErrorPropagation(t *testing.T) {
	wantErr := fmt.Errorf("boom error")
	p := newTestPlayer("errguild", func(cmd PlayerCommand) error {
		return wantErr
	})
	defer p.stopTestProcessor()

	done := sendTestCommand(p, "whatever")
	if err := <-done; err == nil || err.Error() != wantErr.Error() {
		t.Errorf("want %v, got %v", wantErr, err)
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	p := newTestPlayer("unknownguild", nil)
	defer p.stopTestProcessor()

	done := sendTestCommand(p, "nonsense")
	if err := <-done; err == nil {
		t.Error("unknown command type should error via defaultDispatch")
	}
}

func TestForceSkipSpamAdvancesQueueCleanly(t *testing.T) {
	os.RemoveAll("data")
	if err := database.Initialize(); err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() {
		database.Close()
		os.RemoveAll("data")
	}()

	guildID := "spamguild"
	if err := queue.CreateQueue(guildID, "text", "voice"); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	defer queue.DeleteQueue(guildID)

	for i := 0; i < 2; i++ {
		song := &queue.Song{
			URL:            fmt.Sprintf("https://youtube.com/watch?v=spam%d", i),
			Title:          fmt.Sprintf("Spam %d", i),
			Duration:       "3:00",
			RequestedByID:  "user1",
			RequestedByTag: "User#1234",
		}
		if err := queue.AddSong(guildID, song, -1); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	p := newTestPlayer(guildID, func(cmd PlayerCommand) error {
		if cmd.Type == "skip" {
			return queue.RemoveFirstSong(cmd.GuildID)
		}
		return nil
	})
	defer p.stopTestProcessor()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			done := sendTestCommand(p, "skip")
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("skip errored: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Error("skip never completed")
			}
		}()
	}
	wg.Wait()

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	if q == nil || len(q.Songs) != 0 {
		t.Errorf("expected empty queue after 5 skips on 2 songs, got %v", q)
	}

	p.mu.Lock()
	running := p.processorRunning
	p.mu.Unlock()
	if !running {
		t.Error("processor should survive command spam")
	}
}

func TestCommandChanBufferFull(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	p := newTestPlayer("fullguild", func(cmd PlayerCommand) error {
		if cmd.Type == "block" {
			close(started)
			<-release
		}
		return nil
	})
	defer p.stopTestProcessor()
	defer close(release)

	p.CommandChan <- PlayerCommand{Type: "block", GuildID: p.GuildID}
	<-started

	for i := 0; i < cap(p.CommandChan); i++ {
		p.CommandChan <- PlayerCommand{Type: "x", GuildID: p.GuildID}
	}

	select {
	case p.CommandChan <- PlayerCommand{Type: "overflow", GuildID: p.GuildID}:
		t.Error("send to a full CommandChan should not succeed (production returns 'queue full')")
	default:
	}
}
