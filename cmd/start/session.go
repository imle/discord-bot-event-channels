package start

import (
	"github.com/bwmarrin/discordgo"
)

type DiscordSessionConfig struct {
	Token string
}

func NewDiscordGoSession(cfg DiscordSessionConfig) (*discordgo.Session, error) {
	s, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, err
	}

	return s, nil
}
