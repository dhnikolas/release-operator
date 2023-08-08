package gitclient

import (
	"net/http"

	"github.com/xanzy/go-gitlab"
	"go.uber.org/zap"
)

type GitlabClient interface {
	CreateBranch()
}

type Config struct {
	BaseURL    string `validate:"required"`
	Token      string `validate:"required"`
	LogEnabled bool
}

type Client struct {
	c *gitlab.Client
}

func New(c *Config, httpClient http.Client, logger *zap.Logger) (*gitlab.Client, error) {
	zl := &ZapWrapper{logger}
	client, err := gitlab.NewClient(c.Token,
		gitlab.WithoutRetries(),
		gitlab.WithBaseURL(c.BaseURL),
		gitlab.WithHTTPClient(&httpClient),
		gitlab.WithCustomLogger(zl),
		gitlab.WithCustomLeveledLogger(zl),
	)
	return client, err
}

func (c *Client) CreateBranch() {
}

type ZapWrapper struct {
	*zap.Logger
}

func (l *ZapWrapper) Printf(tmpl string, keys ...interface{}) {
	l.Logger.Sugar().Infof(tmpl, keys...)
}

func (l *ZapWrapper) Error(msg string, keysAndValues ...interface{}) {
	l.Logger.Sugar().Error(append([]interface{}{msg}, keysAndValues...))
}

func (l *ZapWrapper) Info(msg string, keysAndValues ...interface{}) {
	l.Logger.Sugar().Info(append([]interface{}{msg}, keysAndValues...))
}

func (l *ZapWrapper) Debug(msg string, keysAndValues ...interface{}) {
	l.Logger.Sugar().Debug(append([]interface{}{msg}, keysAndValues...))
}

func (l *ZapWrapper) Warn(msg string, keysAndValues ...interface{}) {
	l.Logger.Sugar().Warn(append([]interface{}{msg}, keysAndValues...))
}
