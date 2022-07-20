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

func (em *EventManager) ConsumeSession(s *discordgo.Session) {
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log := em.logger.WithFields(logrus.Fields{
			"method": "Ready",
		})

		log.Debug("received")

		err := em.onReady(context.TODO(), s, r)
		if err != nil {
			log.WithError(err).Error("failed")
			return
		}
	})

	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildCreate) {
		log := em.logger.WithFields(logrus.Fields{
			"method":   "GuildCreate",
			"guild_id": m.ID,
		})

		log.Debug("received")

		err := em.onGuildCreate(context.TODO(), s, m)
		if err != nil {
			log.WithError(err).Error("failed guild create")
			return
		}
	})

	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildScheduledEventCreate) {
		log := em.logger.WithFields(logrus.Fields{
			"method":   "GuildScheduledEventCreate",
			"guild_id": m.GuildID,
			"event_id": m.ID,
		})

		log.Debug("received")

		err := em.onGuildEventCreate(context.TODO(), log, s, m)
		if err != nil {
			log.WithError(err).Error("failed")
			return
		}
	})

	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildScheduledEventUpdate) {
		log := em.logger.WithFields(logrus.Fields{
			"method":   "GuildScheduledEventUpdate",
			"guild_id": m.GuildID,
			"event_id": m.ID,
		})

		log.Debug("received")

		err := em.onGuildEventUpdate(context.TODO(), log, s, m)
		if err != nil {
			log.WithError(err).Error("failed")
			return
		}
	})

	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildScheduledEventDelete) {
		log := em.logger.WithFields(logrus.Fields{
			"method":   "GuildScheduledEventDelete",
			"guild_id": m.GuildID,
			"event_id": m.ID,
		})

		log.Debug("received")

		err := em.onGuildEventDelete(context.TODO(), log, s, m)
		if err != nil {
			log.WithError(err).Error("failed")
			return
		}
	})

	s.AddHandler(func(s *discordgo.Session, m *discordgo.ChannelUpdate) {
		// TODO: ? not sure why this is here.
	})

	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildScheduledEventUserAdd) {
		log := em.logger.WithFields(logrus.Fields{
			"method":   "GuildScheduledEventUserAdd",
			"guild_id": m.GuildID,
			"event_id": m.GuildScheduledEventID,
			"user_id":  m.UserID,
		})

		log.Debug("received")

		// Add to role
		err := em.onGuildEventUserAdd(context.TODO(), s, m)
		if err != nil {
			log.WithError(err).Error("failed")
			return
		}
	})

	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildScheduledEventUserRemove) {
		log := em.logger.WithFields(logrus.Fields{
			"method":   "GuildScheduledEventUserRemove",
			"guild_id": m.GuildID,
			"event_id": m.GuildScheduledEventID,
			"user_id":  m.UserID,
		})

		log.Debug("received")

		// Remove from role
		err := em.onGuildEventUserRemove(context.TODO(), s, m)
		if err != nil {
			log.WithError(err).Error("failed")
			return
		}
	})

	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		log := em.logger.WithFields(logrus.Fields{
			"method":         "InteractionCreate",
			"guild_id":       i.GuildID,
			"user_id":        i.User.ID,
			"interaction_id": i.ID,
		})

		log.Debug("received")

		err := em.handleInteraction(s, i)
		if err != nil {
			log.WithError(err).Error("failed")
			return
		}
	})
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

// TODO: Refactor
func (em *EventManager) reconcile(ctx context.Context, session *discordgo.Session, guild *discordgo.Guild) error {
	var internalEvents []*Event
	err := em.engine.Context(ctx).Table(&Event{}).Where("guild_id = ?", guild.ID).Find(&internalEvents)
	if err != nil {
		return err
	}

	var internalEventsMap = map[string]*Event{}
	for i := range internalEvents {
		internalEventsMap[internalEvents[i].ID] = internalEvents[i]
	}

	channels, err := session.GuildChannels(guild.ID)
	if err != nil {
		return err
	}

	channelIDMap := map[string]*discordgo.Channel{}
	for i := range channels {
		channelIDMap[channels[i].ID] = channels[i]
	}

	channelNameMap := map[string]*discordgo.Channel{}
	for i := range channels {
		channelNameMap[channels[i].Name] = channels[i]
	}

	events, err := session.GuildScheduledEvents(guild.ID, false)
	if err != nil {
		return err
	}

	for _, event := range events {
		// Remove all found or created keys to see what was deleted.
		delete(internalEventsMap, event.ID)

		internalEvent, has := internalEventsMap[event.ID]
		log := em.logger.WithFields(logrus.Fields{
			"method":   "Reconcile",
			"guild_id": event.GuildID,
			"event_id": event.ID,
		})

		if has {
			var missing = make([]string, 0, 3)
			if internalEvent.ChannelID == "" {
				missing = append(missing, "ChannelID")
				// Delete the event and re-create it
				_, err := em.engine.Table(&Event{}).ID(event.ID).Delete()
				if err != nil {
					log.Errorf("failed to delete event channel")
				}
				has = false
			}
			if internalEvent.RoleID == "" {
				missing = append(missing, "RoleID")
				log.Warn("found internal event with missing RoleID")
			}
			if internalEvent.AnnounceMessageID == "" {
				missing = append(missing, "AnnounceMessageID")
				log.Warn("found internal event with missing AnnounceMessageID")
			}
			log.Warnf("found internal event with missing (%s)", strings.Join(missing, ", "))
		}

		if !has {
			err := em.onGuildEventCreate(ctx, log, session, &discordgo.GuildScheduledEventCreate{
				GuildScheduledEvent: event,
			})
			if err != nil {
				log.Errorf("failed to create event channel")
			}

			continue
		}

		channel, err := session.Channel(internalEvent.ChannelID)
		if err != nil {
			if isDiscordErrRESTCode(err, http.StatusNotFound) {
				continue
			}
			return err
		}

		if event.Name != channel.Name {
			_, err = session.ChannelEdit(internalEvent.ChannelID, eventChannelName(event.Name))
			if err != nil {
				return fmt.Errorf("failed to update channel name: %w", err)
			}
		}
	}

	for _, event := range internalEventsMap {
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

// Ensures we reconcile all discordgo.Guild after a restart.
func (em *EventManager) onReady(ctx context.Context, s *discordgo.Session, r *discordgo.Ready) error {
	for _, guild := range r.Guilds {
		err := em.reconcile(ctx, s, guild)
		if err != nil {
			return fmt.Errorf("failed to reconcile %q: %w", guild.ID, err)
		}
	}

	return nil
}

// When a discordgo.Guild is added to the service that we reconcile it.
func (em *EventManager) onGuildCreate(ctx context.Context, s *discordgo.Session, m *discordgo.GuildCreate) error {
	_, exists, err := em.possiblyCreateGuild(ctx, m.ID)
	if err != nil {
		return fmt.Errorf("failed to create guild: %w", err)
	}

	// We don't want to reconcile if we already have the Guild recorded as this is just an update.
	if exists {
		return nil
	}

	err = em.reconcile(ctx, s, m.Guild)
	if err != nil {
		return fmt.Errorf("failed to reconcile guild: %w", err)
	}

	return nil
}

// Create a discordgo.Role and discordgo.Channel for the event then put a discordgo.Message in the
// discordgo.Guild's specified EventAnnouncement discordgo.Channel.
func (em *EventManager) onGuildEventCreate(ctx context.Context, log *logrus.Entry, s *discordgo.Session, m *discordgo.GuildScheduledEventCreate) (err error) {
	var role *discordgo.Role
	var channel *discordgo.Channel
	var message *discordgo.Message
	var event *Event

	var guild *Guild
	guild, _, err = em.possiblyCreateGuild(ctx, m.GuildID)
	if err != nil {
		return fmt.Errorf("failed to create guild: %w", err)
	}

	// Try to clean up our mess if anything failed.
	defer func() {
		if err == nil {
			return
		}

		if role != nil {
			if err := s.GuildRoleDelete(m.GuildID, role.ID); err != nil {
				log.WithError(err).Errorf("failed to cleanup role %q", role.ID)
			}
		}
		if channel != nil {
			if _, err := s.ChannelDelete(channel.ID); err != nil {
				log.WithError(err).Errorf("failed to cleanup channel %q", channel.ID)
			}
		}
		if message != nil {
			if err := s.ChannelMessageDelete(guild.ID, message.ID); err != nil {
				log.WithError(err).Errorf("failed to cleanup announce message %q", message.ID)
			}
		}
		if event != nil {
			if _, err := em.engine.Table(&Event{}).ID(m.ID).Delete(); err != nil {
				log.WithError(err).Errorf("failed to cleanup internal event")
			}
		}
	}()

	// TODO: Make a PR to allow for giving a struct with create options.
	role, err = s.GuildRoleCreate(m.GuildID)
	if err != nil {
		return fmt.Errorf("failed to create role: %w", err)
	}

	// Update the role with actual values we want.
	role, err = s.GuildRoleEdit(m.GuildID, role.ID, eventChannelName(m.Name), guild.EventColor, false, 0, false)
	if err != nil {
		return fmt.Errorf("failed to edit role: %w", err)
	}

	channel, err = s.GuildChannelCreateComplex(m.GuildID, discordgo.GuildChannelCreateData{
		Name: eventChannelName(m.Name),
		Type: discordgo.ChannelTypeGuildText,
	})
	if err != nil {
		return fmt.Errorf("failed to create channel: %w", err)
	}

	err = s.ChannelPermissionSet(channel.ID, "@everyone", discordgo.PermissionOverwriteTypeRole, 0, discordgo.PermissionViewChannel)
	if err != nil {
		return fmt.Errorf("failed to hide channel: %w", err)
	}

	err = s.ChannelPermissionSet(channel.ID, role.ID, discordgo.PermissionOverwriteTypeRole, EventRolePermission, 0)
	if err != nil {
		return fmt.Errorf("failed to add permissions to channel: %w", err)
	}

	message, err = s.ChannelMessageSend(guild.EventAnnouncementChannelID, guild.GetNewEventChannelMessage(m.Name))
	if err != nil {
		return fmt.Errorf("failed to announce channel: %w", err)
	}

	event = &Event{
		ID:                m.ID,
		GuildID:           m.GuildID,
		RoleID:            role.ID,
		ChannelID:         channel.ID,
		AnnounceMessageID: message.ID,
	}
	_, err = em.engine.InsertOne(event)
	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	return nil
}

// Check to see if the discordgo.GuildScheduledEvent ended and if so, remove it, otherwise update the name if it changed.
func (em *EventManager) onGuildEventUpdate(ctx context.Context, log *logrus.Entry, s *discordgo.Session, m *discordgo.GuildScheduledEventUpdate) error {
	guild, event, err := em.getGuildAndEvent(ctx, m.GuildScheduledEvent.GuildID, m.GuildScheduledEvent.ID)
	if err != nil {
		return err
	}

	switch m.Status {
	case discordgo.GuildScheduledEventStatusCompleted, discordgo.GuildScheduledEventStatusCanceled:
		log.Debug("received delete via update event")

		return em.deleteEvent(ctx, log, s, guild, event, m.GuildScheduledEvent)
	default:
		_, err = s.ChannelEdit(event.ChannelID, eventChannelName(m.Name))
		if err != nil {
			return fmt.Errorf("failed to update channel name: %w", err)
		}
	}

	return nil
}

// The discordgo.GuildScheduledEvent was canceled so we delete its internal representation.
func (em *EventManager) onGuildEventDelete(ctx context.Context, log *logrus.Entry, s *discordgo.Session, m *discordgo.GuildScheduledEventDelete) error {
	guild, event, err := em.getGuildAndEvent(ctx, m.GuildScheduledEvent.GuildID, m.GuildScheduledEvent.ID)
	if err != nil {
		return err
	}

	err = em.deleteEvent(ctx, log, s, guild, event, m.GuildScheduledEvent)
	if err != nil {
		return fmt.Errorf("failed to delete event: %w", err)
	}

	return nil
}

// Add the discordgo.User to the discordgo.Role for the discordgo.Event to allow them to see the discordgo.Channel.
func (em *EventManager) onGuildEventUserAdd(ctx context.Context, s *discordgo.Session, m *discordgo.GuildScheduledEventUserAdd) error {
	_, event, err := em.getGuildAndEvent(ctx, m.GuildID, m.GuildScheduledEventID)
	if err != nil {
		return err
	}

	err = s.GuildMemberRoleAdd(m.GuildID, m.UserID, event.RoleID)
	if err != nil {
		return fmt.Errorf("failed to add user to event role: %w", err)
	}

	return nil
}

func (em *EventManager) onGuildEventUserRemove(ctx context.Context, s *discordgo.Session, m *discordgo.GuildScheduledEventUserRemove) error {
	_, event, err := em.getGuildAndEvent(ctx, m.GuildID, m.GuildScheduledEventID)
	if err != nil {
		return err
	}

	err = s.GuildMemberRoleRemove(m.GuildID, m.UserID, event.RoleID)
	if err != nil {
		return fmt.Errorf("failed to remove user to event role: %w", err)
	}

	return nil
}

func (em *EventManager) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) error {
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
			reply = "Failed to update."
		} else {
			reply = "Message has been set!"
		}
	case cmdDeleteWhenDone.Name:
		shouldDelete := options[cmdDeleteWhenDone.Options[0].Name]

		_, err := em.engine.Table(&Guild{}).ID(i.GuildID).Update(map[string]interface{}{
			"delete_when_done": shouldDelete.BoolValue(),
		})
		if err != nil {
			em.logger.WithError(err).Error("failed to update guild")
			reply = "Failed to update."
		} else {
			reply = "Options updated!"
		}
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: reply,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to reply to command: %w", err)
	}

	return nil
}

const PGUniqueConstraintViolation = "23505"

func (em *EventManager) possiblyCreateGuild(ctx context.Context, guildId string) (guild *Guild, exists bool, err error) {
	guild = &Guild{
		ID:                     guildId,
		NewEventChannelMessage: "@here a new event has been created!",
		EventColor:             0xFFFFFF,
	}

	_, err = em.engine.Context(ctx).Insert(guild)
	if err == nil {
		return guild, false, nil
	}

	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return nil, false, err
	}

	if pqErr.Code != PGUniqueConstraintViolation {
		return nil, false, err
	}

	_, err = em.engine.Context(ctx).ID(guildId).Get(guild)
	if err != nil {
		return nil, false, err
	}

	return nil, true, nil
}

func (em *EventManager) deleteEvent(ctx context.Context, log *logrus.Entry, s *discordgo.Session, guild *Guild, event *Event, m *discordgo.GuildScheduledEvent) (err error) {
	if guild.DeleteWhenDone {
		_, err = s.ChannelDelete(event.ChannelID)
		if err != nil {
			log.WithError(err).Warn("failed to delete channel")
		}
	} else {
		// TODO: Configurable archival?
		_, err = s.ChannelEdit(event.ChannelID, "done-"+eventChannelName(m.Name))
		if err != nil {
			log.WithError(err).Warn("failed to archive channel")
		}
	}

	err = s.GuildRoleDelete(event.GuildID, event.RoleID)
	if err != nil {
		log.WithError(err).Warn("failed to remove role")
	}

	_, err = em.engine.Context(ctx).Delete(event)
	if err != nil {
		log.WithError(err).Warn("failed to delete event")
	}

	return nil
}

func (em *EventManager) getGuildAndEvent(ctx context.Context, guildID string, eventID string) (*Guild, *Event, error) {
	guild, _, err := em.possiblyCreateGuild(ctx, guildID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create guild: %w", err)
	}

	event := &Event{ID: eventID}
	found, err := em.engine.Get(event)
	if err != nil {
		return nil, nil, err
	}

	if !found {
		return nil, nil, fmt.Errorf("was not able to find internal event")
	}

	return guild, event, nil
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
