package player

import (
	"context"
	"fmt"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/queue"
	"sync"
	"sync/atomic"

	"github.com/bwmarrin/discordgo"
)

type mockVoiceConn struct {
	opusSend chan []byte
	dead     chan struct{}

	mu          sync.Mutex
	speaking    []bool
	disconnects int
}

func newMockVoiceConn() *mockVoiceConn {
	return &mockVoiceConn{
		opusSend: make(chan []byte, 4096),
		dead:     make(chan struct{}),
	}
}

func (m *mockVoiceConn) OpusSendChan() chan []byte { return m.opusSend }
func (m *mockVoiceConn) DeadChan() <-chan struct{} { return m.dead }
func (m *mockVoiceConn) Err() error                { return nil }

func (m *mockVoiceConn) Speaking(b bool) error {
	m.mu.Lock()
	m.speaking = append(m.speaking, b)
	m.mu.Unlock()
	return nil
}

func (m *mockVoiceConn) Disconnect(ctx context.Context) error {
	m.mu.Lock()
	m.disconnects++
	m.mu.Unlock()
	return nil
}

func (m *mockVoiceConn) disconnectCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.disconnects
}

func (m *mockVoiceConn) speakingTrueSeen() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.speaking {
		if b {
			return true
		}
	}
	return false
}

type fakeStream struct {
	pcm      chan []int16
	errs     chan error
	stopChan chan struct{}
	stopOnce sync.Once
	endState atomic.Pointer[ffmpeg.EndState]
}

func newFakeStream(buffer int) *fakeStream {
	return &fakeStream{
		pcm:      make(chan []int16, buffer),
		errs:     make(chan error, 1),
		stopChan: make(chan struct{}),
	}
}

func (f *fakeStream) PCM() <-chan []int16        { return f.pcm }
func (f *fakeStream) Errs() <-chan error         { return f.errs }
func (f *fakeStream) EndState() *ffmpeg.EndState { return f.endState.Load() }
func (f *fakeStream) Buffered() int              { return len(f.pcm) }
func (f *fakeStream) Diagnostics() string        { return "" }

func (f *fakeStream) setEndState(es *ffmpeg.EndState) {
	f.endState.Store(es)
}

func (f *fakeStream) Stop() {
	f.stopOnce.Do(func() { close(f.stopChan) })
}

func (f *fakeStream) sendFrames(frames int) {
	go func() {
		defer close(f.pcm)
		for i := 0; frames < 0 || i < frames; i++ {
			select {
			case <-f.stopChan:
				return
			case f.pcm <- make([]int16, frameSize*channels):
			}
		}
	}()
}

func fakeAudioStream() audioStream {
	s := newFakeStream(ffmpeg.BufSize)
	s.sendFrames(-1)
	return s
}

type sentEmbed struct {
	channelID string
	messageID string
	embed     *discordgo.MessageEmbed
}

type fakeEmbedSender struct {
	mu       sync.Mutex
	sends    []sentEmbed
	edits    []sentEmbed
	editFail bool
}

func (f *fakeEmbedSender) ChannelMessageSendEmbed(channelID string, embed *discordgo.MessageEmbed, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, sentEmbed{channelID: channelID, embed: embed})
	return &discordgo.Message{ID: "sent", ChannelID: channelID}, nil
}

func (f *fakeEmbedSender) ChannelMessageEditEmbed(channelID, messageID string, embed *discordgo.MessageEmbed, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.editFail {
		return nil, fmt.Errorf("edit rejected")
	}
	f.edits = append(f.edits, sentEmbed{channelID: channelID, messageID: messageID, embed: embed})
	return &discordgo.Message{ID: messageID, ChannelID: channelID}, nil
}

func nowPlayingFixture(guildID string, showStartedTrack bool) (*queue.Song, *queue.Queue) {
	song := &queue.Song{
		ID:             7,
		URL:            "https://youtube.com/watch?v=announce",
		Title:          "Announced Song",
		Duration:       "3:00",
		Uploader:       "Uploader",
		RequestedByTag: "User#1234",
	}
	q := &queue.Queue{
		GuildID:          guildID,
		TextChannelID:    "text",
		ShowStartedTrack: showStartedTrack,
	}
	return song, q
}
