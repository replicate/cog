package http

import (
	"context"
	"net/http"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/global"
)

const UserAgentHeader = "User-Agent"

func ProvideHTTPClient(ctx context.Context, dockerCommand command.Command) (*http.Client, error) {
	userInfo, err := dockerCommand.LoadUserInformation(ctx, global.ReplicateRegistryHost)
	if err != nil {
		return nil, err
	}

	client := http.Client{
		Transport: &Transport{
			headers: map[string]string{
				UserAgentHeader: UserAgent(),
				"Authorization": "Bearer " + userInfo.Token,
				"Content-Type":  "application/json",
			},
		},
	}

	return &client, nil
}
