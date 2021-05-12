package client

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/settings"
)

type cogURL struct {
	mod  *model.Model
	path string
}

func newURL(mod *model.Model, path string, args ...interface{}) *cogURL {
	u := &cogURL{
		mod:  mod,
		path: path,
	}
	if len(args) > 0 {
		u.path = fmt.Sprintf(u.path, args...)
	}
	return u
}

func (u *cogURL) String() string {
	host := hostOrDefault(u.mod)
	return fmt.Sprintf("%s/%s", host, u.path)
}

type Client struct {
}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) newRequest(method string, url *cogURL, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url.String(), body)
	if err != nil {
		return nil, err
	}
	err = c.addAuthHeader(req, url.mod)
	return req, err
}

func (c *Client) addAuthHeader(req *http.Request, mod *model.Model) error {
	host := hostOrDefault(mod)
	token, err := settings.LoadAuthToken(host)
	if err != nil {
		return err
	}
	if token != "" {
		tokenBase64 := base64.StdEncoding.EncodeToString([]byte(token))
		req.Header.Add("Authorization", "Bearer "+tokenBase64)
	}
	return nil
}

func hostOrDefault(mod *model.Model) string {
	if mod.Host != "" {
		return mod.Host
	}
	return global.CogServerAddress
}
