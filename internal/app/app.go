package app

import (
	"scm.x5.ru/dis.cloud/go-pkgs/configurator"
	"scm.x5.ru/dis.cloud/go-pkgs/configurator/sources/consul"
	"scm.x5.ru/dis.cloud/go-pkgs/configurator/sources/vault"
	httpclient "scm.x5.ru/dis.cloud/go-pkgs/http-client"
	"scm.x5.ru/dis.cloud/go-pkgs/log"
	"scm.x5.ru/dis.cloud/operators/release-operator/pkg/clients/gitclient"
)

type App struct {
	GitClient *gitclient.Client
	Config    *Config
}

type Config struct {
	Service          Service `validate:"required"`
	RegisterWebhooks bool
	LogLevel         string
}

type Service struct {
	GitlabToken string `validate:"required"`
	GitlabURL   string `validate:"required"`
}

func New() (*App, error) {
	var config Config
	if err := configurator.ReadConfig(&config, consul.Src(), vault.Src()); err != nil {
		return nil, err
	}

	httpClient, err := httpclient.NewClient(&httpclient.ClientOptions{Insecure: true})
	if err != nil {
		return nil, err
	}

	gitlabConfig := &gitclient.Config{
		BaseURL:    config.Service.GitlabURL,
		Token:      config.Service.GitlabToken,
		LogEnabled: true,
	}
	gitlabClient, err := gitclient.New(gitlabConfig, httpClient.GetHttpClient(), log.GetLogger())
	if err != nil {
		return nil, err
	}

	app := &App{
		GitClient: gitlabClient,
		Config:    &config,
	}

	return app, nil
}
