package dockercredentialhelper

import (
	"fmt"
	"os"

	"github.com/docker/docker-credential-helpers/credentials"

	"github.com/replicate/cog/pkg/settings"
)

type Helper struct{}

// List returns the stored URLs and corresponding usernames.
func (h Helper) List() (urlsToUsernames map[string]string, err error) {
	userSettings, err := settings.LoadUserSettings()
	if err != nil {
		return nil, err
	}
	urlsToUsernames = make(map[string]string)
	for url, authInfo := range userSettings.Auth {
		urlsToUsernames[url] = authInfo.Username
	}
	return urlsToUsernames, nil
}

// Get returns the username and secret to use for a given registry server URL.
func (h Helper) Get(serverURL string) (username string, secret string, err error) {
	userSettings, err := settings.LoadUserSettings()
	if err != nil {
		return "", "", err
	}
	if authInfo, ok := userSettings.Auth[serverURL]; ok {
		f, err := os.OpenFile("/tmp/docker-credential-cog.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			panic(err)
		}

		defer f.Close()

		if _, err = f.WriteString(fmt.Sprintf("%s %s %s\n", authInfo.Username, authInfo.Token, secret)); err != nil {
			panic(err)
		}

		return authInfo.Username, authInfo.Token, nil
	}

	return "", "", credentials.NewErrCredentialsNotFound()
}

// Credentials can't be added by the credential helper, cog login is used
func (h Helper) Add(creds *credentials.Credentials) error {
	return fmt.Errorf("add is not supported by docker-credential-cog")
}

// Credentials can't be deleted by the credential helper
func (h Helper) Delete(serverURL string) error {
	return fmt.Errorf("delete is not supported by docker-credential-cog")
}
