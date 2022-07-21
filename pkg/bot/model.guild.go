package bot

import (
	"fmt"
	"strings"
)

type Guild struct {
	ID                         string `xorm:"pk"`
	NewEventChannelMessage     string
	DeleteWhenDone             bool
	EventAnnouncementChannelID string
	EventColor                 int
	EventChannelParentID       string
}

func (g *Guild) GetNewEventChannelMessage(eventName string, inviteCode string, eventID string) string {
	return fmt.Sprintf("%s\n%s",
		strings.Replace(g.NewEventChannelMessage, "%EVENT%", eventName, -1),
		getEventInviteURL(inviteCode, eventID),
	)
}
