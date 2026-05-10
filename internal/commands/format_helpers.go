package commands

import (
	"noraegaori/internal/youtube"
)

func createProgressBar(currentMs int, durationStr string) string {
	totalSeconds := youtube.ParseDurationToSeconds(durationStr)
	if totalSeconds == 0 {
		return "▬▬▬▬▬▬▬▬▬▬"
	}

	currentSeconds := currentMs / 1000
	progress := float64(currentSeconds) / float64(totalSeconds)
	if progress > 1.0 {
		progress = 1.0
	}

	barLength := 10
	filled := int(progress * float64(barLength))

	bar := ""
	for i := 0; i < barLength; i++ {
		if i < filled {
			bar += "▬"
		} else if i == filled {
			bar += "🔘"
		} else {
			bar += "▬"
		}
	}

	return bar
}

func boolToEmoji(b bool) string {
	if b {
		return "✅"
	}
	return "❌"
}
