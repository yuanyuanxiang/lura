// SPDX-License-Identifier: Apache-2.0

package vicg

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/luraproject/lura/v2/config"
	logger "github.com/luraproject/lura/v2/logging"
	"github.com/luraproject/lura/v2/proxy"
	"github.com/luraproject/lura/v2/router/gin"
)

/* ***************************************************************************
* 代码功能: 默认的代理工厂实现示例
* 	该代理工厂包含一系列的插件工厂, 创建HTTP接口代理时根据配置生产相应的插件.
* 	处理HTTP接口时, 将按照插件顺序进行.
*************************************************************************** */

// DefaultFactory 创建默认的代理工厂.
func DefaultVicgFactory(logger logger.Logger, factory map[string]VicgPluginFactory) gin.VicgFactory {
	return defaultVicgFactory{
		logger:        logger,
		pluginFactory: factory,
	}
}

// defaultVicgFactory 自定义代理工厂.
type defaultVicgFactory struct {
	logger        logger.Logger
	pluginFactory map[string]VicgPluginFactory // 插件集合
}

// createNewPlugin 通过插件工厂创建插件.
func (pf defaultVicgFactory) createNewPlugin(cfg *config.PluginConfig, infra interface{}) (VicgPlugin, error) {
	f, ok := pf.pluginFactory[cfg.Name]
	if !ok {
		return nil, fmt.Errorf("the plugin '%s' not found", cfg.Name)
	}
	return f.New(cfg, infra)
}

// Infra 用户自定义结构示例.
type Infra struct {
	ExtraConfig map[string]interface{}
}

// BuildInfra 创建用户自定义结构.
func (pf defaultVicgFactory) BuildInfra(ctx context.Context, cfg config.ExtraConfig) (infra interface{}, err error) {
	return &Infra{ExtraConfig: cfg}, nil
}

// New 创建HTTP接口代理.
func (pf defaultVicgFactory) New(cfg *config.EndpointConfig, infra interface{}) (proxy.Proxy, error) {
	plugins := make([]VicgPlugin, len(cfg.Plugins))
	for i, c := range cfg.Plugins {
		p, err := pf.createNewPlugin(c, infra)
		if err != nil {
			return nil, err
		}
		plugins[i] = p
	}
	// 从小到大进行排序
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Priority() < plugins[j].Priority()
	})

	return func(ctx context.Context, request *proxy.Request) (*proxy.Response, error) {
		response := &proxy.Response{
			Data:       make(map[string]interface{}),
			IsComplete: true,
			Metadata: proxy.Metadata{
				Headers:    map[string][]string{"Content-Type": {"application/VIID+JSON"}},
				StatusCode: http.StatusOK,
			},
		}
		var err error
		var sec = 5 * time.Second
		for _, p := range plugins {
			tick := time.Now()
			err = p.HandleHTTPMessage(ctx, request, response)
			if err != nil {
				pf.logger.Infof("plugin index %d: %s", p.Priority(), err.Error())
				break
			}
			if span := time.Since(tick); span > sec {
				pf.logger.Infof("The '%d' plugin cost %v on %s '%s'.", p.Priority(), span, request.Method, request.Path)
			}
		}
		return response, err
	}, nil
}
