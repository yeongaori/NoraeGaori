package commands

import (
	"fmt"
	"runtime"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

// HandleStatus handles the status command (admin only)
func HandleStatus(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	// Defer response for long-running operation
	DeferResponse(s, i)

	// Show loading message
	loadingEmbed := messages.CreateWarningEmbed(messages.T().Status.LoadingTitle, messages.T().Status.LoadingDesc)
	UpdateResponseEmbed(s, i, loadingEmbed)

	// Gather system information
	logger.Debugf("[Status] Starting system info gathering")

	// CPU information
	cpuInfo, err := cpu.Info()
	cpuModel := "Unknown"
	cpuCores := runtime.NumCPU()
	if err == nil && len(cpuInfo) > 0 {
		cpuModel = cpuInfo[0].ModelName
	}
	logger.Debugf("[Status] CPU info gathered: %s (%d cores)", cpuModel, cpuCores)

	// CPU usage
	cpuPercent, err := cpu.Percent(100*time.Millisecond, false)
	cpuUsage := 0.0
	if err == nil && len(cpuPercent) > 0 {
		cpuUsage = cpuPercent[0]
	}
	logger.Debugf("[Status] CPU usage: %.2f%%", cpuUsage)

	// Memory information
	memInfo, err := mem.VirtualMemory()
	totalMemoryGB := 0.0
	usedMemoryMB := 0.0
	memoryUsagePercent := 0.0
	if err == nil {
		totalMemoryGB = float64(memInfo.Total) / 1024 / 1024 / 1024
		usedMemoryMB = float64(memInfo.Used) / 1024 / 1024
		memoryUsagePercent = memInfo.UsedPercent
	}
	logger.Debugf("[Status] Memory: %.2f GB total, %.0f MB used (%.2f%%)", totalMemoryGB, usedMemoryMB, memoryUsagePercent)

	// Bot memory usage
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	botHeapUsed := float64(m.HeapAlloc) / 1024 / 1024  // MB
	botRSS := float64(m.Sys) / 1024 / 1024            // MB
	logger.Debugf("[Status] Bot memory: Heap %.2f MB, RSS %.2f MB", botHeapUsed, botRSS)

	// Per-guild memory usage (approximate)
	guildMemoryUsage := getGuildMemoryUsage(i.GuildID)
	logger.Debugf("[Status] Guild memory: %.2f MB", guildMemoryUsage)

	// Count active playing guilds
	playingGuildsCount := countPlayingGuilds(s)
	logger.Debugf("[Status] Playing guilds: %d", playingGuildsCount)

	// Create info embed
	infoEmbed := &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       messages.T().Status.Title,
		Description: messages.T().Status.Description,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   messages.FieldCPUInfo,
				Value:  fmt.Sprintf(messages.T().Status.CPUInfoValue, cpuModel, cpuCores),
				Inline: false,
			},
			{
				Name:   messages.FieldCPUUsage,
				Value:  fmt.Sprintf(messages.T().Status.CPUUsageValue, cpuUsage),
				Inline: true,
			},
			{
				Name:   messages.FieldTotalMemory,
				Value:  fmt.Sprintf(messages.T().Status.TotalMemoryValue, totalMemoryGB),
				Inline: true,
			},
			{
				Name:   messages.FieldMemoryUsage,
				Value:  fmt.Sprintf(messages.T().Status.MemoryUsageValue, usedMemoryMB, totalMemoryGB, memoryUsagePercent),
				Inline: false,
			},
			{
				Name:   messages.FieldBotMemory,
				Value:  fmt.Sprintf(messages.T().Status.BotMemoryValue, botHeapUsed, botRSS),
				Inline: true,
			},
			{
				Name:   messages.FieldServerMemory,
				Value:  fmt.Sprintf(messages.T().Status.ServerMemoryValue, guildMemoryUsage),
				Inline: true,
			},
			{
				Name:   messages.FieldPlayingServers,
				Value:  fmt.Sprintf(messages.T().Status.PlayingServersValue, playingGuildsCount),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf(messages.FooterRequestedBy, i.Member.User.Username),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Edit the response with the info
	logger.Debugf("[Status] Updating response with info embed")
	if err := UpdateResponseEmbed(s, i, infoEmbed); err != nil {
		logger.Errorf("[Status] Failed to update response: %v", err)
		return err
	}
	logger.Debugf("[Status] Successfully updated response")

	return nil
}

// getGuildMemoryUsage calculates approximate memory usage for a specific guild
func getGuildMemoryUsage(guildID string) float64 {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil {
		return 0.0
	}

	// Approximate memory calculation
	memoryUsage := 0.0

	// Queue object base size
	memoryUsage += 1.0 // Base object ~1KB

	// Songs in queue
	if len(q.Songs) > 0 {
		// Each song object is approximately 2-3KB (title, url, thumbnail, etc.)
		memoryUsage += float64(len(q.Songs)) * 2.5
	}

	return memoryUsage
}

// countPlayingGuilds counts how many guilds are currently playing music
func countPlayingGuilds(s *discordgo.Session) int {
	count := 0

	// Iterate through all guilds
	for _, guild := range s.State.Guilds {
		p := player.GetPlayer(guild.ID)
		if p.Playing {
			count++
		}
	}

	return count
}
