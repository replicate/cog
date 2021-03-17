module github.com/replicate/cog

go 1.16

require (
	cloud.google.com/go v0.78.0
	github.com/Microsoft/go-winio v0.4.16 // indirect
	github.com/anaskhan96/soup v1.2.4
	github.com/containerd/containerd v1.4.4 // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/docker v20.10.5+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/docker/go-units v0.4.0 // indirect
	github.com/gorilla/mux v1.8.0
	github.com/kr/text v0.2.0 // indirect
	github.com/mholt/archiver/v3 v3.5.0
	github.com/moby/term v0.0.0-20201110203204-bea5bbe245bf // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/onsi/ginkgo v1.8.0 // indirect
	github.com/onsi/gomega v1.4.3 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/schollz/progressbar/v3 v3.7.6
	github.com/sirupsen/logrus v1.7.0
	github.com/spf13/cobra v1.1.3
	github.com/stretchr/testify v1.7.0
	golang.org/x/net v0.0.0-20210224082022-3d97a244fca7 // indirect
	golang.org/x/oauth2 v0.0.0-20210220000619-9bb904979d93 // indirect
	google.golang.org/genproto v0.0.0-20210222152913-aa3ee6e6a81c
	gopkg.in/airbrake/gobrake.v2 v2.0.9 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	gopkg.in/gemnasium/logrus-airbrake-hook.v2 v2.1.2 // indirect
	gopkg.in/yaml.v2 v2.4.0
	gotest.tools/gotestsum v1.6.2 // indirect
	gotest.tools/v3 v3.0.3 // indirect
)

replace github.com/mholt/archiver/v3 => github.com/bfirsh/archiver/v3 v3.5.1-0.20210316180101-755470a1a69b
