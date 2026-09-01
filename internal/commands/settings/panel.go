package settings

import (
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
)

const settingsPanelExpiry = 5 * time.Minute

type panelSession struct {
	guildID    string
	token      string
	panelAdmin bool

	viewMux     sync.Mutex
	category    string
	openSetting string
}

func (session *panelSession) currentCategory() string {
	session.viewMux.Lock()
	defer session.viewMux.Unlock()
	return session.category
}

func (session *panelSession) setCategory(category string) {
	session.viewMux.Lock()
	defer session.viewMux.Unlock()
	session.category = category
	session.openSetting = ""
}

func (session *panelSession) currentOpenSetting() string {
	session.viewMux.Lock()
	defer session.viewMux.Unlock()
	return session.openSetting
}

func (session *panelSession) setOpenSetting(key string) {
	session.viewMux.Lock()
	defer session.viewMux.Unlock()
	session.openSetting = key
}

func (session *panelSession) render() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	session.viewMux.Lock()
	category, open := session.category, session.openSetting
	session.viewMux.Unlock()

	view := newPanelView(session.guildID, category, open, session.token, session.panelAdmin)
	return buildSettingsEmbed(view), buildSettingsComponents(view)
}

func canEditAdminSettings(s *discordgo.Session, guildID string, member *discordgo.Member) bool {
	if member == nil || member.User == nil {
		return false
	}
	return config.IsAdmin(member.User.ID) || discord.IsGuildAdmin(s, guildID, member)
}

func requestedCategory(i *discordgo.InteractionCreate, isAdmin bool) string {
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		requested := strings.ToLower(strings.TrimSpace(options[0].StringValue()))
		if isKnownCategory(requested) && len(settingsInCategory(requested, isAdmin)) > 0 {
			return requested
		}
	}
	return defaultCategory(isAdmin)
}

func HandleSettingsPanel(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	isAdmin := canEditAdminSettings(s, i.GuildID, i.Member)

	session := &panelSession{
		guildID:    i.GuildID,
		token:      discord.NewComponentToken(),
		panelAdmin: isAdmin,
		category:   requestedCategory(i, isAdmin),
	}

	embed, components := session.render()

	removeHandler := s.AddHandler(func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		handlePanelInteraction(s, ic, session)
	})

	panelMsg, err := discord.RespondEmbedWithComponents(s, i, embed, components)
	if err != nil {
		removeHandler()
		logger.Errorf("Failed to send the settings panel: %v", err)
		return err
	}

	go expireSettingsPanel(s, i, panelMsg, session, removeHandler)
	return nil
}

func expireSettingsPanel(s *discordgo.Session, i *discordgo.InteractionCreate, panelMsg *discordgo.Message, session *panelSession, removeHandler func()) {
	defer removeHandler()

	<-time.After(settingsPanelExpiry)

	panelMsg = discord.ResolvePanelMessage(s, i, panelMsg)
	if panelMsg == nil {
		return
	}

	embed, _ := session.render()
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         panelMsg.ID,
		Channel:    panelMsg.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{},
	}); err != nil {
		logger.Errorf("Failed to close the settings panel: %v", err)
	}
}
