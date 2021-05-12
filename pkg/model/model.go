package model

type Model struct {
	Host string `json:"host"`
	User string `json:"user"`
	Name string `json:"name"`
}

func (m *Model) String() string {
	s := ""
	if m.Host != "" {
		s += m.Host + "/"
	}
	return s + m.User + "/" + m.Name
}
