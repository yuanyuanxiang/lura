package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/pprof"
	"github.com/luraproject/lura/v2/config"
	"github.com/luraproject/lura/v2/logging"
	"github.com/luraproject/lura/v2/plugin/identifycheck"
	"github.com/luraproject/lura/v2/router/gin"
	"github.com/luraproject/lura/v2/vicg"
)

// 配置文件: plugin\plugin.json
// 在上述配置文件中配置HTTP接口的处理插件
func main() {
	var ctx = context.TODO()
	var log, _ = logging.NewLogger("INFO", os.Stdout, "")
	var srvConf = config.ServiceConfig{
		Version:         1,
		Name:            "main",
		Debug:           false,
		Timeout:         time.Duration(180) * time.Second,
		CacheTTL:        time.Duration(10) * time.Second,
		Port:            9000,
		SequentialStart: true,
		ExtraConfig:     make(config.ExtraConfig),
		InfraConfig:     &config.InfraConfig{Log: log},
	}
	var err error
	srvConf.Endpoints, err = ReadPluginDir("plugin")
	if err != nil {
		log.Info(err)
		return
	}
	srvConf.NormalizeEndpoints()
	// 全局插件工厂
	factory := map[string]vicg.VicgPluginFactory{
		"IdentifyCheck": identifycheck.Factory{},
	}
	f := func(cfg *gin.Config) {
		pprof.Register(cfg.Engine) // 注册pprof
	}
	router := gin.DefaultVicgFactory(vicg.DefaultVicgFactory(log, factory), vicg.DefaultInfraFactory(log), log, f).NewWithContext(ctx)
	router.Run(srvConf)
}

func ReadPluginDir(dirName string) ([]*config.EndpointConfig, error) {
	array := make([]*config.EndpointConfig, 0)
	var fileList = make([]string, 0)

	files, err := os.ReadDir(dirName)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		fileList = append(fileList, filepath.Join(dirName, file.Name()))
	}
	var errString string
	for _, p := range fileList {
		plugin, err := readPluginFile(p)
		if plugin == nil {
			if err != nil {
				errString += err.Error() + ";"
			}
			continue
		}
		array = append(array, plugin...)
	}
	if errString == "" {
		return array, nil
	}
	return array, fmt.Errorf("%s", errString)
}

func readPluginFile(fileName string) ([]*config.EndpointConfig, error) {
	plugin := &config.EndpointPluginList{}
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(bytes, plugin)
	if err != nil {
		return nil, err
	}
	return plugin.Plugin, nil
}
