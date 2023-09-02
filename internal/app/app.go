package app

import (
	"go.uber.org/zap"
	"net/http"
	"os"
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
	config.Service.GitlabToken = os.Getenv("GITLAB_TOKEN")
	config.Service.GitlabURL = os.Getenv("GITLAB_HOST")
	//if err := configurator.ReadConfig(&config, consul.Src(), vault.Src()); err != nil {
	//	return nil, err
	//}

	//httpClient, err := httpclient.NewClient(&httpclient.ClientOptions{Insecure: true})
	//if err != nil {
	//	return nil, err
	//}

	gitlabConfig := &gitclient.Config{
		BaseURL:    config.Service.GitlabURL,
		Token:      config.Service.GitlabToken,
		LogEnabled: true,
	}

	currentClient := http.Client{}
	z, _ := zap.NewProduction()
	gitlabClient, err := gitclient.New(gitlabConfig, currentClient, z)
	if err != nil {
		return nil, err
	}

	app := &App{
		GitClient: gitlabClient,
		Config:    &config,
	}

	return app, nil
}
