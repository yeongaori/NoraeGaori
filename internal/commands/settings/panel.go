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
	messageID  string
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

	return buildSettingsEmbed(session.guildID, category, session.panelAdmin),
		buildSettingsComponents(session.guildID, category, open, session.token, session.panelAdmin)
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
	msg, err := discord.RespondEmbedWithComponents(s, i, embed, components)
	if err != nil {
		logger.Errorf("Failed to send the settings panel: %v", err)
		return err
	}

	session.messageID = msg.ID
	go runSettingsPanel(s, msg, session)
	return nil
}

func runSettingsPanel(s *discordgo.Session, panelMsg *discordgo.Message, session *panelSession) {
	removeHandler := s.AddHandler(func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		handlePanelInteraction(s, ic, session)
	})
	defer removeHandler()

	<-time.After(settingsPanelExpiry)

	embed, _ := session.render()
	s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         panelMsg.ID,
		Channel:    panelMsg.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{},
	})
}
