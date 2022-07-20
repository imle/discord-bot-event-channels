package bot

import (
	"github.com/bwmarrin/discordgo"
)

const EventRolePermission = discordgo.PermissionSendMessages &
	discordgo.PermissionSendMessagesInThreads &
	discordgo.PermissionCreatePublicThreads &
	discordgo.PermissionEmbedLinks &
	discordgo.PermissionAttachFiles &
	discordgo.PermissionAddReactions &
	discordgo.PermissionUseExternalEmojis &
	discordgo.PermissionUseExternalStickers &
	discordgo.PermissionMentionEveryone &
	discordgo.PermissionReadMessageHistory
