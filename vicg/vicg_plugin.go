package vicg

import (
	"context"

	"github.com/luraproject/lura/v2/config"
	"github.com/luraproject/lura/v2/proxy"
)

// VicgPlugin 插件接口定义.
type VicgPlugin interface {
	HandleHTTPMessage(ctx context.Context, request *proxy.Request, response *proxy.Response) error
	Priority() int
}

// VicgPluginFactory 插件工厂.
type VicgPluginFactory interface {
	New(cfg *config.PluginConfig, infra interface{}) (VicgPlugin, error)
}
