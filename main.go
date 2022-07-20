package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/imle/discord-bot-event-channels/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	err := cmd.Execute(ctx)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
