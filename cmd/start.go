package cmd

import (
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/xo/dburl"

	"github.com/imle/discord-bot-event-channels/cmd/start"
)

var (
	token       = ""
	dburlString = ""
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "start the bot",
	RunE: func(cmd *cobra.Command, args []string) error {
		u, err := dburl.Parse(dburlString)
		if err != nil {
			return err
		}

		logger := logrus.New()
		if _, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST"); ok {
			logger.SetFormatter(&logrus.JSONFormatter{})
		}
		logger.SetLevel(logrus.DebugLevel)

		manager, err := start.InitializeEventManager(logger, start.EngineConfig{
			URI:        u,
			LogQueries: true,
		})
		if err != nil {
			return err
		}

		session, err := start.InitializeDiscordGoSession(logger, start.DiscordSessionConfig{
			Token: token,
		})
		if err != nil {
			return err
		}

		err = manager.SyncDB()
		if err != nil {
			return err
		}

		manager.ConsumeSession(session)
		if err != nil {
			return err
		}

		err = session.Open()
		if err != nil {
			return fmt.Errorf("cannot open the session: %w", err)
		}

		defer func() {
			err := session.Close()
			if err != nil {
				logger.WithError(err).Error("failed to properly close session")
			}
		}()

		stop := make(chan os.Signal, 1)
		defer close(stop)
		signal.Notify(stop, os.Interrupt)
		<-stop
		log.Println("stopping")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)

	startCmd.Flags().StringVarP(&token, "token", "t", token, "bot token")
	startCmd.Flags().StringVar(&dburlString, "dburl", dburlString, "dburl connection string")
}
