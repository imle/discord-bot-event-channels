package start

import (
	"fmt"

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

	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		fmt.Println("Bot is ready")
	})

	return s, nil
}
