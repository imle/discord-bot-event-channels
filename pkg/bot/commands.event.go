package bot

import (
	"github.com/bwmarrin/discordgo"
)

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

// TODO: Build reconcile command (don't do it on add).
