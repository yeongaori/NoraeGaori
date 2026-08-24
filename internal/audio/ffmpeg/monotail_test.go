package ffmpeg

import "testing"

func TestMonoTailKeepsMostRecentSamples(t *testing.T) {
	cases := []struct {
		capacity int
		appends  int
	}{
		{capacity: 1, appends: 0},
		{capacity: 1, appends: 1},
		{capacity: 1, appends: 5},
		{capacity: 7, appends: 6},
		{capacity: 7, appends: 7},
		{capacity: 7, appends: 8},
		{capacity: 7, appends: 23},
		{capacity: 64, appends: 200},
	}

	for _, tc := range cases {
		tail := newMonoTail(tc.capacity)
		for i := 0; i < tc.appends; i++ {
			tail.append(float32(i))
		}

		kept := tc.appends
		if kept > tc.capacity {
			kept = tc.capacity
		}
		wantStart := int64(tc.appends - kept)

		samples, startSample := tail.snapshot()

		if startSample != wantStart {
			t.Errorf("capacity %d after %d appends: startSample = %d, want %d",
				tc.capacity, tc.appends, startSample, wantStart)
		}
		if len(samples) != kept {
			t.Fatalf("capacity %d after %d appends: len = %d, want %d",
				tc.capacity, tc.appends, len(samples), kept)
		}
		for i, sample := range samples {
			if want := float32(wantStart + int64(i)); sample != want {
				t.Fatalf("capacity %d after %d appends: sample[%d] = %v, want %v",
					tc.capacity, tc.appends, i, sample, want)
			}
		}
	}
}

func TestMonoTailSnapshotIsRepeatable(t *testing.T) {
	tail := newMonoTail(8)
	for i := 0; i < 21; i++ {
		tail.append(float32(i))
	}

	first, firstStart := tail.snapshot()
	captured := append([]float32(nil), first...)

	second, secondStart := tail.snapshot()

	if secondStart != firstStart {
		t.Errorf("startSample changed between snapshots: %d then %d", firstStart, secondStart)
	}
	if len(second) != len(captured) {
		t.Fatalf("length changed between snapshots: %d then %d", len(captured), len(second))
	}
	for i := range captured {
		if second[i] != captured[i] {
			t.Fatalf("sample[%d] changed between snapshots: %v then %v", i, captured[i], second[i])
		}
	}
}
