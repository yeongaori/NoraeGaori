package commands

import (
	"github.com/bwmarrin/discordgo"
)

func HandleClear(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	return HandleStop(s, i)
}
