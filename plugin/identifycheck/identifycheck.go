package identifycheck

import (
	"context"
	"fmt"

	"github.com/luraproject/lura/v2/config"
	"github.com/luraproject/lura/v2/proxy"
	"github.com/luraproject/lura/v2/vicg"
)

/* ************************** 校验请求者身份插件 ******************** */

type Factory struct {
}

// Plugin defines
type Plugin struct {
	name  string
	index int
	infra interface{}
}

func (e Factory) New(cfg *config.PluginConfig, infra interface{}) (vicg.VicgPlugin, error) {
	return &Plugin{
		index: cfg.Index,
		name:  cfg.Name,
		infra: infra,
	}, nil
}

func (e *Plugin) HandleHTTPMessage(ctx context.Context, request *proxy.Request, response *proxy.Response) error {
	identify := request.HeaderGet("User-Identify")
	if len(identify) != 20 {
		return fmt.Errorf("identify check failed")
	}

	return nil
}

func (e *Plugin) Priority() int {
	return e.index
}
