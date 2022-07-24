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
	EventChannelParentID       string
	ConfigurationWasRun        bool
	FirstReconcileRun          bool

	// TODO: DM Server Owner on add to explain how to get started.
}

func (g *Guild) GetNewEventChannelMessage(eventName string, inviteCode string, eventID string) string {
	return fmt.Sprintf("%s\n%s",
		strings.Replace(g.NewEventChannelMessage, "%EVENT%", eventName, -1),
		getEventInviteURL(inviteCode, eventID),
	)
}
