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

func HandleStatus(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	
	DeferResponse(s, i)

	
	loadingEmbed := messages.CreateWarningEmbed(messages.T(i.GuildID).Status.LoadingTitle, messages.T(i.GuildID).Status.LoadingDesc)
	UpdateResponseEmbed(s, i, loadingEmbed)

	
	logger.Debugf("[Status] Starting system info gathering")

	
	cpuInfo, err := cpu.Info()
	cpuModel := "Unknown"
	cpuCores := runtime.NumCPU()
	if err == nil && len(cpuInfo) > 0 {
		cpuModel = cpuInfo[0].ModelName
	}
	logger.Debugf("[Status] CPU info gathered: %s (%d cores)", cpuModel, cpuCores)

	
	cpuPercent, err := cpu.Percent(100*time.Millisecond, false)
	cpuUsage := 0.0
	if err == nil && len(cpuPercent) > 0 {
		cpuUsage = cpuPercent[0]
	}
	logger.Debugf("[Status] CPU usage: %.2f%%", cpuUsage)

	
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

	
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	botHeapUsed := float64(m.HeapAlloc) / 1024 / 1024  
	botRSS := float64(m.Sys) / 1024 / 1024            
	logger.Debugf("[Status] Bot memory: Heap %.2f MB, RSS %.2f MB", botHeapUsed, botRSS)

	
	guildMemoryUsage := getGuildMemoryUsage(i.GuildID)
	logger.Debugf("[Status] Guild memory: %.2f MB", guildMemoryUsage)

	
	playingGuildsCount := countPlayingGuilds(s)
	logger.Debugf("[Status] Playing guilds: %d", playingGuildsCount)

	
	t := messages.T(i.GuildID)
	infoEmbed := &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       t.Status.Title,
		Description: t.Status.Description,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   t.Fields.CPUInfo,
				Value:  fmt.Sprintf(t.Status.CPUInfoValue, cpuModel, cpuCores),
				Inline: false,
			},
			{
				Name:   t.Fields.CPUUsage,
				Value:  fmt.Sprintf(t.Status.CPUUsageValue, cpuUsage),
				Inline: true,
			},
			{
				Name:   t.Fields.TotalMemory,
				Value:  fmt.Sprintf(t.Status.TotalMemoryValue, totalMemoryGB),
				Inline: true,
			},
			{
				Name:   t.Fields.MemoryUsage,
				Value:  fmt.Sprintf(t.Status.MemoryUsageValue, usedMemoryMB, totalMemoryGB, memoryUsagePercent),
				Inline: false,
			},
			{
				Name:   t.Fields.BotMemory,
				Value:  fmt.Sprintf(t.Status.BotMemoryValue, botHeapUsed, botRSS),
				Inline: true,
			},
			{
				Name:   t.Fields.ServerMemory,
				Value:  fmt.Sprintf(t.Status.ServerMemoryValue, guildMemoryUsage),
				Inline: true,
			},
			{
				Name:   t.Fields.PlayingServers,
				Value:  fmt.Sprintf(t.Status.PlayingServersValue, playingGuildsCount),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf(t.Footers.RequestedBy, i.Member.User.Username),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	
	logger.Debugf("[Status] Updating response with info embed")
	if err := UpdateResponseEmbed(s, i, infoEmbed); err != nil {
		logger.Errorf("[Status] Failed to update response: %v", err)
		return err
	}
	logger.Debugf("[Status] Successfully updated response")

	return nil
}

func getGuildMemoryUsage(guildID string) float64 {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil {
		return 0.0
	}

	
	memoryUsage := 0.0

	
	memoryUsage += 1.0 

	
	if len(q.Songs) > 0 {
		
		memoryUsage += float64(len(q.Songs)) * 2.5
	}

	return memoryUsage
}

func countPlayingGuilds(s *discordgo.Session) int {
	count := 0

	
	for _, guild := range s.State.Guilds {
		p := player.GetPlayer(guild.ID)
		if p.Playing {
			count++
		}
	}

	return count
}
