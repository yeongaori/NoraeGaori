package playback_test

import (
	"testing"

	"noraegaori/internal/commands/playback"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/dbtest"
)

func TestPausedDescriptionFollowsAutoLeave(t *testing.T) {
	dbtest.Setup(t)
	descriptions := messages.T("g1").Descriptions

	if got := playback.HookPausedDescription("g1"); got != descriptions.Paused {
		t.Errorf("default pause reply = %q, want the leaving text", got)
	}

	if err := queue.SetAutoLeave("g1", false); err != nil {
		t.Fatalf("failed to turn auto-leave off: %v", err)
	}
	if got := playback.HookPausedDescription("g1"); got != descriptions.PausedStay {
		t.Errorf("pause reply with auto-leave off = %q, want the staying text", got)
	}
}
