package client

import (
	"fmt"
	"io"
	"net/http"
)

type HTTPClient interface {
	Do(req *http.Request) (res *http.Response, err error)
}

func NewRequest(httpCfg *HTTPConfig, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Add(httpCfg.HeaderAuthorizationName, fmt.Sprintf("Bearer %s", httpCfg.TokenJWT))

	return req, nil
}
