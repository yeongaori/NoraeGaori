//go:build testhooks

package ffmpeg

const HookStderrTailBytes = stderrTailBytes

type HookStderrTail = stderrTail

var HookNewMonoTail = newMonoTail

func (s *Stream) HookErrChan() *chan error {
	return &s.errChan
}

func (m *monoTail) HookAppend(s float32) {
	m.append(s)
}

func (m *monoTail) HookSnapshot() ([]float32, int64) {
	return m.snapshot()
}
