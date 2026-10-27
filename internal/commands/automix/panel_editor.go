package automix

import (
	"fmt"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/discord"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
)

func describeCategories(guildID string, categories []transition.Category, describe func(category transition.Category) string) string {
	lines := make([]string, 0, len(categories))
	for _, category := range categories {
		lines = append(lines, categoryLabel(guildID, category)+": "+describe(category))
	}
	return strings.Join(lines, "\n")
}

func compatibilityLabel(panel *messages.AutoMixPanelMessages, row *transitionRow) string {
	compatibility := panel.Unknown
	if delta, distance, ok := analysis.Compare(row.fromAnalysis, row.toAnalysis); ok {
		compatibility = fmt.Sprintf(panel.BPMDelta, delta*100)
		if distance >= 0 {
			verdict := panel.Clashing
			if distance <= 1 {
				verdict = panel.Harmonic
			}
			compatibility += fmt.Sprintf(" · %s (%s)", fmt.Sprintf(panel.FifthsDistance, distance), verdict)
		}
	}
	return compatibility
}

func trackFields(guildID string, panel *messages.AutoMixPanelMessages, row *transitionRow) (*discordgo.MessageEmbedField, *discordgo.MessageEmbedField) {
	incoming := &discordgo.MessageEmbedField{Name: panel.IncomingField, Value: panel.OutroField}
	if !row.isOutro() {
		incoming = &discordgo.MessageEmbedField{
			Name: fmt.Sprintf("%s (#%d)", panel.IncomingField, row.toIndex+1),
			Value: fmt.Sprintf("%s\n%s",
				messages.EscapeMarkdown(discord.TruncateRunes(row.toSong.Title, 80)),
				describeTrack(guildID, row.toAnalysis, row.toAnalyzing)),
		}
	}

	outgoing := &discordgo.MessageEmbedField{
		Name: fmt.Sprintf("%s (#%d)", panel.OutgoingField, row.fromIndex+1),
		Value: fmt.Sprintf("%s\n%s",
			messages.EscapeMarkdown(discord.TruncateRunes(row.fromSong.Title, 80)),
			describeTrack(guildID, row.fromAnalysis, row.fromAnalyzing)),
	}
	return outgoing, incoming
}

func createEditorEmbed(guildID string, row *transitionRow, tab *editorTab, errorMessage string) *discordgo.MessageEmbed {
	panel := &messages.T(guildID).AutoMixPanel
	categories := row.tabCategories(tab)

	outgoing, incoming := trackFields(guildID, panel, row)
	fields := []*discordgo.MessageEmbedField{
		outgoing,
		incoming,
		{Name: panel.CompatibilityField, Value: compatibilityLabel(panel, row)},
	}
	if overlap := describeOverlap(guildID, row); len(overlap) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{Name: panel.OverlapField, Value: strings.Join(overlap, "\n")})
	}
	fields = append(fields,
		&discordgo.MessageEmbedField{
			Name: panel.AutoRecipeField,
			Value: describeCategories(guildID, categories, func(category transition.Category) string {
				return autoValueLabel(guildID, row, category)
			}),
		},
		&discordgo.MessageEmbedField{
			Name: panel.EffectiveField,
			Value: describeCategories(guildID, categories, func(category transition.Category) string {
				return fmt.Sprintf("%s (%s)", effectiveValueLabel(guildID, row, category), sourceLabel(guildID, row.source(category)))
			}),
		},
	)
	if errorMessage != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:  messages.T(guildID).Titles.Error,
			Value: discord.TruncateRunes(errorMessage, 1024),
		})
	}

	return &discordgo.MessageEmbed{
		Color:  messages.ColorInfo,
		Title:  discord.TruncateRunes(fmt.Sprintf(panel.EditorTitle, transitionLabel(guildID, row.transitionPair)), 256),
		Fields: fields,
	}
}

func createEditorControls(guildID string, row *transitionRow, tab *editorTab, location *panelLocation) []discordgo.MessageComponent {
	panel := &messages.T(guildID).AutoMixPanel
	songArgument := strconv.Itoa(row.fromSong.ID)
	pageArgument := strconv.Itoa(location.page)
	categories := row.tabCategories(tab)

	components := make([]discordgo.MessageComponent, 0, len(categories)+1)
	for _, category := range categories {
		current := row.override(category)
		prefix := categoryLabel(guildID, category) + ": "
		values := transition.StyleValues(category)

		options := make([]discordgo.SelectMenuOption, 0, len(values))
		options = append(options, discordgo.SelectMenuOption{
			Label:       discord.TruncateRunes(prefix+panel.AutoOption, discordLabelLimit),
			Description: discord.TruncateRunes(fmt.Sprintf(panel.AutoOptionDesc, autoValueLabel(guildID, row, category)), discordLabelLimit),
			Value:       transition.StyleAuto,
			Default:     current == transition.StyleAuto,
		})
		for _, style := range values[1:] {
			options = append(options, discordgo.SelectMenuOption{
				Label:   discord.TruncateRunes(prefix+styleLabel(guildID, category, style), discordLabelLimit),
				Value:   style,
				Default: current == style,
			})
		}

		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    discord.ComponentID(transitionStyleRoute, string(category), songArgument, location.messageID, pageArgument),
					Placeholder: categoryLabel(guildID, category),
					Options:     options,
				},
			},
		})
	}

	return append(components, editorTabRow(guildID, row, tab, songArgument, location))
}

func editorTabRow(guildID string, row *transitionRow, tab *editorTab, songArgument string, location *panelLocation) discordgo.ActionsRow {
	pageArgument := strconv.Itoa(location.page)
	buttons := make([]discordgo.MessageComponent, 0, len(editorTabs))
	for index := range editorTabs {
		candidate := &editorTabs[index]
		style := discordgo.SecondaryButton
		if candidate == tab {
			style = discordgo.PrimaryButton
		}
		buttons = append(buttons, discordgo.Button{
			Label:    tabLabel(guildID, candidate.key),
			Style:    style,
			CustomID: discord.ComponentID(transitionTabRoute, candidate.key, songArgument, location.messageID, pageArgument),
			Disabled: candidate == tab || (row.isOutro() && candidate.key == tabIncoming),
		})
	}
	return discordgo.ActionsRow{Components: buttons}
}
