package bot

import (
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

func (g *Guild) GetNewEventChannelMessage(eventName string) string {
	return strings.Replace(g.NewEventChannelMessage, "%EVENT%", eventName, -1)
}
