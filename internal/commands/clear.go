package commands

import (
	"github.com/bwmarrin/discordgo"
)

// HandleClear is an alias for HandleStop.
func HandleClear(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	return HandleStop(s, i)
}
