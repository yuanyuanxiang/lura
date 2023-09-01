// SPDX-License-Identifier: Apache-2.0
/*
Package gin provides some basic implementations for building routers based on gin-gonic/gin
*/
package gin

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/luraproject/lura/v2/config"
	"github.com/luraproject/lura/v2/core"
	"github.com/luraproject/lura/v2/logging"
	"github.com/luraproject/lura/v2/proxy"
	"github.com/luraproject/lura/v2/router"
	"github.com/luraproject/lura/v2/transport/http/server"
	"github.com/luraproject/lura/v2/vicg"
)

const logPrefix = "[SERVICE: Gin]"

// RunServerFunc is a func that will run the http Server with the given params.
type RunServerFunc func(context.Context, config.ServiceConfig, http.Handler) error

// Config is the struct that collects the parts the router should be builded from
type Config struct {
	Engine         *gin.Engine
	Middlewares    []gin.HandlerFunc
	HandlerFactory HandlerFactory
	ProxyFactory   proxy.Factory
	VicgFactory    vicg.VicgFactory
	InfraFactory   vicg.InfraFactory
	Logger         logging.Logger
	RunServer      RunServerFunc
}

func (c *Config) getFactory() vicg.VicgFactory {
	if c.VicgFactory != nil {
		return c.VicgFactory
	}
	return vicg.NewVicgFactory(c.ProxyFactory)
}

// DefaultFactory returns a gin router factory with the injected proxy factory and logger.
// It also uses a default gin router and the default HandlerFactory
func DefaultFactory(proxyFactory proxy.Factory, logger logging.Logger) router.Factory {
	return NewFactory(
		Config{
			Engine:         gin.Default(),
			Middlewares:    []gin.HandlerFunc{},
			HandlerFactory: EndpointHandler,
			ProxyFactory:   proxyFactory,
			Logger:         logger,
			RunServer:      server.RunServer,
		},
	)
}

type Option func(*Config)

func DefaultVicgFactory(vicgFactory vicg.VicgFactory, infraFactory vicg.InfraFactory, logger logging.Logger, opts ...Option) router.Factory {
	cfg := Config{
		Engine:         gin.Default(),
		Middlewares:    []gin.HandlerFunc{},
		HandlerFactory: CustomErrorEndpointHandler(logger, server.DefaultToHTTPError),
		VicgFactory:    vicgFactory,
		InfraFactory:   infraFactory,
		Logger:         logger,
		RunServer:      server.RunServer,
	}
	for _, f := range opts {
		f(&cfg)
	}
	return NewFactory(cfg)
}

// NewFactory returns a gin router factory with the injected configuration
func NewFactory(cfg Config) router.Factory {
	return factory{cfg}
}

type factory struct {
	cfg Config
}

// New implements the factory interface
func (rf factory) New() router.Router {
	return rf.NewWithContext(context.Background())
}

// NewWithContext implements the factory interface
func (rf factory) NewWithContext(ctx context.Context) router.Router {
	return ginRouter{
		cfg:        rf.cfg,
		ctx:        ctx,
		runServerF: rf.cfg.RunServer,
		mu:         new(sync.Mutex),
		urlCatalog: urlCatalog{
			mu:      new(sync.Mutex),
			catalog: map[string][]string{},
		},
	}
}

type ginRouter struct {
	cfg        Config
	ctx        context.Context
	runServerF RunServerFunc
	mu         *sync.Mutex
	urlCatalog urlCatalog
}

type urlCatalog struct {
	mu      *sync.Mutex
	catalog map[string][]string
}

// Run completes the router initialization and executes it
func (r ginRouter) Run(cfg config.ServiceConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	server.InitHTTPDefaultTransport(cfg)

	infra, err := r.buildInfra(cfg)

	if err != nil {
		r.cfg.Logger.Infof("%s Router execution failed: %v", logPrefix, err)
		return
	}
	// 令所有插件加载成功进程才会启动
	if err = r.registerEndpointsAndMiddlewares(cfg, infra); err != nil {
		return
	}

	// TODO: remove this ugly hack once https://github.com/gin-gonic/gin/pull/2692 and
	// https://github.com/gin-gonic/gin/issues/2862 are completely fixed
	// go r.cfg.Engine.Run("0.0.0.0:18899")
	// pprof.Register(r.cfg.Engine) // 注册pprof
	// r.cfg.Logger.RegisterAPI(r.cfg.Engine) // 通过API接口动态修改日志级别
	r.cfg.Logger.Info("[SERVICE: Gin] Listening on port:", cfg.Port)
	if err := r.runServerF(r.ctx, cfg, r.cfg.Engine.Handler()); err != nil && err != http.ErrServerClosed {
		r.cfg.Logger.Error(logPrefix, err.Error())
	}

	r.cfg.Logger.Info(logPrefix, "Router execution ended")
}

func (r ginRouter) registerEndpointsAndMiddlewares(cfg config.ServiceConfig, infra vicg.InfraAPI) error {
	if cfg.Debug {
		r.cfg.Engine.Any("/__debug/*param", DebugHandler(r.cfg.Logger))
	}

	if cfg.Echo {
		r.cfg.Engine.Any("/__echo/*param", EchoHandler())
	}

	endpointGroup := r.cfg.Engine.Group("/")
	endpointGroup.Use(r.cfg.Middlewares...)

	err := r.registerKrakendEndpoints(endpointGroup, cfg, infra)
	if opts, ok := cfg.ExtraConfig[Namespace].(map[string]interface{}); ok {
		if v, ok := opts["auto_options"].(bool); ok && v {
			r.cfg.Logger.Debug(logPrefix, "Enabling the auto options endpoints")
			r.registerOptionEndpoints(endpointGroup)
		}
	}
	return err
}

func (r ginRouter) buildInfra(cfg config.ServiceConfig) (api vicg.InfraAPI, err error) {
	if r.cfg.InfraFactory != nil {
		api, err = r.cfg.InfraFactory.New(r.ctx, cfg.InfraConfig)
	}
	return api, err
}

// mergeConfig 将ServiceConfig中定义的, 可以在EndpintConfig中使用的参数, 作为EndpointConfig的默认值.
func mergeConfig(gc config.ServiceConfig, ec *config.EndpointConfig) {
	if ec.OutputEncoding == "" && gc.OutputEncoding != "" {
		ec.OutputEncoding = gc.OutputEncoding
	}
	if ec.Timeout == 0 && gc.Timeout != 0 {
		ec.Timeout = gc.Timeout
	}
	if ec.CacheTTL == 0 && gc.CacheTTL != 0 {
		ec.CacheTTL = gc.CacheTTL
	}
}

func (r ginRouter) registerKrakendEndpoints(rg *gin.RouterGroup, cfg config.ServiceConfig, infra vicg.InfraAPI) error {
	// build and register the pipes and endpoints sequentially
	for _, c := range cfg.Endpoints {
		// merge some common global configurations
		mergeConfig(cfg, c)
		proxyStack, err := r.cfg.getFactory().New(c, infra)
		if err != nil {
			r.cfg.Logger.Error(logPrefix, "Calling the ProxyFactory", err.Error())
			return err
		}
		r.registerKrakendEndpoint(rg, c.Method, c, r.cfg.HandlerFactory(c, proxyStack), len(c.Backend))
	}
	return nil
}

func (r ginRouter) registerKrakendEndpoint(rg *gin.RouterGroup, method string, e *config.EndpointConfig, h gin.HandlerFunc, total int) {
	method = strings.ToTitle(method)
	path := e.Endpoint
	if method != http.MethodGet && total > 1 {
		if !router.IsValidSequentialEndpoint(e) {
			r.cfg.Logger.Error(logPrefix, method, "endpoints with sequential proxy enabled only allow a non-GET in the last backend! Ignoring", path)
			return
		}
	}

	switch method {
	case http.MethodGet:
		rg.GET(path, h)
	case http.MethodPost:
		rg.POST(path, h)
	case http.MethodPut:
		rg.PUT(path, h)
	case http.MethodPatch:
		rg.PATCH(path, h)
	case http.MethodDelete:
		rg.DELETE(path, h)
	default:
		r.cfg.Logger.Error(logPrefix, "[ENDPOINT:", path, "] Unsupported method", method)
		return
	}

	r.urlCatalog.mu.Lock()
	defer r.urlCatalog.mu.Unlock()

	methods, ok := r.urlCatalog.catalog[path]
	if !ok {
		r.urlCatalog.catalog[path] = []string{method}
		return
	}
	r.urlCatalog.catalog[path] = append(methods, method)
}

func (r ginRouter) registerOptionEndpoints(rg *gin.RouterGroup) {
	r.urlCatalog.mu.Lock()
	defer r.urlCatalog.mu.Unlock()

	for path, methods := range r.urlCatalog.catalog {
		sort.Strings(methods)
		allowed := strings.Join(methods, ", ")

		rg.OPTIONS(path, func(c *gin.Context) {
			c.Header("Allow", allowed)
			c.Header(core.KrakendHeaderName, core.KrakendHeaderValue)
		})
	}
}
