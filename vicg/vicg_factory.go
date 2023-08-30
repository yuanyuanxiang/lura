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
)

// Factory creates proxies based on the received endpoint configuration.
//
// Both, factories and backend factories, create proxies but factories are designed as a stack makers
// because they are intended to generate the complete proxy stack for a given frontend endpoint
// the app would expose and they could wrap several proxies provided by a backend factory
type VicgFactory interface {
	New(cfg *config.EndpointConfig, infra InfraAPI) (proxy.Proxy, error)
}

type VicgPluginFactory interface {
	New(cfg *config.PluginConfig, infra InfraAPI) (VicgPlugin, error)
}

// DefaultFactory returns a default http proxy factory with the injected logger
func DefaultVicgFactory(logger logger.Logger, factory map[string]VicgPluginFactory) VicgFactory {
	return defaultVicgFactory{
		logger:        logger,
		pluginFactory: factory,
	}
}

type defaultProxyFactory struct {
	factory proxy.Factory
}

func (pf defaultProxyFactory) New(cfg *config.EndpointConfig, infra InfraAPI) (proxy.Proxy, error) {
	return pf.factory.New(cfg)
}

func NewVicgFactory(factory proxy.Factory) VicgFactory {
	return defaultProxyFactory{factory: factory}
}

type defaultVicgFactory struct {
	logger        logger.Logger
	pluginFactory map[string]VicgPluginFactory
}

func (pf defaultVicgFactory) createNewPlugin(cfg *config.PluginConfig, infra InfraAPI) (VicgPlugin, error) {
	f, err := pf.getPluginFactory(cfg.Name)
	if err != nil {
		return nil, err
	}
	return f.New(cfg, infra)
}

func (pf defaultVicgFactory) getPluginFactory(namespace string) (VicgPluginFactory, error) {
	if f, ok := pf.pluginFactory[namespace]; ok {
		return f, nil
	}

	return nil, fmt.Errorf("the plugin '%s' not found", namespace)
}

func (pf defaultVicgFactory) New(cfg *config.EndpointConfig, infra InfraAPI) (proxy.Proxy, error) {
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
