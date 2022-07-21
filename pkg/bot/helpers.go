package bot

import (
	"fmt"
)

func getEventInviteURL(inviteCode string, eventID string) string {
	return fmt.Sprintf("https://discord.gg/%s?event=%s", inviteCode, eventID)
}
