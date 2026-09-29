package download

import (
	"slices"
	"testing"
)

func recordingWriter(total int64) (*ProgressWriter, *[]int) {
	reports := []int{}
	return &ProgressWriter{Total: total, Report: func(percent int) { reports = append(reports, percent) }}, &reports
}

func TestProgressWriterReportsEveryPercentOnce(t *testing.T) {
	writer, reports := recordingWriter(1000)

	for range 100 {
		if n, err := writer.Write(make([]byte, 10)); n != 10 || err != nil {
			t.Fatalf("Write returned %d, %v, want 10, nil", n, err)
		}
	}

	want := make([]int, 100)
	for i := range want {
		want[i] = i + 1
	}
	if !slices.Equal(*reports, want) {
		t.Errorf("got %v, want 1 through 100 once each", *reports)
	}
}

func TestProgressWriterReportsOnlyTheLatestPercentOfABigWrite(t *testing.T) {
	writer, reports := recordingWriter(1000)

	writer.Write(make([]byte, 375))
	writer.Write(make([]byte, 4))
	writer.Write(make([]byte, 1))

	if !slices.Equal(*reports, []int{37, 38}) {
		t.Errorf("got %v, want 37 for the first write and 38 once the next whole percent is reached", *reports)
	}
}

func TestProgressWriterStopsAtOneHundred(t *testing.T) {
	writer, reports := recordingWriter(1000)

	writer.Write(make([]byte, 999))
	writer.Write(make([]byte, 1))
	writer.Write(make([]byte, 500))

	if !slices.Equal(*reports, []int{99, 100}) {
		t.Errorf("got %v, want 99 then 100 and nothing past the total", *reports)
	}
}

func TestProgressWriterIgnoresAnUnknownTotal(t *testing.T) {
	for _, total := range []int64{0, -1} {
		writer, reports := recordingWriter(total)

		if n, err := writer.Write(make([]byte, 64)); n != 64 || err != nil {
			t.Fatalf("Write returned %d, %v, want 64, nil", n, err)
		}
		if len(*reports) != 0 {
			t.Errorf("total %d: got %v, want no reports for an unknown size", total, *reports)
		}
	}
}
