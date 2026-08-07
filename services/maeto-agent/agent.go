package maetoagent

import (
	"context"
	"net/http"
)

type Agent struct {
	controlClient *http.Client
}

func NewAgent(
	controlClient *http.Client,
) *Agent {
	return &Agent{
		controlClient: controlClient,
	}
}

func (a *Agent) Run(
	ctx context.Context,
) {
}
