package bot

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/lib/pq"
)

func getEventInviteURL(inviteCode string, eventID string) string {
	return fmt.Sprintf("https://discord.gg/%s?event=%s", inviteCode, eventID)
}

var dash = regexp.MustCompile(`\s+`)

func eventChannelName(name string) string {
	name = string(dash.ReplaceAll([]byte(name), []byte{'-'}))
	return strings.ToLower(name)
}

func isDiscordErrRESTCode(err error, code int) bool {
	restCode, present := getDiscordErrRESTCode(err)
	if present {
		return false
	}

	return restCode == code
}

func getDiscordErrRESTCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}

	var discordErr *discordgo.RESTError
	if !errors.As(err, &discordErr) {
		return 0, false
	}

	return discordErr.Response.StatusCode, true
}

const PGUniqueConstraintViolation = "23505"

func isErrDuplicatePGConstraint(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}

	return pqErr.Code == PGUniqueConstraintViolation
}
