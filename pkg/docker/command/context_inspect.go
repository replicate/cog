package command

type Endpoint struct {
	Host          string `json:"Host"`
	SkipTLSVerify bool   `json:"SkipTLSVerify"`
}

type ContextInspect struct {
	Endpoints map[string]Endpoint `json:"Endpoints"`
}
