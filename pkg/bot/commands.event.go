package bot

import (
	"github.com/bwmarrin/discordgo"
)

var dmPermission = false
var defaultMemberPermissions int64 = discordgo.PermissionManageServer

var cmdNewEventChannelMessage = discordgo.ApplicationCommand{
	Name:                     "new-event-channel-message",
	Description:              "Message to send when a new channel is created",
	DMPermission:             &dmPermission,
	DefaultMemberPermissions: &defaultMemberPermissions,
	Options: []*discordgo.ApplicationCommandOption{
		{
			Name:        "message",
			Description: "The message",
			Type:        discordgo.ApplicationCommandOptionString,
			Required:    false,
			MaxLength:   255,
		},
	},
}

var cmdDeleteWhenDone = discordgo.ApplicationCommand{
	Name:                     "event-delete-when-done",
	Description:              "Delete the event channel when the event is finished or canceled",
	DMPermission:             &dmPermission,
	DefaultMemberPermissions: &defaultMemberPermissions,
	Options: []*discordgo.ApplicationCommandOption{
		{
			Name:        "value",
			Description: "Whether or not to delete",
			Type:        discordgo.ApplicationCommandOptionBoolean,
			Required:    true,
		},
	},
}
