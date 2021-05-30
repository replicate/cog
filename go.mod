module github.com/replicate/cog

go 1.16

require (
	github.com/Microsoft/go-winio v0.4.16 // indirect
	github.com/TylerBrock/colorjson v0.0.0-20200706003622-8a50f05110d2
	github.com/ahmetalpbalkan/dlog v0.0.0-20170105205344-4fb5f8204f26 // indirect
	github.com/ahmetb/dlog v0.0.0-20170105205344-4fb5f8204f26
	github.com/anaskhan96/soup v1.2.4
	github.com/bgentry/speakeasy v0.1.0
	github.com/briandowns/spinner v1.13.0
	github.com/containerd/console v1.0.2
	github.com/containerd/containerd v1.4.4 // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/docker v20.10.5+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/docker/go-units v0.4.0 // indirect
	github.com/fatih/color v1.10.0
	github.com/golang/protobuf v1.4.3 // indirect
	github.com/golangci/golangci-lint v1.40.0
	github.com/gorilla/handlers v1.5.1
	github.com/gorilla/mux v1.8.0
	github.com/hashicorp/waypoint-plugin-sdk v0.0.0-20210527173936-a097f9714b93
	github.com/hokaccha/go-prettyjson v0.0.0-20210113012101-fb4e108d2519 // indirect
	github.com/hpcloud/tail v1.0.0
	github.com/lab47/vterm v0.0.0-20201001232628-a9dd795f94c2
	github.com/logrusorgru/aurora v2.0.3+incompatible
	github.com/mattn/go-isatty v0.0.12
	github.com/mholt/archiver/v3 v3.5.0
	github.com/mitchellh/go-glint v0.0.0-20201119015200-53f6eb3bf4d2
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mitchellh/go-wordwrap v1.0.1
	github.com/moby/term v0.0.0-20201110203204-bea5bbe245bf
	github.com/montanaflynn/stats v0.6.5
	github.com/morikuni/aec v1.0.0
	github.com/olekukonko/tablewriter v0.0.5
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/pkg/profile v1.5.0
	github.com/sabhiram/go-gitignore v0.0.0-20201211210132-54b8a0bf510f
	github.com/schollz/progressbar/v3 v3.7.6
	github.com/segmentio/ksuid v1.0.3
	github.com/spf13/cobra v1.1.3
	github.com/stretchr/testify v1.7.0
	github.com/xeonx/timeago v1.0.0-rc4
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83
	golang.org/x/sys v0.0.0-20210403161142-5e06dd20ab57
	golang.org/x/tools v0.1.1-0.20210430200834-7a6108e9b210
	google.golang.org/genproto v0.0.0-20210222152913-aa3ee6e6a81c // indirect
	google.golang.org/grpc v1.35.0 // indirect
	gopkg.in/fsnotify.v1 v1.4.9 // indirect
	gopkg.in/yaml.v2 v2.4.0
	gotest.tools/gotestsum v1.6.4
)

replace (
	github.com/mholt/archiver/v3 => github.com/bfirsh/archiver/v3 v3.5.1-0.20210316180101-755470a1a69b
	gopkg.in/fsnotify.v1 => github.com/kolaente/fsnotify v1.4.10-0.20200411160148-1bc3c8ff4048
)
