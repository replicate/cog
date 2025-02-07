package http

import (
	"net/http"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/global"
)

func ProvideHTTPClient(command command.Command) (*http.Client, error) {
	userInfo, err := command.LoadUserInformation(global.ReplicateRegistryHost)
	if err != nil {
		return nil, err
	}

	client := http.Client{
		Transport: &Transport{
			headers: map[string]string{
				"User-Agent":    UserAgent(),
				"Authorization": "Bearer " + userInfo.Token,
			},
		},
	}

	return &client, nil
}
