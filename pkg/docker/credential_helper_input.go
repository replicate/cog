package docker

type CredentialHelperInput struct {
	Username  string
	Secret    string //nolint:gosec // G117: this is a Docker credential, not a hardcoded secret
	ServerURL string
}
