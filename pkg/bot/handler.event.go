package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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
		err := em.Reconcile(ctx, session, existingGuild.ID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (em *EventManager) Reconcile(ctx context.Context, session *discordgo.Session, existingGuildID string) error {
	var existingEvents []*Event
	err := em.engine.Context(ctx).Table(&Event{}).Where("guild_id = ?", existingGuildID).Find(&existingEvents)
	if err != nil {
		return err
	}

	var existingEventsMap = map[string]*Event{}
	for i := range existingEvents {
		existingEventsMap[existingEvents[i].ID] = existingEvents[i]
	}

	channels, err := session.GuildChannels(existingGuildID)
	if err != nil {
		return err
	}

	channelNameMap := map[string]*discordgo.Channel{}
	for i := range channels {
		channelNameMap[channels[i].Name] = channels[i]
	}

	events, err := session.GuildScheduledEvents(existingGuildID, false)
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

				err = em.createChannelForEvent(session, existingEvent, event.Name)
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
			if isDiscordErrNotFound(err) {
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
		if err != nil && !isDiscordErrNotFound(err) {
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
	_, err := session.ApplicationCommandCreate(session.State.User.ID, "", &cmdNewEventChannelMessage)
	if err != nil {
		return err
	}

	return nil
}

func (em *EventManager) ConsumeSession(session *discordgo.Session) {
	session.AddHandler(func(s *discordgo.Session, m *discordgo.GuildCreate) {
		em.logger.Debug("received guild create event")

		err := em.possiblyCreateGuild(s, m.ID)
		if err != nil {
			em.logger.WithError(err).Error("failed to create guild")
		}
	})

	session.AddHandler(func(s *discordgo.Session, m *discordgo.GuildScheduledEventCreate) {
		em.logger.Debug("received create event")

		err := em.possiblyCreateGuild(s, m.GuildID)
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

		err := em.possiblyCreateGuild(s, m.GuildID)
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

		err := em.possiblyCreateGuild(s, m.GuildID)
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

		var err error
		switch i.ApplicationCommandData().Name {
		case cmdNewEventChannelMessage.Name:
			message := options[cmdNewEventChannelMessage.Options[0].Name]

			_, err := em.engine.ID(i.GuildID).Update(&Guild{NewEventChannelMessage: message.StringValue()})
			if err != nil {
				em.logger.WithError(err).Error("failed to update guild message")
			}
			err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Message has been set!",
				},
			})
		}
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

	err = em.createChannelForEvent(s, evt, m.Name)
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

	_, err = s.ChannelDelete(existingEvent.ChannelID)
	if err != nil {
		return err
	}

	_, err = em.engine.Delete(existingEvent)
	if err != nil {
		return err
	}

	return nil
}

func (em *EventManager) createChannelForEvent(s *discordgo.Session, evt *Event, name string) error {
	channel, err := s.GuildChannelCreate(evt.GuildID, eventChannelName(name), discordgo.ChannelTypeGuildText)
	if err != nil {
		return err
	}

	evt.ChannelID = channel.ID
	_, err = em.engine.ID(evt.ID).Update(evt)
	if err != nil {
		return err
	}

	var guild Guild
	has, err := em.engine.ID(evt.GuildID).Get(&guild)
	if err != nil {
		return err
	}

	message := "@here a new event has been created!"
	if has {
		message = guild.NewEventChannelMessage
	}

	if message == "" {
		return nil
	}

	_, err = s.ChannelMessageSend(channel.ID, message)
	if err != nil {
		return err
	}

	return nil
}

func (em *EventManager) possiblyCreateGuild(_ *discordgo.Session, guildId string) error {
	_, err := em.engine.InsertOne(&Guild{
		ID:                     guildId,
		NewEventChannelMessage: "@here a new event has been created!",
	})
	if err != nil {
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) {
			return err
		}

		if pqErr.Code != "23505" {
			return err
		}
	}

	return nil
}

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
			Required:    true,
			MaxLength:   255,
		},
	},
}

func eventChannelName(name string) string {
	return "event-" + name
}

func isDiscordErrNotFound(err error) bool {
	if err == nil {
		return false
	}

	var discordErr *discordgo.RESTError
	if !errors.As(err, &discordErr) {
		return false
	}

	return discordErr.Response.StatusCode == http.StatusNotFound
}
