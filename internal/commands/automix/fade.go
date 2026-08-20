package automix

import (
	"fmt"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/discord"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

type fadeArgs struct {
	hasState bool
	enabled  bool
	hasNum   bool
	num      float64
}

func fadeOptionByName(options []*discordgo.ApplicationCommandInteractionDataOption, name string) *discordgo.ApplicationCommandInteractionDataOption {
	for _, opt := range options {
		if opt.Name == name {
			return opt
		}
	}
	return nil
}

func fadeNumericValue(opt *discordgo.ApplicationCommandInteractionDataOption) (float64, bool) {
	if opt == nil {
		return 0, false
	}
	switch v := opt.Value.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func resolveFadeArgs(options []*discordgo.ApplicationCommandInteractionDataOption, numName string) fadeArgs {
	res := fadeArgs{}
	if opt := fadeOptionByName(options, "setting"); opt != nil {
		if s, ok := opt.Value.(string); ok {
			switch s {
			case "on":
				res.hasState = true
				res.enabled = true
			case "off":
				res.hasState = true
				res.enabled = false
			default:
				if f, err := strconv.ParseFloat(s, 64); err == nil {
					res.hasNum = true
					res.num = f
					res.hasState = true
					res.enabled = true
				}
			}
		}
	}
	if opt := fadeOptionByName(options, numName); opt != nil {
		if f, ok := fadeNumericValue(opt); ok {
			res.hasNum = true
			res.num = f
		}
	}
	return res
}

func clampFadeDuration(v float64) float64 {
	if v < 1 {
		return 1
	}
	if v > 30 {
		return 30
	}
	return v
}

func clampAutoMixBeats(v float64) int {
	n := int(v)
	if n < 4 {
		return 4
	}
	if n > 64 {
		return 64
	}
	return n
}

func buildSettingEmbed(guildID string, enabled bool, titleFmt, descFmt, whatTitle, whatDesc, extraName, extraValue string) *discordgo.MessageEmbed {
	t := messages.T(guildID)
	statusText := t.Settings.StatusOff
	color := messages.ColorError
	if enabled {
		statusText = t.Settings.StatusOn
		color = messages.ColorSuccess
	}

	fields := []*discordgo.MessageEmbedField{
		{Name: whatTitle, Value: whatDesc, Inline: false},
	}
	if extraName != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: extraName, Value: extraValue, Inline: true})
	}

	return &discordgo.MessageEmbed{
		Color:       color,
		Title:       fmt.Sprintf(titleFmt, "", statusText),
		Description: fmt.Sprintf(descFmt, statusText),
		Fields:      fields,
	}
}

func resolveToggleState(res fadeArgs, current bool) (enabled bool, changed bool) {
	switch {
	case res.hasState:
		return res.enabled, true
	case res.hasNum:
		return current, false
	default:
		return !current, true
	}
}

func HandleFadeIn(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	guildID := i.GuildID
	t := messages.T(guildID)
	res := resolveFadeArgs(i.ApplicationCommandData().Options, "duration")

	if res.hasNum {
		if err := queue.SetFadeInDuration(guildID, clampFadeDuration(res.num)); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.FadeInError, err)))
			return err
		}
	}

	current, err := queue.GetFadeIn(guildID)
	if err != nil {
		current = false
	}
	enabled, changed := resolveToggleState(res, current)
	if changed {
		if err := queue.SetFadeIn(guildID, enabled); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.FadeInError, err)))
			return err
		}
	}

	duration, err := queue.GetFadeInDuration(guildID)
	if err != nil {
		duration = 3
	}
	embed := buildSettingEmbed(guildID, enabled,
		t.Settings.FadeInTitle, t.Settings.FadeInDesc,
		t.Settings.FadeInWhatTitle, t.Settings.FadeInWhatDesc,
		t.Settings.FadeInDurationLabel, fmt.Sprintf(t.Settings.DurationLabel, duration))
	discord.RespondEmbed(s, i, embed)
	return nil
}

func HandleFadeOut(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	guildID := i.GuildID
	t := messages.T(guildID)
	res := resolveFadeArgs(i.ApplicationCommandData().Options, "duration")

	if res.hasNum {
		if err := queue.SetFadeOutDuration(guildID, clampFadeDuration(res.num)); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.FadeOutError, err)))
			return err
		}
	}

	current, err := queue.GetFadeOut(guildID)
	if err != nil {
		current = false
	}
	enabled, changed := resolveToggleState(res, current)
	if changed {
		if err := queue.SetFadeOut(guildID, enabled); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.FadeOutError, err)))
			return err
		}
	}

	duration, err := queue.GetFadeOutDuration(guildID)
	if err != nil {
		duration = 3
	}
	embed := buildSettingEmbed(guildID, enabled,
		t.Settings.FadeOutTitle, t.Settings.FadeOutDesc,
		t.Settings.FadeOutWhatTitle, t.Settings.FadeOutWhatDesc,
		t.Settings.FadeOutDurationLabel, fmt.Sprintf(t.Settings.DurationLabel, duration))
	discord.RespondEmbed(s, i, embed)
	return nil
}

func HandleAutoMix(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	guildID := i.GuildID
	t := messages.T(guildID)
	res := resolveFadeArgs(i.ApplicationCommandData().Options, "beats")

	if res.hasNum {
		if err := queue.SetAutoMixBeats(guildID, clampAutoMixBeats(res.num)); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.AutoMixError, err)))
			return err
		}
	}

	current, err := queue.GetAutoMix(guildID)
	if err != nil {
		current = false
	}
	enabled, changed := resolveToggleState(res, current)
	if changed {
		if err := queue.SetAutoMix(guildID, enabled); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.AutoMixError, err)))
			return err
		}
	}

	beats, err := queue.GetAutoMixBeats(guildID)
	if err != nil {
		beats = 16
	}
	embed := buildSettingEmbed(guildID, enabled,
		t.Settings.AutoMixTitle, t.Settings.AutoMixDesc,
		t.Settings.AutoMixWhatTitle, t.Settings.AutoMixWhatDesc,
		t.Settings.AutoMixBeatsLabel, strconv.Itoa(beats))
	discord.RespondEmbed(s, i, embed)
	return nil
}

func autoMixStyleOptionValue(options []*discordgo.ApplicationCommandInteractionDataOption, name string) string {
	opt := fadeOptionByName(options, name)
	if opt == nil {
		return ""
	}
	value, ok := opt.Value.(string)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func autoMixStyleFields(guildID string, categories []string) []*discordgo.MessageEmbedField {
	fields := make([]*discordgo.MessageEmbedField, 0, len(categories))
	for _, category := range categories {
		current, err := queue.GetAutoMixStyle(guildID, category)
		if err != nil {
			current = queue.AutoMixStyleAuto
		}
		values := transition.StyleValues(category)
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   category,
			Value:  fmt.Sprintf("**%s**\n%s", current, strings.Join(values, ", ")),
			Inline: false,
		})
	}
	return fields
}

func HandleAutoMixStyle(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	guildID := i.GuildID
	t := messages.T(guildID)
	options := i.ApplicationCommandData().Options

	category := autoMixStyleOptionValue(options, "category")
	style := autoMixStyleOptionValue(options, "style")

	if category == "" {
		discord.RespondEmbed(s, i, &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       t.Settings.AutoMixStyleTitle,
			Description: t.Settings.AutoMixStyleDesc,
			Fields:      autoMixStyleFields(guildID, queue.AutoMixStyleCategories()),
		})
		return nil
	}

	if transition.StyleValues(category) == nil {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
			fmt.Sprintf(t.Settings.AutoMixStyleInvalidCategory, category, strings.Join(queue.AutoMixStyleCategories(), ", "))))
		return nil
	}

	if style == "" {
		discord.RespondEmbed(s, i, &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       t.Settings.AutoMixStyleTitle,
			Description: t.Settings.AutoMixStyleDesc,
			Fields:      autoMixStyleFields(guildID, []string{category}),
		})
		return nil
	}

	if !transition.ValidStyle(category, style) {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
			fmt.Sprintf(t.Settings.AutoMixStyleInvalidValue, style, category,
				strings.Join(transition.StyleValues(category), ", "))))
		return nil
	}

	if err := queue.SetAutoMixStyle(guildID, category, style); err != nil {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.AutoMixStyleError, err)))
		return err
	}

	discord.RespondEmbed(s, i, &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       t.Settings.AutoMixStyleTitle,
		Description: fmt.Sprintf(t.Settings.AutoMixStyleChanged, category, style),
		Fields: []*discordgo.MessageEmbedField{
			{Name: t.Settings.AutoMixStyleWhatTitle, Value: t.Settings.AutoMixStyleWhatDesc, Inline: false},
		},
	})
	return nil
}

func HandleCrossfade(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	guildID := i.GuildID
	t := messages.T(guildID)
	res := resolveFadeArgs(i.ApplicationCommandData().Options, "duration")

	if res.hasNum {
		if err := queue.SetCrossfadeDuration(guildID, clampFadeDuration(res.num)); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.CrossfadeError, err)))
			return err
		}
	}

	current, err := queue.GetCrossfade(guildID)
	if err != nil {
		current = false
	}
	enabled, changed := resolveToggleState(res, current)
	if changed {
		if err := queue.SetCrossfade(guildID, enabled); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.CrossfadeError, err)))
			return err
		}
	}

	duration, err := queue.GetCrossfadeDuration(guildID)
	if err != nil {
		duration = 8
	}
	embed := buildSettingEmbed(guildID, enabled,
		t.Settings.CrossfadeTitle, t.Settings.CrossfadeDesc,
		t.Settings.CrossfadeWhatTitle, t.Settings.CrossfadeWhatDesc,
		t.Settings.CrossfadeDurationLabel, fmt.Sprintf(t.Settings.DurationLabel, duration))
	discord.RespondEmbed(s, i, embed)
	return nil
}

func HandleFadeOnStop(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	guildID := i.GuildID
	t := messages.T(guildID)
	res := resolveFadeArgs(i.ApplicationCommandData().Options, "")

	current, err := queue.GetFadeOnStop(guildID)
	if err != nil {
		current = false
	}
	enabled, changed := resolveToggleState(res, current)
	if changed {
		if err := queue.SetFadeOnStop(guildID, enabled); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.FadeOnStopError, err)))
			return err
		}
	}

	embed := buildSettingEmbed(guildID, enabled,
		t.Settings.FadeOnStopTitle, t.Settings.FadeOnStopDesc,
		t.Settings.FadeOnStopWhatTitle, t.Settings.FadeOnStopWhatDesc,
		"", "")
	discord.RespondEmbed(s, i, embed)
	return nil
}

func HandleTrimSilence(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	guildID := i.GuildID
	t := messages.T(guildID)
	res := resolveFadeArgs(i.ApplicationCommandData().Options, "")

	current, err := queue.GetTrimSilence(guildID)
	if err != nil {
		current = false
	}
	enabled, changed := resolveToggleState(res, current)
	if changed {
		if err := queue.SetTrimSilence(guildID, enabled); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.TrimSilenceError, err)))
			return err
		}
	}

	embed := buildSettingEmbed(guildID, enabled,
		t.Settings.TrimSilenceTitle, t.Settings.TrimSilenceDesc,
		t.Settings.TrimSilenceWhatTitle, t.Settings.TrimSilenceWhatDesc,
		"", "")
	discord.RespondEmbed(s, i, embed)
	return nil
}
