package app

import (
	"net/http"
	"os"

	"github.com/dhnikolas/release-operator/pkg/clients/gitclient"
	"go.uber.org/zap"
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
