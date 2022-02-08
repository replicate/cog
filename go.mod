module github.com/replicate/cog

go 1.16

require (
	github.com/anaskhan96/soup v1.2.5
	github.com/docker/cli v20.10.12+incompatible
	github.com/docker/docker v20.10.12+incompatible
	github.com/docker/docker-credential-helpers v0.6.4 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/getkin/kin-openapi v0.89.0
	github.com/golangci/golangci-lint v1.44.0
	github.com/logrusorgru/aurora v2.0.3+incompatible
	github.com/mattn/go-isatty v0.0.14
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/mitchellh/go-homedir v1.1.0
	github.com/moby/term v0.0.0-20201110203204-bea5bbe245bf
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/spf13/cobra v1.3.0
	github.com/stretchr/objx v0.2.0 // indirect
	github.com/stretchr/testify v1.7.0
	github.com/vincent-petithory/dataurl v1.0.0
	github.com/xeipuuv/gojsonschema v1.2.0
	github.com/xeonx/timeago v1.0.0-rc4
	golang.org/x/sys v0.0.0-20211216021012-1d35b9e2eb4e
	golang.org/x/term v0.0.0-20210615171337-6886f2dfbf5b // indirect
	golang.org/x/tools v0.1.9-0.20211228192929-ee1ca4ffc4da
	gopkg.in/yaml.v2 v2.4.0
	gotest.tools/gotestsum v1.7.0
	sigs.k8s.io/yaml v1.3.0
)

replace (
	github.com/mholt/archiver/v3 => github.com/bfirsh/archiver/v3 v3.5.1-0.20210316180101-755470a1a69b
	gopkg.in/fsnotify.v1 => github.com/kolaente/fsnotify v1.4.10-0.20200411160148-1bc3c8ff4048
)
