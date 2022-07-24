package bot

import (
	"github.com/bwmarrin/discordgo"
)

var globalCommands = []*discordgo.ApplicationCommand{
	&cmdOptions,
	&cmdSync,
}

var dmPermission = false
var defaultMemberPermissions int64 = discordgo.PermissionManageServer

type ConfigOption = string

const (
	ConfigOptionAnnounceMessage            ConfigOption = "announce-message"
	ConfigOptionAnnounceChannel            ConfigOption = "announce-channel"
	ConfigOptionDeleteChannelWhenEventDone ConfigOption = "delete-channel-when-event-done"
	ConfigOptionCategoryID                 ConfigOption = "category-channel"
)

var cmdOptions = discordgo.ApplicationCommand{
	Name:                     "event-channels-bot-options",
	Description:              "Set bot options",
	DMPermission:             &dmPermission,
	DefaultMemberPermissions: &defaultMemberPermissions,
	Options: []*discordgo.ApplicationCommandOption{
		{
			Name:        ConfigOptionAnnounceMessage,
			Description: "The message",
			Type:        discordgo.ApplicationCommandOptionString,
			MaxLength:   255,
		},
		{
			Name:         ConfigOptionAnnounceChannel,
			Description:  "The channel",
			Type:         discordgo.ApplicationCommandOptionChannel,
			ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText},
		},
		{
			Name:        ConfigOptionDeleteChannelWhenEventDone,
			Description: "Whether or not to delete",
			Type:        discordgo.ApplicationCommandOptionBoolean,
		},
		{
			Name:         ConfigOptionCategoryID,
			Description:  "The category channel to add the event channel to",
			Type:         discordgo.ApplicationCommandOptionChannel,
			ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildCategory},
		},
	},
}

func getConfigOptionsMap(s *discordgo.Session, options map[string]*discordgo.ApplicationCommandInteractionDataOption, g *Guild) (errMessage string) {
	message := options[ConfigOptionAnnounceMessage]
	channel := options[ConfigOptionAnnounceChannel]
	shouldDelete := options[ConfigOptionDeleteChannelWhenEventDone]
	category := options[ConfigOptionCategoryID]

	if message != nil {
		g.NewEventChannelMessage = message.StringValue()
	}

	if channel != nil {
		channelValue := channel.ChannelValue(s)
		if channelValue == nil {
			return "not a valid announce channel"
		}
		g.EventAnnouncementChannelID = channelValue.ID
	}

	if shouldDelete != nil {
		g.DeleteWhenDone = shouldDelete.BoolValue()
	}

	if category != nil {
		channelValue := category.ChannelValue(s)
		if channelValue == nil {
			return "not a valid category channel"
		}
		g.EventChannelParentID = channelValue.ID
	}

	return ""
}

var cmdSync = discordgo.ApplicationCommand{
	Name:                     "event-channels-sync",
	Description:              "Run the initial sync",
	DMPermission:             &dmPermission,
	DefaultMemberPermissions: &defaultMemberPermissions,
	Options:                  []*discordgo.ApplicationCommandOption{},
}
