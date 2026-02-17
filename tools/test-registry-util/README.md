# `test-registry-util`

A tool for creating and inspecting a local registry for testing. 

## Purpose

We have a lot of intricate image manipulation code that needs to be tested. Mocks are't great for this because we need to make sure the code works with actual data. This tool helps setup real data for a test registry.

## Usage

Image data is stored in `pkg/registry_testhelpers/testdata` and matches the structore expected by `distribution/distribution`. 

During tests an ephemeral registry is spun up on a random local port, populated with the image data, and turn down when the test finishes.

### Booting a registry in a test:

```go
import "github.com/replicate/cog/pkg/registry_testhelpers"

func TestMyFunction(t *testing.T) {
	registryContainer := registry_testhelpers.StartTestRegistry(ctx)
  image := registryContainer.ImageRef("alpine:latest")
	
  // use image as a real image reference
}
```
### Inspect the current images in the registry:

```bash
go run ./tools/test-registry-util catalog
```
will print something like:

```
alpine:latest application/vnd.oci.image.index.v1+json
  index -> sha256:9a0ff41dccad7a96f324a4655a715c623ed3511c7336361ffa9dadcecbdb99e5
  linux/amd64 -> sha256:1c4eef651f65e2f7daee7ee785882ac164b02b78fb74503052a26dc061c90474
  linux/arm64 -> sha256:757d680068d77be46fd1ea20fb21db16f150468c5e7079a08a2e4705aec096ac
python:3.10 application/vnd.oci.image.manifest.v1+json
  single platform image -> sha256:f33bb19d5a518ba7e0353b6da48d58a04ef674de0bab0810e4751230ea1d4b19
```

You can then use these images in your tests using references like:

- `localhost:<port>/alpine:latest` to get a multi-platform index
- `localhost:<port>/alpine:latest` with platform `linux/amd64` to get a single image from a multi-platform index
- `localhost:<port>/alpine:latest@sha256:1c4eef651f65e2f7daee7ee785882ac164b02b78fb74503052a26dc061c90474` to get a specific image
- `localhost:<port>/python:3.10` to get a single-platform image


### Initialize a new registry storage

To create a new directory of images, run:

```
go run ./tools/test-registry-util init
```

This will download all the images specified in `main.go` and save them to `pkg/registry_testhelpers/testdata`.

### Run a registry

This is just a convenience to inspect a registry outside of a test.

```
go run ./tools/test-registry-util run
```
