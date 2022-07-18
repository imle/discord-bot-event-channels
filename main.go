package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/imle/discord-bot-event-channels/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	cmd.Execute(ctx)
}
