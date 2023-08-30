package vicg

import (
	"context"

	"github.com/luraproject/lura/v2/config"
	logger "github.com/luraproject/lura/v2/logging"
)

type InfraAPI interface {
}

type InfraAPIImpl struct {
	cfg *config.InfraConfig
}

type InfraFactory interface {
	New(ctx context.Context, cfg *config.InfraConfig) (InfraAPI, error)
}

type defaultInfraFactory struct {
}

func (df *defaultInfraFactory) New(ctx context.Context, cfg *config.InfraConfig) (InfraAPI, error) {
	return &InfraAPIImpl{cfg: cfg}, nil
}

func DefaultInfraFactory(logger logger.Logger) InfraFactory {
	return &defaultInfraFactory{}
}
