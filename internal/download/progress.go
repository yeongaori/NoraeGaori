package download

type ProgressWriter struct {
	Total  int64
	Report func(percent int)
	done   int64
	last   int
}

func (writer *ProgressWriter) Write(p []byte) (int, error) {
	writer.done += int64(len(p))
	if writer.Total <= 0 {
		return len(p), nil
	}

	percent := int(min(writer.done*100/writer.Total, 100))
	if percent > writer.last {
		writer.last = percent
		writer.Report(percent)
	}
	return len(p), nil
}
