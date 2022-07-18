//go:build wireinject

package start

import (
	"github.com/bwmarrin/discordgo"
	"github.com/google/wire"
	"github.com/sirupsen/logrus"

	"github.com/imle/discord-bot-event-channels/pkg/bot"
)

func InitializeEventManager(_ *logrus.Logger, _ EngineConfig) (*bot.EventManager, error) {
	wire.Build(
		NewEngine,
		bot.NewEventManager,
	)
	return &bot.EventManager{}, nil
}

func InitializeDiscordGoSession(_ *logrus.Logger, _ DiscordSessionConfig) (*discordgo.Session, error) {
	wire.Build(
		NewDiscordGoSession,
	)
	return &discordgo.Session{}, nil
}
