package dockertest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRef(t *testing.T) {
	ref := NewRef(t)
	assert.Equal(t, "testref:latest", ref.String())

	ref = ref.WithTag("v2")
	assert.Equal(t, "testref:v2", ref.String())

	ref = ref.WithRegistry("r8.im")
	assert.Equal(t, "r8.im/testref:v2", ref.String())

	ref = ref.WithoutRegistry()
	assert.Equal(t, "testref:v2", ref.String())

	ref = ref.WithDigest("sha256:71859b0c62df47efaeae4f93698b56a8dddafbf041778fd668bbd1ab45a864f8")
	assert.Equal(t, "testref@sha256:71859b0c62df47efaeae4f93698b56a8dddafbf041778fd668bbd1ab45a864f8", ref.String())
}
