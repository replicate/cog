package model

type Repo struct {
	Host string `json:"host"`
	User string `json:"user"`
	Name string `json:"name"`
}

func (r *Repo) String() string {
	s := ""
	if r.Host != "" {
		s += r.Host + "/"
	}
	return s + r.User + "/" + r.Name
}
