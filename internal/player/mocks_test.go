package player

import (
	"context"
	"sync"
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

func fakeAudioStream() *audioStream {
	s := &audioStream{
		pcmChan:  make(chan []int16, audioStreamBufSize),
		errChan:  make(chan error, 1),
		stopChan: make(chan struct{}),
	}
	go func() {
		defer close(s.pcmChan)
		for {
			select {
			case <-s.stopChan:
				return
			case s.pcmChan <- make([]int16, frameSize*channels):
			}
		}
	}()
	return s
}
