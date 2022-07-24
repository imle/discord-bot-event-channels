package bot

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
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
	em.engine.ShowSQL(false)
	em.engine.SetMapper(names.GonicMapper{})
	if err := em.engine.Sync2(new(Guild)); err != nil {
		return err
	}
	if err := em.engine.Sync2(new(Event)); err != nil {
		return err
	}
	em.engine.ShowSQL(true)

	return nil
}

func (em *EventManager) ConsumeSession(s *discordgo.Session) {
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log := em.logger.WithFields(logrus.Fields{
			"method": "Ready",
		})

		log.Debug("received")

		err := em.onReady(context.TODO(), log, s, r)
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

	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildDelete) {
		log := em.logger.WithFields(logrus.Fields{
			"method":   "GuildCreate",
			"guild_id": m.ID,
		})

		log.Debug("received")

		err := em.onGuildDelete(context.TODO(), log, s, m)
		if err != nil {
			log.WithError(err).Error("failed guild remove")
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

	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildScheduledEventUserAdd) {
		log := em.logger.WithFields(logrus.Fields{
			"method":   "GuildScheduledEventUserAdd",
			"guild_id": m.GuildID,
			"event_id": m.GuildScheduledEventID,
			"user_id":  m.UserID,
		})

		log.Debug("received")

		err := em.onGuildEventUserAdd(context.TODO(), log, s, m)
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

		err := em.onGuildEventUserRemove(context.TODO(), log, s, m)
		if err != nil {
			log.WithError(err).Error("failed")
			return
		}
	})

	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		fields := logrus.Fields{
			"method":         "InteractionCreate",
			"guild_id":       i.GuildID,
			"interaction_id": i.ID,
		}

		if i.User != nil {
			fields["user_id"] = i.User.ID
		}
		if i.Member != nil && i.Member.User != nil {
			fields["member_user_id"] = i.Member.User.ID
		}

		log := em.logger.WithFields(fields)

		log.Debug("received")

		err := em.handleInteraction(context.TODO(), log, s, i)
		if err != nil {
			log.WithError(err).Error("failed")
			return
		}
	})
}

func (em *EventManager) RegisterGlobalCommands(session *discordgo.Session) error {
	_, err := session.ApplicationCommandBulkOverwrite(session.State.User.ID, "", globalCommands)
	if err != nil {
		return err
	}

	return nil
}

func (em *EventManager) reconcile(ctx context.Context, session *discordgo.Session, guild *discordgo.Guild) error {
	var internalGuild Guild
	found, err := em.engine.Context(ctx).Table(&Guild{}).Where("id = ?", guild.ID).Get(&internalGuild)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("no guild with that id")
	}

	if !internalGuild.ConfigurationWasRun || !internalGuild.FirstReconcileRun {
		return nil
	}

	var internalEvents []*Event
	err = em.engine.Context(ctx).Table(&Event{}).Where("guild_id = ?", guild.ID).Find(&internalEvents)
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
		internalEvent, has := internalEventsMap[event.ID]
		log := em.logger.WithFields(logrus.Fields{
			"method":   "Reconcile",
			"guild_id": event.GuildID,
			"event_id": event.ID,
		})

		// Remove all found or created keys to see what was deleted.
		delete(internalEventsMap, event.ID)

		if has {
			if internalEvent.ChannelID == "" {
				log.Warnf("found internal event with missing ChannelID")
				// Delete the event and re-create it
				_, err := em.engine.Table(&Event{}).ID(event.ID).Delete()
				if err != nil {
					log.WithError(err).Errorf("failed to delete event channel")
				}
				has = false
			}
		}

		if !has {
			err := em.onGuildEventCreate(ctx, log, session, &discordgo.GuildScheduledEventCreate{
				GuildScheduledEvent: event,
			})
			if err != nil {
				log.WithError(err).Errorf("failed to create event channel")
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
func (em *EventManager) onReady(ctx context.Context, log *logrus.Entry, s *discordgo.Session, r *discordgo.Ready) error {
	err := em.RegisterGlobalCommands(s)
	if err != nil {
		return err
	}

	for _, guild := range r.Guilds {
		err := em.reconcile(ctx, s, guild)
		if err != nil {
			log.WithField("guild_id", guild.ID).WithError(err).Errorf("failed to reconcile")
		}
	}

	return nil
}

// When a discordgo.Guild is added we want to alert the Owner that they need to run the config command.
func (em *EventManager) onGuildCreate(ctx context.Context, s *discordgo.Session, m *discordgo.GuildCreate) error {
	_, exists, err := em.possiblyCreateGuild(ctx, m.Guild)
	if err != nil {
		return fmt.Errorf("failed to create guild: %w", err)
	}

	// We don't want to reconcile if we already have the Guild recorded as this is just an update.
	if exists {
		return nil
	}

	// First time we have seen the guild, we want to go ahead and create the welcome message.
	channel, err := s.UserChannelCreate(m.OwnerID)
	if err != nil {
		return err
	}

	_, err = s.ChannelMessageSend(channel.ID,
		"Thanks for adding Event Channels to your Discord server!\nDon't forget to run the setup command!",
	)
	if err != nil {
		return err
	}

	return nil
}

// When a discordgo.Guild is removed, we drop its data.
func (em *EventManager) onGuildDelete(ctx context.Context, log *logrus.Entry, s *discordgo.Session, m *discordgo.GuildDelete) error {
	_, err := em.engine.Context(ctx).Delete(&Guild{ID: m.Guild.ID})
	if err != nil {
		log.WithError(err).Warn("failed to delete the guild")
	}

	_, err = em.engine.Context(ctx).Delete(&Event{GuildID: m.Guild.ID})
	if err != nil {
		log.WithError(err).Warn("failed to delete the guild")
	}

	return nil
}

// Create a discordgo.Channel for the event then put a discordgo.Message in the
// discordgo.Guild's specified EventAnnouncement discordgo.Channel.
func (em *EventManager) onGuildEventCreate(ctx context.Context, log *logrus.Entry, s *discordgo.Session, m *discordgo.GuildScheduledEventCreate) (err error) {
	var channel *discordgo.Channel
	var message *discordgo.Message
	var event *Event

	var guild Guild
	_, err = em.engine.Context(ctx).ID(m.GuildID).Get(&guild)
	if err != nil {
		return fmt.Errorf("failed to find internal guild: %w", err)
	}

	// Try to clean up our mess if anything failed.
	defer func() {
		if err == nil {
			return
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

	var roles []*discordgo.Role
	roles, err = s.GuildRoles(m.GuildID)
	if err != nil {
		return fmt.Errorf("failed to create role: %w", err)
	}
	var atEveryoneRole *discordgo.Role
	for _, role := range roles {
		if role.Name == "@everyone" {
			atEveryoneRole = role
			break
		}
	}
	if atEveryoneRole == nil {
		return fmt.Errorf("failed to find @everyone role")
	}

	channel, err = s.GuildChannelCreateComplex(m.GuildID, discordgo.GuildChannelCreateData{
		Name:     eventChannelName(m.Name),
		Type:     discordgo.ChannelTypeGuildText,
		Topic:    m.Description,
		ParentID: guild.EventChannelParentID,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{
			{
				ID:    s.State.User.ID,
				Type:  discordgo.PermissionOverwriteTypeMember,
				Allow: discordgo.PermissionViewChannel,
			},
			{
				ID:   atEveryoneRole.ID,
				Type: discordgo.PermissionOverwriteTypeRole,
				Deny: discordgo.PermissionViewChannel,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create channel: %w", err)
	}

	if guild.EventAnnouncementChannelID != "" {
		invite, err := s.ChannelInviteCreate(channel.ID, discordgo.Invite{
			MaxAge:    0,
			MaxUses:   0,
			Temporary: false,
			Unique:    false,
		})
		if err != nil {
			return err
		}

		message, err = s.ChannelMessageSend(guild.EventAnnouncementChannelID, guild.GetNewEventChannelMessage(m.Name, invite.Code, m.ID))
		if err != nil {
			return fmt.Errorf("failed to announce channel: %w", err)
		}

		message, err = s.ChannelMessageSend(channel.ID, getEventInviteURL(invite.Code, m.ID))
		if err != nil {
			return fmt.Errorf("failed to announce channel: %w", err)
		}
	}

	event = &Event{
		ID:        m.ID,
		GuildID:   m.GuildID,
		ChannelID: channel.ID,
	}
	if message != nil {
		event.AnnounceMessageID = &message.ID
	}
	_, err = em.engine.Insert(event)
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

	if event == nil {
		return fmt.Errorf("was not able to find internal event")
	}

	switch m.Status {
	case discordgo.GuildScheduledEventStatusCompleted, discordgo.GuildScheduledEventStatusCanceled:
		log.Debug("received delete via update event")

		return em.deleteEvent(ctx, log, s, guild, event)
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

	if event == nil {
		return fmt.Errorf("was not able to find internal event")
	}

	err = em.deleteEvent(ctx, log, s, guild, event)
	if err != nil {
		return fmt.Errorf("failed to delete event: %w", err)
	}

	return nil
}

// Add the discordgo.User to the discordgo.Channel for the discordgo.Event.
func (em *EventManager) onGuildEventUserAdd(ctx context.Context, log *logrus.Entry, s *discordgo.Session, m *discordgo.GuildScheduledEventUserAdd) error {
	if m.UserID == s.State.User.ID {
		return nil
	}

	var event *Event
	var err error
	for i := 0; i < 5; i++ {
		_, event, err = em.getGuildAndEvent(ctx, m.GuildID, m.GuildScheduledEventID)
		if err != nil {
			return err
		}
		if event != nil {
			break
		}

		time.Sleep(1 * time.Second)
	}

	if event == nil {
		log.Warn("was not able to find internal event")
		return nil
	}

	err = s.ChannelPermissionSet(event.ChannelID, m.UserID, discordgo.PermissionOverwriteTypeMember, discordgo.PermissionViewChannel, 0)
	if err != nil {
		return fmt.Errorf("failed to add permissions to channel: %w", err)
	}

	return nil
}

func (em *EventManager) onGuildEventUserRemove(ctx context.Context, log *logrus.Entry, s *discordgo.Session, m *discordgo.GuildScheduledEventUserRemove) error {
	if m.UserID == s.State.User.ID {
		return nil
	}

	_, event, err := em.getGuildAndEvent(ctx, m.GuildID, m.GuildScheduledEventID)
	if err != nil {
		return err
	}

	if event == nil {
		log.Warn("was not able to find internal event")
		return nil
	}

	err = s.ChannelPermissionDelete(event.ChannelID, m.UserID)
	if err != nil {
		return fmt.Errorf("failed to remove permissions for channel: %w", err)
	}

	return nil
}

func (em *EventManager) handleInteraction(ctx context.Context, log *logrus.Entry, s *discordgo.Session, i *discordgo.InteractionCreate) (err error) {
	defer func() {
		if err != nil {
			_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: "Something went wrong",
			})
			return
		}
	}()

	switch i.Type {
	case discordgo.InteractionPing:
	case discordgo.InteractionApplicationCommand:
		switch i.ApplicationCommandData().Name {
		case cmdOptions.Name:
			optionsSlice := i.ApplicationCommandData().Options
			options := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(optionsSlice))
			for _, opt := range optionsSlice {
				options[opt.Name] = opt
			}

			var guild Guild
			found, err := em.engine.Context(ctx).ID(i.GuildID).Get(&guild)
			if err != nil {
				return err
			}

			if !found {
				return fmt.Errorf("could not find guild")
			}

			var reply string
			errMessage := getConfigOptionsMap(s, options, &guild)
			if errMessage != "" {
				reply = errMessage
				break
			}

			guild.ConfigurationWasRun = true
			_, err = em.engine.ID(i.GuildID).UseBool().Update(guild)
			if err != nil {
				em.logger.WithError(err).Error("failed to update guild message")
				reply = "Failed to update config settings."
			} else {
				reply = "Successfully updated config settings!"
			}

			err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: reply,
				},
			})
			if err != nil {
				return fmt.Errorf("failed to reply to command: %w", err)
			}
		case cmdSync.Name:
			err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "loading your events",
				},
			})
			if err != nil {
				return err
			}

			selects, err := em.getEventSelects(s, i.GuildID, false)
			if err != nil {
				return err
			}

			_, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: "Select the channels you want to assign to these already existing events.\n" +
					"If no channel is selected for an event, one will be created.\n" +
					"Channels selected here will be made private and the users marked as interested will be given access.",
				Components: append(selects, &discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						&discordgo.Button{
							CustomID: "finish",
							Label:    "Done",
							Style:    discordgo.SuccessButton,
						},
					},
				}),
			})
			if err != nil {
				return err
			}
		}
	case discordgo.InteractionMessageComponent:
		data := i.Data.(discordgo.MessageComponentInteractionData)
		switch data.CustomID {
		case "finish":
			err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredMessageUpdate,
			})
			if err != nil {
				return err
			}

			message := "finishing up..."
			components := make([]discordgo.MessageComponent, 0)
			_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content:    &message,
				Components: &components,
			})
			if err != nil {
				return err
			}

			events, err := s.GuildScheduledEvents(i.GuildID, false)
			if err != nil {
				return err
			}

			var guild Guild
			found, err := em.engine.Context(ctx).ID(i.GuildID).Get(&guild)
			if err != nil {
				return err
			}

			if !found {
				return fmt.Errorf("could not find guild")
			}

			var internalEvents = map[string]*Event{}
			err = em.engine.Context(ctx).Where("guild_id = ?", i.GuildID).Find(&internalEvents)
			if err != nil {
				return err
			}

			var roles []*discordgo.Role
			roles, err = s.GuildRoles(i.GuildID)
			if err != nil {
				return fmt.Errorf("failed to create role: %w", err)
			}
			var atEveryoneRole *discordgo.Role
			for _, role := range roles {
				if role.Name == "@everyone" {
					atEveryoneRole = role
					break
				}
			}
			if atEveryoneRole == nil {
				return fmt.Errorf("failed to find @everyone role")
			}

			for _, event := range events {
				internalEvent, found := internalEvents[event.ID]
				if !found {
					err := em.onGuildEventCreate(ctx, log, s, &discordgo.GuildScheduledEventCreate{
						GuildScheduledEvent: event,
					})
					if err != nil {
						return err
					}
				} else {
					permissionOverwrites := []*discordgo.PermissionOverwrite{
						{
							ID:    s.State.User.ID,
							Type:  discordgo.PermissionOverwriteTypeMember,
							Allow: discordgo.PermissionViewChannel,
						},
						{
							ID:   atEveryoneRole.ID,
							Type: discordgo.PermissionOverwriteTypeRole,
							Deny: discordgo.PermissionViewChannel,
						},
					}

					var lastID string
					for {
						eventUsers, err := s.GuildScheduledEventUsers(i.GuildID, event.ID, 100, false, "", lastID)
						if err != nil {
							return err
						}
						if len(eventUsers) == 0 {
							break
						}
						lastID = eventUsers[len(eventUsers)-1].User.ID
						for _, eventUser := range eventUsers {
							permissionOverwrites = append(permissionOverwrites, &discordgo.PermissionOverwrite{
								ID:    eventUser.User.ID,
								Type:  discordgo.PermissionOverwriteTypeMember,
								Allow: discordgo.PermissionViewChannel,
							})
						}
					}

					_, err = s.ChannelEditComplex(internalEvent.ChannelID, &discordgo.ChannelEdit{
						ParentID:             guild.EventChannelParentID,
						PermissionOverwrites: permissionOverwrites,
					})
					if err != nil {
						return err
					}
				}
			}

			guild.FirstReconcileRun = true
			_, err = em.engine.Context(ctx).ID(guild.ID).UseBool().Update(&guild)
			if err != nil {
				return err
			}

			content := "Channels linked!"
			_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &content,
			})
			if err != nil {
				return err
			}
		default:
			if len(data.Values) != 1 {
				_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "No value selected",
				})
				return nil
			}
			channelID := data.Values[0]

			err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredMessageUpdate,
			})
			if err != nil {
				return err
			}

			_, err = em.engine.Context(ctx).Insert(&Event{
				ID:                data.CustomID,
				GuildID:           i.GuildID,
				ChannelID:         channelID,
				AnnounceMessageID: nil,
			})
			if isErrDuplicatePGConstraint(err) {
				_, err = em.engine.Context(ctx).Update(&Event{
					ID:                data.CustomID,
					GuildID:           i.GuildID,
					ChannelID:         channelID,
					AnnounceMessageID: nil,
				})
			}
			if err != nil {
				return err
			}
		}
	case discordgo.InteractionApplicationCommandAutocomplete:
	case discordgo.InteractionModalSubmit:
	}

	return nil
}

func (em *EventManager) getEventSelects(s *discordgo.Session, guildID string, disabled bool) ([]discordgo.MessageComponent, error) {
	events, err := s.GuildScheduledEvents(guildID, false)
	if err != nil {
		return nil, err
	}

	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return nil, err
	}

	selectOptions := make([]discordgo.SelectMenuOption, 0, len(channels))
	for idx := range channels {
		if channels[idx].Type != discordgo.ChannelTypeGuildText {
			continue
		}

		selectOptions = append(selectOptions, discordgo.SelectMenuOption{
			Label: channels[idx].Name,
			Value: channels[idx].ID,
		})
	}

	selects := make([]discordgo.MessageComponent, 0, 2*len(events))
	for idx := range events {
		selects = append(selects, &discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				&discordgo.SelectMenu{
					CustomID:    events[idx].ID,
					Placeholder: events[idx].Name,
					MaxValues:   1,
					Options:     selectOptions,
					Disabled:    disabled,
				},
			},
		})
	}
	return selects, nil
}

func (em *EventManager) possiblyCreateGuild(ctx context.Context, m *discordgo.Guild) (guild *Guild, exists bool, err error) {
	guild = &Guild{
		ID:                         m.ID,
		NewEventChannelMessage:     "`%EVENT%` was just created, if you want to join the channel, mark yourself as interested on the event!",
		EventAnnouncementChannelID: m.PublicUpdatesChannelID,
	}

	_, err = em.engine.Context(ctx).Insert(guild)
	if err == nil {
		return guild, false, nil
	}

	if !isErrDuplicatePGConstraint(err) {
		return nil, false, err
	}

	_, err = em.engine.Context(ctx).ID(m.ID).Get(guild)
	if err != nil {
		return nil, false, err
	}

	return guild, true, nil
}

func (em *EventManager) deleteEvent(ctx context.Context, log *logrus.Entry, s *discordgo.Session, guild *Guild, event *Event) (err error) {
	if guild.DeleteWhenDone {
		_, err = s.ChannelDelete(event.ChannelID)
		if err != nil {
			log.WithError(err).Warn("failed to delete channel")
		}
	}

	_, err = em.engine.Context(ctx).Delete(event)
	if err != nil {
		log.WithError(err).Warn("failed to delete event")
	}

	return nil
}

func (em *EventManager) getGuildAndEvent(ctx context.Context, guildID string, eventID string) (*Guild, *Event, error) {
	guild := &Guild{}
	_, err := em.engine.Context(ctx).ID(guildID).Get(guild)
	if err != nil {
		return nil, nil, err
	}

	event := &Event{ID: eventID}
	found, err := em.engine.Get(event)
	if err != nil {
		return nil, nil, err
	}

	if !found {
		return guild, nil, nil
	}

	return guild, event, nil
}
