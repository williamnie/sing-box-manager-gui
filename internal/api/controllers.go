package api

import (
	"context"
	"github.com/xiaobei/singbox-manager/internal/gateway"
)

type processController interface {
	ApplyConfig([]byte) error
	CheckConfig([]byte) error
	Version() (string, error)
	Start() error
	Stop() error
	Restart() error
	Reload() error
	IsRunning() bool
	GetPID() int
	SetConfigPath(string)
}
type gatewayController interface {
	Check(context.Context, string, gateway.Config) (gateway.CheckResult, error)
	Apply(context.Context, string, gateway.Config) (gateway.Status, error)
	Status(context.Context) (gateway.Status, error)
	Rollback(context.Context) (gateway.Status, error)
}
