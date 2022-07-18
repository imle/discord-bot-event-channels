package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"xorm.io/xorm"
	"xorm.io/xorm/names"
)

type EventManager struct {
	logger *logrus.Logger
	engine xorm.EngineInterface
}

func NewEventManager(
	logger *logrus.Logger,
	engine xorm.EngineInterface,
) *EventManager {
	em := &EventManager{
		logger: logger,
		engine: engine,
	}
	//em.engine.ShowSQL(true)

	return em
}

func (em EventManager) SyncDB() error {
	em.engine.SetMapper(names.GonicMapper{})
	if err := em.engine.Sync2(new(Guild)); err != nil {
		return err
	}
	if err := em.engine.Sync2(new(Event)); err != nil {
		return err
	}

	return nil
}

func (em EventManager) ReconcileAll(ctx context.Context, session *discordgo.Session) error {
	var existingGuilds []*Guild
	err := em.engine.Context(ctx).Table(&Guild{}).Find(&existingGuilds)
	if err != nil {
		return err
	}

	for _, existingGuild := range existingGuilds {
		guild, err := session.Guild(existingGuild.ID)
		if err != nil {
			if code, present := getDiscordErrRESTCode(err); present {
				switch code {
				case http.StatusNotFound, http.StatusForbidden:
					_, err = em.engine.Table(&Event{}).Where("guild_id = ?", existingGuild.ID).Delete()
					if err != nil {
						return err
					}
					_, err = em.engine.Table(&Guild{}).Where("id = ?", existingGuild.ID).Delete()
					if err != nil {
						return err
					}
					continue
				}

				return err
			}
		}

		err = em.Reconcile(ctx, session, guild)
		if err != nil {
			return err
		}
	}

	return nil
}

func (em *EventManager) Reconcile(ctx context.Context, session *discordgo.Session, existingGuild *discordgo.Guild) error {
	var existingEvents []*Event
	err := em.engine.Context(ctx).Table(&Event{}).Where("guild_id = ?", existingGuild.ID).Find(&existingEvents)
	if err != nil {
		return err
	}

	var existingEventsMap = map[string]*Event{}
	for i := range existingEvents {
		existingEventsMap[existingEvents[i].ID] = existingEvents[i]
	}

	channels, err := session.GuildChannels(existingGuild.ID)
	if err != nil {
		return err
	}

	channelNameMap := map[string]*discordgo.Channel{}
	for i := range channels {
		channelNameMap[channels[i].Name] = channels[i]
	}

	events, err := session.GuildScheduledEvents(existingGuild.ID, false)
	if err != nil {
		return err
	}

	for _, event := range events {
		existingEvent, has := existingEventsMap[event.ID]
		if !has {
			em.logger.Infof("no existing event found for %s", event.ID)

			existingEvent, err = em.eventCreated(session, event)
			if err != nil {
				return err
			}
		}

		// Remove all found or created keys to see what was deleted
		delete(existingEventsMap, event.ID)

		if existingEvent.ChannelID == "" {
			name := eventChannelName(event.Name)
			channel, found := channelNameMap[name]
			if !found {
				em.logger.Infof("no existing event channel found for %s", event.ID)

				existingEvent.ChannelID, err = em.createChannelForEvent(session, event)
				if err != nil {
					return err
				}
			} else {
				existingEvent.ChannelID = channel.ID
				_, err = em.engine.ID(existingEvent.ID).Update(existingEvent)
				if err != nil {
					return err
				}
			}
		}

		channel, err := session.Channel(existingEvent.ChannelID)
		if err != nil {
			if isDiscordErrRESTCode(err, http.StatusNotFound) {
				continue
			}
			return err
		}

		if event.Name != channel.Name {
			existingEvent, err = em.eventUpdated(session, event)
			if err != nil {
				return err
			}
		}
	}

	for _, event := range existingEventsMap {
		_, err = session.ChannelDelete(event.ChannelID)
		if err != nil && !isDiscordErrRESTCode(err, http.StatusNotFound) {
			return err
		}

		_, err = em.engine.Delete(event)
		if err != nil {
			return err
		}
	}

	return nil
}

func (em *EventManager) RegisterGlobalCommands(session *discordgo.Session) error {
	cmds := []*discordgo.ApplicationCommand{
		&cmdNewEventChannelMessage,
		&cmdDeleteWhenDone,
	}

	for i := range cmds {
		_, err := session.ApplicationCommandCreate(session.State.User.ID, "", cmds[i])
		if err != nil {
			return err
		}
	}

	return nil
}

func (em *EventManager) ConsumeSession(session *discordgo.Session) {
	session.AddHandler(func(s *discordgo.Session, m *discordgo.GuildCreate) {
		em.logger.Debug("received guild create event")

		exists, err := em.possiblyCreateGuild(s, m.ID)
		if err != nil {
			em.logger.WithError(err).Error("failed to create guild")
		}

		if !exists {
			err = em.Reconcile(context.Background(), session, m.Guild)
			if err != nil {
				return
			}
		}
	})

	session.AddHandler(func(s *discordgo.Session, m *discordgo.GuildScheduledEventCreate) {
		em.logger.Debug("received create event")

		_, err := em.possiblyCreateGuild(s, m.GuildID)
		if err != nil {
			em.logger.WithError(err).Error("failed to create guild")
		}
		_, err = em.eventCreated(s, m.GuildScheduledEvent)
		if err != nil {
			em.logger.WithError(err).Error("failed to create event")
		}
	})

	session.AddHandler(func(s *discordgo.Session, m *discordgo.GuildScheduledEventUpdate) {
		em.logger.Debug("received update event")

		_, err := em.possiblyCreateGuild(s, m.GuildID)
		if err != nil {
			em.logger.WithError(err).Error("failed to create guild")
		}

		switch m.Status {
		case discordgo.GuildScheduledEventStatusCompleted, discordgo.GuildScheduledEventStatusCanceled:
			em.logger.Debug("received delete via update event")

			err = em.eventDeleted(s, m.GuildScheduledEvent)
			if err != nil {
				em.logger.WithError(err).Error("failed to delete event")
			}
		default:
			_, err = em.eventUpdated(s, m.GuildScheduledEvent)
			if err != nil {
				em.logger.WithError(err).Error("failed to update event")
			}
		}
	})

	session.AddHandler(func(s *discordgo.Session, m *discordgo.GuildScheduledEventDelete) {
		em.logger.Debug("received delete event")

		_, err := em.possiblyCreateGuild(s, m.GuildID)
		if err != nil {
			em.logger.WithError(err).Error("failed to create guild")
		}
		err = em.eventDeleted(s, m.GuildScheduledEvent)
		if err != nil {
			em.logger.WithError(err).Error("failed to delete event")
		}
	})

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		em.logger.Debug("received interaction")

		optionsSlice := i.ApplicationCommandData().Options
		options := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(optionsSlice))
		for _, opt := range optionsSlice {
			options[opt.Name] = opt
		}

		var reply string
		switch i.ApplicationCommandData().Name {
		case cmdNewEventChannelMessage.Name:
			message := options[cmdNewEventChannelMessage.Options[0].Name]

			_, err := em.engine.ID(i.GuildID).Update(&Guild{NewEventChannelMessage: message.StringValue()})
			if err != nil {
				em.logger.WithError(err).Error("failed to update guild message")
			}
			reply = "Message has been set!"
		case cmdDeleteWhenDone.Name:
			shouldDelete := options[cmdDeleteWhenDone.Options[0].Name]

			_, err := em.engine.Table(&Guild{}).ID(i.GuildID).Update(map[string]interface{}{
				"delete_when_done": shouldDelete.BoolValue(),
			})
			if err != nil {
				em.logger.WithError(err).Error("failed to update guild")
			}
			reply = "Options updated!"
		}

		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: reply,
			},
		})
		if err != nil {
			em.logger.WithError(err).Error("failed to reply to command")
		}
	})
}

func (em *EventManager) eventCreated(s *discordgo.Session, m *discordgo.GuildScheduledEvent) (*Event, error) {
	evt := &Event{
		ID:      m.ID,
		GuildID: m.GuildID,
	}
	_, err := em.engine.InsertOne(evt)
	if err != nil {
		return nil, err
	}

	evt.ChannelID, err = em.createChannelForEvent(s, m)
	if err != nil {
		return nil, err
	}

	return evt, nil
}

func (em *EventManager) eventUpdated(s *discordgo.Session, m *discordgo.GuildScheduledEvent) (*Event, error) {
	existingEvent := &Event{ID: m.ID}
	found, err := em.engine.Get(existingEvent)
	if err != nil {
		return nil, err
	}

	if !found {
		return nil, fmt.Errorf("was not able to find event %s: %q", m.ID, m.Name)
	}

	_, err = s.ChannelEdit(existingEvent.ChannelID, eventChannelName(m.Name))
	if err != nil {
		return nil, err
	}

	return existingEvent, nil
}

func (em *EventManager) eventDeleted(s *discordgo.Session, m *discordgo.GuildScheduledEvent) error {
	existingEvent := &Event{ID: m.ID}
	found, err := em.engine.Get(existingEvent)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("was not able to find event %s: %q", m.ID, m.Name)
	}

	guild := &Guild{ID: m.GuildID}
	found, err = em.engine.Get(guild)
	if err != nil {
		return err
	}

	if guild.DeleteWhenDone {
		_, err = s.ChannelDelete(existingEvent.ChannelID)
	} else {
		_, err = s.ChannelEdit(existingEvent.ChannelID, "done-"+eventChannelName(m.Name))
	}
	if err != nil {
		return err
	}

	_, err = em.engine.Delete(existingEvent)
	if err != nil {
		return err
	}

	return nil
}

func (em *EventManager) createChannelForEvent(s *discordgo.Session, evt *discordgo.GuildScheduledEvent) (string, error) {
	channels, err := s.GuildChannels(evt.GuildID)
	if err != nil {
		return "", err
	}

	var eventsCategory *discordgo.Channel
	for i := range channels {
		if channels[i].Type != discordgo.ChannelTypeGuildCategory {
			continue
		}

		if strings.ToLower(channels[i].Name) != "events" {
			continue
		}

		eventsCategory = channels[i]
		break
	}
	if eventsCategory == nil {
		eventsCategory, err = s.GuildChannelCreateComplex(evt.GuildID, discordgo.GuildChannelCreateData{
			Name:     "EVENTS",
			Type:     discordgo.ChannelTypeGuildCategory,
			Position: 1,
		})
		if err != nil {
			return "", err
		}
	}

	channel, err := s.GuildChannelCreateComplex(evt.GuildID, discordgo.GuildChannelCreateData{
		Name:     eventChannelName(evt.Name),
		Type:     discordgo.ChannelTypeGuildText,
		ParentID: eventsCategory.ID,
	})
	if err != nil {
		return "", err
	}

	_, err = em.engine.ID(evt.ID).Update(&Event{
		ID:        evt.ID,
		GuildID:   evt.GuildID,
		ChannelID: channel.ID,
	})
	if err != nil {
		return "", err
	}

	var guild Guild
	has, err := em.engine.ID(evt.GuildID).Get(&guild)
	if err != nil {
		return "", err
	}

	message := "@here a new event has been created!"
	if has {
		message = guild.NewEventChannelMessage
	}

	if message == "" {
		return channel.ID, nil
	}

	_, err = s.ChannelMessageSend(channel.ID, message)
	if err != nil {
		return "", err
	}

	return channel.ID, nil
}

func (em *EventManager) possiblyCreateGuild(_ *discordgo.Session, guildId string) (exists bool, err error) {
	_, err = em.engine.InsertOne(&Guild{
		ID:                     guildId,
		NewEventChannelMessage: "@here a new event has been created!",
	})
	if err != nil {
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) {
			return false, err
		}

		if pqErr.Code == "23505" {
			return true, nil
		} else {
			return false, err
		}
	}

	return false, nil
}

var strip = regexp.MustCompile(`[^\w\d\s]+`)
var dash = regexp.MustCompile(`\s+`)

func eventChannelName(name string) string {
	name = string(strip.ReplaceAll([]byte(name), []byte{}))
	name = string(dash.ReplaceAll([]byte(name), []byte{'-'}))
	return name
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
