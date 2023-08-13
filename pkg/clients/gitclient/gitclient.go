package gitclient

import (
	"net/http"

	"github.com/xanzy/go-gitlab"
	"go.uber.org/zap"
	"fmt"
)

type Config struct {
	BaseURL    string `validate:"required"`
	Token      string `validate:"required"`
	LogEnabled bool
}

type Client struct {
	c          *gitlab.Client
	baseLabels gitlab.Labels
}

func New(c *Config, httpClient http.Client, logger *zap.Logger) (*Client, error) {
	baseLabels := gitlab.Labels{"release-operator"}
	zl := &ZapWrapper{logger}
	client, err := gitlab.NewClient(c.Token,
		gitlab.WithoutRetries(),
		gitlab.WithBaseURL(c.BaseURL),
		gitlab.WithHTTPClient(&httpClient),
		gitlab.WithCustomLogger(zl),
		gitlab.WithCustomLeveledLogger(zl),
	)
	return &Client{c: client, baseLabels: baseLabels}, err
}

func (c *Client) GetProject(pid string) (*gitlab.Project, bool, error) {
	project, r, err := c.c.Projects.GetProject(pid, &gitlab.GetProjectOptions{})
	if err != nil {
		if r != nil {
			if r.StatusCode == 404 {
				return nil, false, nil
			}
		}
		return nil, false, err
	}

	return project, true, nil
}

func (c *Client) RemoveBranch(pid, branchName string) error {
	_, exist, err := c.GetBranch(pid, branchName)
	if err != nil {
		return err
	}
	if exist {
		_, err = c.c.Branches.DeleteBranch(pid, branchName)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) CreateBranch(pid, branchName, from string) error {
	_, _, err := c.c.Branches.CreateBranch(pid, &gitlab.CreateBranchOptions{
		Branch: &branchName,
		Ref:    &from,
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) GetBranch(pid, branchName string) (*gitlab.Branch, bool, error) {
	branch, r, err := c.c.Branches.GetBranch(pid, branchName)
	if err != nil {
		if r != nil {
			if r.StatusCode == 404 {
				return nil, false, nil
			}
		}
		return nil, false, err
	}
	return branch, true, nil
}

func (c *Client) CreateMR(pid string, opts *gitlab.CreateMergeRequestOptions) (*gitlab.MergeRequest, error) {
	mr, _, err := c.c.MergeRequests.CreateMergeRequest(pid, opts)
	if err != nil {
		return nil, err
	}
	return mr, nil
}

func (c *Client) RemoveMR(pid string, id int) error {
	_, err := c.c.MergeRequests.DeleteMergeRequest(pid, id)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) RecreateBranch(pid string, branchName string) error {
	branch, branchExist, err := c.GetBranch(pid, branchName)
	if err != nil {
		return err
	}

	if branchExist {
		err = c.RemoveBranch(pid, branch.Name)
		if err != nil {
			return err
		}
	}

	err = c.CreateBranch(pid, branchName, "main")
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) GetOrCreateBranch(pid string, branchName string) error {
	_, branchExist, err := c.GetBranch(pid, branchName)
	if err != nil {
		return err
	}
	if !branchExist {
		err = c.CreateBranch(pid, branchName, "main")
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) AcceptMR(pid string, id int) error {
	_, _, err := c.c.MergeRequests.AcceptMergeRequest(pid, id, &gitlab.AcceptMergeRequestOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) GetMergeRequest(pid string, IID int) (*gitlab.MergeRequest, bool, error) {
	mr, r, err := c.c.MergeRequests.GetMergeRequest(pid, IID, &gitlab.GetMergeRequestsOptions{})
	if err != nil {
		if r != nil {
			if r.StatusCode == 404 {
				return nil, false, nil
			}
		}
		return nil, false, err
	}

	return mr, true, nil
}

func (c *Client) GetMR(pid, branchName, targetBranch string) (*gitlab.MergeRequest, bool, error) {
	openedState := "opened"
	withMergeStatusRecheck := true
	mrs, _, err := c.c.MergeRequests.ListProjectMergeRequests(pid, &gitlab.ListProjectMergeRequestsOptions{
		SourceBranch:           &branchName,
		TargetBranch:           &targetBranch,
		State:                  &openedState,
		WithMergeStatusRecheck: &withMergeStatusRecheck,
	})
	if err != nil {
		return nil, false, err
	}
	if len(mrs) > 0 {
		mr, _, err := c.c.MergeRequests.GetMergeRequest(pid, mrs[0].IID, &gitlab.GetMergeRequestsOptions{})
		if err != nil {
			return nil, false, err
		}
		return mr, true, nil
	}

	return nil, false, nil
}

func (c *Client) GetOrCreateMR(pid, branchName, targetBranch string, IID int) (*gitlab.MergeRequest, error) {
	if IID > 0 {
		mr, exist, err := c.GetMergeRequest(pid, IID)
		if err != nil {
			return nil, err
		}
		if exist {
			return mr, nil
		}
	}
	notRemoveBranch := false
	title := fmt.Sprintf("Release operator: merge branch %s to %s", branchName, targetBranch)
	mr, err := c.CreateMR(pid, &gitlab.CreateMergeRequestOptions{
		Title:              &title,
		SourceBranch:       &branchName,
		TargetBranch:       &targetBranch,
		Labels:             &c.baseLabels,
		RemoveSourceBranch: &notRemoveBranch,
	})
	if err != nil {
		return nil, err
	}

	return mr, nil
}

func (c *Client) RemoveMRIfExist(pid string, IID int) error {
	mr, exist, err := c.GetMergeRequest(pid, IID)
	if err != nil {
		return err
	}
	if exist {
		err := c.RemoveMR(pid, mr.IID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) MergeBranch(pid, branchName, targetBranch string) (bool, error) {
	notRemoveBranch := false
	title := fmt.Sprintf("Release operator: merge branch %s to %s", branchName, targetBranch)
	mr, err := c.CreateMR(pid, &gitlab.CreateMergeRequestOptions{
		Title:              &title,
		SourceBranch:       &branchName,
		TargetBranch:       &targetBranch,
		Labels:             &c.baseLabels,
		RemoveSourceBranch: &notRemoveBranch,
	})
	if err != nil {
		return false, err
	}
	if mr.HasConflicts {
		err := c.RemoveMR(pid, mr.IID)
		if err != nil {
			return mr.HasConflicts, err
		}
		return mr.HasConflicts, nil
	}

	err = c.AcceptMR(pid, mr.IID)
	if err != nil {
		return false, err
	}

	return false, nil
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
