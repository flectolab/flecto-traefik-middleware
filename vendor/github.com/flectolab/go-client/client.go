package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/flectolab/flecto-manager/common/types"
	"github.com/jonboulle/clockwork"
)

type Client interface {
	Init() error
	GetStateVersion() int
	RedirectMatch(host, uri string) (*types.Redirect, string)
	PageMatch(host, uri string) *types.Page
	Reload() error
	Start(ctx context.Context)
}

func New(cfg *Config) Client {
	c := &client{cfg: cfg, httpClient: cfg.Http.Client, clock: clockwork.NewRealClock()}
	c.State.Store(&State{RedirectMatcher: types.NewRedirectTreeMatcher(), PageMatcher: types.NewPageTreeMatcher()})
	return c
}

type State struct {
	ProjectVersion  int
	RedirectMatcher types.RedirectTreeMatcher
	PageMatcher     types.PageTreeMatcher
}

type client struct {
	cfg        *Config
	httpClient HTTPClient
	State      atomic.Value
	clock      clockwork.Clock
	reloadMu   sync.Mutex
}

func (c *client) Init() error {
	if !c.cfg.AgentType.IsValid() {
		return fmt.Errorf("invalid agent type: %s", c.cfg.AgentType)
	}

	err := c.Reload()
	if err != nil {
		return err
	}

	return nil
}

func (c *client) load() *State {
	return c.State.Load().(*State)
}

func (c *client) RedirectMatch(host, uri string) (*types.Redirect, string) {
	return c.load().RedirectMatcher.Match(host, uri)
}
func (c *client) PageMatch(host, uri string) *types.Page {
	return c.load().PageMatcher.Match(host, uri)
}

func (c *client) GetStateVersion() int {
	return c.load().ProjectVersion
}
func (c *client) Reload() error {
	if !c.reloadMu.TryLock() {
		return nil
	}
	defer c.reloadMu.Unlock()
	version, err := c.getProjectVersion()
	if err != nil {
		return err
	}
	agent := types.Agent{Name: c.cfg.AgentName, Type: c.cfg.AgentType, Version: version}
	if version != c.load().ProjectVersion {
		now := c.clock.Now()
		err = c.loadState()
		duration := c.clock.Now().Sub(now)
		agent.LoadDuration = types.NewDuration(duration)
		if err != nil {
			agent.Status = types.AgentStatusError
			agent.Error = err.Error()
			_ = c.sendAgentStatus(agent)
			return err
		}
		agent.Status = types.AgentStatusSuccess
		return c.sendAgentStatus(agent)
	}
	return c.sendAgentHit(agent.Name)
}

func (c *client) Start(ctx context.Context) {
	ticker := c.clock.NewTimer(c.cfg.IntervalCheck)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.Chan():
			_ = c.Reload()
		case <-ctx.Done():
			return
		}
	}
}

func (c *client) loadState() error {
	redirectTreeMatcher := types.NewRedirectTreeMatcher()
	pagesTreeMatcher := types.NewPageTreeMatcher()
	version, errVersion := c.getProjectVersion()
	if errVersion != nil {
		return errVersion
	}

	redirects, errRedirects := c.getProjectRedirects()
	if errRedirects != nil {
		return errRedirects
	}

	for i := range redirects {
		err := redirectTreeMatcher.Insert(&redirects[i])
		if err != nil {
			return err
		}
	}

	pages, errPages := c.getProjectPages()
	if errPages != nil {
		return errPages
	}
	for i := range pages {
		pagesTreeMatcher.Insert(&pages[i])
	}
	state := &State{ProjectVersion: version, RedirectMatcher: redirectTreeMatcher, PageMatcher: pagesTreeMatcher}
	c.State.Store(state)
	return nil
}

func (c *client) getProjectVersion() (int, error) {
	req, err := NewRequest(c.cfg.Http, http.MethodGet, c.cfg.GetUrlApiVersion(), nil)
	if err != nil {
		return 0, err
	}
	resp, errReq := c.httpClient.Do(req)
	if errReq != nil {
		return 0, errReq
	}
	defer func() { _ = resp.Body.Close() }()

	body, errReadBody := io.ReadAll(resp.Body)
	if errReadBody != nil {
		return 0, errReadBody
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status code for %s: %s (%d) %s", c.cfg.GetUrlApiVersion(), resp.Status, resp.StatusCode, body)
	}

	version, errCastInt := strconv.Atoi(strings.TrimSpace(string(body)))
	if errCastInt != nil {
		return 0, errCastInt
	}

	return version, nil
}

func (c *client) getProjectRedirects() ([]types.Redirect, error) {
	redirects := make([]types.Redirect, 0)
	offset := 0
	limit := 100
	for {
		redirectList := types.RedirectList{}
		url := fmt.Sprintf("%s?limit=%d&offset=%d", c.cfg.GetUrlApiRedirects(), limit, offset)
		req, err := NewRequest(c.cfg.Http, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, errReq := c.httpClient.Do(req)
		if errReq != nil {
			return nil, errReq
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("unexpected status code for %s: %s (%d) %s", c.cfg.GetUrlApiRedirects(), resp.Status, resp.StatusCode, body)
		}

		err = json.NewDecoder(resp.Body).Decode(&redirectList)
		if err != nil {
			return nil, err
		}
		_ = resp.Body.Close()
		redirects = append(redirects, redirectList.Items...)
		offset += limit
		if offset >= redirectList.Total {
			break
		}
	}

	return redirects, nil
}

func (c *client) getProjectPages() ([]types.Page, error) {
	pages := make([]types.Page, 0)
	offset := 0
	limit := 100
	for {
		pageList := types.PageList{}
		url := fmt.Sprintf("%s?limit=%d&offset=%d", c.cfg.GetUrlApiPages(), limit, offset)
		req, err := NewRequest(c.cfg.Http, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, errReq := c.httpClient.Do(req)
		if errReq != nil {
			return nil, errReq
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("unexpected status code for %s: %s (%d) %s", c.cfg.GetUrlApiPages(), resp.Status, resp.StatusCode, body)
		}

		err = json.NewDecoder(resp.Body).Decode(&pageList)
		if err != nil {
			return nil, err
		}
		_ = resp.Body.Close()
		pages = append(pages, pageList.Items...)
		offset += limit
		if offset >= pageList.Total {
			break
		}
	}

	return pages, nil
}

func (c *client) sendAgentStatus(agent types.Agent) error {
	if err := types.ValidateAgent(agent); err != nil {
		return err
	}

	jsonAgent, errMarshal := json.Marshal(agent)
	if errMarshal != nil {
		return errMarshal
	}

	body := bytes.NewReader(jsonAgent)
	req, err := NewRequest(c.cfg.Http, http.MethodPost, c.cfg.GetUrlApiAgents(), body)
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	resp, errReq := c.httpClient.Do(req)
	if errReq != nil {
		return errReq
	}

	if resp.StatusCode != http.StatusOK {
		bodyResp, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code for %s: %s (%d) %s", c.cfg.GetUrlApiAgents(), resp.Status, resp.StatusCode, bodyResp)
	}
	return nil
}

func (c *client) sendAgentHit(name string) error {
	req, err := NewRequest(c.cfg.Http, http.MethodPatch, c.cfg.GetUrlApiAgentsHit(name), nil)
	if err != nil {
		return err
	}

	resp, errReq := c.httpClient.Do(req)
	if errReq != nil {
		return errReq
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code for %s: %s (%d) %s", c.cfg.GetUrlApiAgentsHit(name), resp.Status, resp.StatusCode, body)
	}
	return nil
}
