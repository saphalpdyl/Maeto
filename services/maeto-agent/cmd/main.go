package main

import (
	"context"
	"os/signal"
	"syscall"

	maetoagent "github.com/saphalpdyl/maeto/services/maeto-agent"
)

func main() {
	ctx := context.Background()

	controlClient := maetoagent.NewControlClient("unix", "/tmp/agent.sock")
	agent := maetoagent.NewAgent(
		controlClient,
	)

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	agent.Run(ctx)
}
