package dockerfile

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
	r8HTTP "github.com/replicate/cog/pkg/http"
)

func TestMonobaseMatrixDefaultCUDA(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
  "id": 18,
  "cuda_versions": [
    "11.7",
    "11.8",
    "12.1",
    "12.4",
    "12.6",
    "12.8"
  ],
  "cudnn_versions": [
    "8",
    "9"
  ],
  "python_versions": [
    "3.8",
    "3.9",
    "3.10",
    "3.11",
    "3.12",
    "3.13"
  ],
  "torch_versions": [
    "2.0.0",
    "2.0.1",
    "2.1.0",
    "2.1.1",
    "2.1.2",
    "2.2.0",
    "2.2.1",
    "2.2.2",
    "2.3.0",
    "2.3.1",
    "2.4.0",
    "2.4.1",
    "2.5.0",
    "2.5.1",
    "2.6.0",
    "2.7.0"
  ],
  "torch_cudas": {
    "2.0.0": [
      "11.7",
      "11.8"
    ],
    "2.0.1": [
      "11.7",
      "11.8"
    ],
    "2.1.0": [
      "11.8",
      "12.1"
    ],
    "2.1.1": [
      "11.8",
      "12.1"
    ],
    "2.1.2": [
      "11.8",
      "12.1"
    ],
    "2.2.0": [
      "11.8",
      "12.1"
    ],
    "2.2.1": [
      "11.8",
      "12.1"
    ],
    "2.2.2": [
      "11.8",
      "12.1"
    ],
    "2.3.0": [
      "11.8",
      "12.1"
    ],
    "2.3.1": [
      "11.8",
      "12.1"
    ],
    "2.4.0": [
      "11.8",
      "12.1",
      "12.4"
    ],
    "2.4.1": [
      "11.8",
      "12.1",
      "12.4"
    ],
    "2.5.0": [
      "11.8",
      "12.1",
      "12.4"
    ],
    "2.5.1": [
      "11.8",
      "12.1",
      "12.4"
    ],
    "2.6.0": [
      "11.8",
      "12.4",
      "12.6"
    ],
    "2.7.0": [
      "11.8",
      "12.6",
      "12.8"
    ]
  },
  "venvs": [
    {
      "python": "3.13",
      "torch": "2.7.0",
      "cuda": "cpu"
    },
    {
      "python": "3.13",
      "torch": "2.7.0",
      "cuda": "12.8"
    },
    {
      "python": "3.13",
      "torch": "2.7.0",
      "cuda": "12.6"
    },
    {
      "python": "3.13",
      "torch": "2.7.0",
      "cuda": "11.8"
    },
    {
      "python": "3.13",
      "torch": "2.6.0",
      "cuda": "cpu"
    },
    {
      "python": "3.13",
      "torch": "2.6.0",
      "cuda": "12.6"
    },
    {
      "python": "3.13",
      "torch": "2.6.0",
      "cuda": "12.4"
    },
    {
      "python": "3.13",
      "torch": "2.6.0",
      "cuda": "11.8"
    },
    {
      "python": "3.12",
      "torch": "2.7.0",
      "cuda": "cpu"
    },
    {
      "python": "3.12",
      "torch": "2.7.0",
      "cuda": "12.8"
    },
    {
      "python": "3.12",
      "torch": "2.7.0",
      "cuda": "12.6"
    },
    {
      "python": "3.12",
      "torch": "2.7.0",
      "cuda": "11.8"
    },
    {
      "python": "3.12",
      "torch": "2.6.0",
      "cuda": "cpu"
    },
    {
      "python": "3.12",
      "torch": "2.6.0",
      "cuda": "12.6"
    },
    {
      "python": "3.12",
      "torch": "2.6.0",
      "cuda": "12.4"
    },
    {
      "python": "3.12",
      "torch": "2.6.0",
      "cuda": "11.8"
    },
    {
      "python": "3.12",
      "torch": "2.5.1",
      "cuda": "cpu"
    },
    {
      "python": "3.12",
      "torch": "2.5.1",
      "cuda": "12.4"
    },
    {
      "python": "3.12",
      "torch": "2.5.1",
      "cuda": "12.1"
    },
    {
      "python": "3.12",
      "torch": "2.5.1",
      "cuda": "11.8"
    },
    {
      "python": "3.12",
      "torch": "2.5.0",
      "cuda": "cpu"
    },
    {
      "python": "3.12",
      "torch": "2.5.0",
      "cuda": "12.4"
    },
    {
      "python": "3.12",
      "torch": "2.5.0",
      "cuda": "12.1"
    },
    {
      "python": "3.12",
      "torch": "2.5.0",
      "cuda": "11.8"
    },
    {
      "python": "3.12",
      "torch": "2.4.1",
      "cuda": "cpu"
    },
    {
      "python": "3.12",
      "torch": "2.4.1",
      "cuda": "12.4"
    },
    {
      "python": "3.12",
      "torch": "2.4.1",
      "cuda": "12.1"
    },
    {
      "python": "3.12",
      "torch": "2.4.1",
      "cuda": "11.8"
    },
    {
      "python": "3.12",
      "torch": "2.4.0",
      "cuda": "cpu"
    },
    {
      "python": "3.12",
      "torch": "2.4.0",
      "cuda": "12.4"
    },
    {
      "python": "3.12",
      "torch": "2.4.0",
      "cuda": "12.1"
    },
    {
      "python": "3.12",
      "torch": "2.4.0",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.7.0",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.7.0",
      "cuda": "12.8"
    },
    {
      "python": "3.11",
      "torch": "2.7.0",
      "cuda": "12.6"
    },
    {
      "python": "3.11",
      "torch": "2.7.0",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.6.0",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.6.0",
      "cuda": "12.6"
    },
    {
      "python": "3.11",
      "torch": "2.6.0",
      "cuda": "12.4"
    },
    {
      "python": "3.11",
      "torch": "2.6.0",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.5.1",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.5.1",
      "cuda": "12.4"
    },
    {
      "python": "3.11",
      "torch": "2.5.1",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.5.1",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.5.0",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.5.0",
      "cuda": "12.4"
    },
    {
      "python": "3.11",
      "torch": "2.5.0",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.5.0",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.4.1",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.4.1",
      "cuda": "12.4"
    },
    {
      "python": "3.11",
      "torch": "2.4.1",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.4.1",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.4.0",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.4.0",
      "cuda": "12.4"
    },
    {
      "python": "3.11",
      "torch": "2.4.0",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.4.0",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.3.1",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.3.1",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.3.1",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.3.0",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.3.0",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.3.0",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.2.2",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.2.2",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.2.2",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.2.1",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.2.1",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.2.1",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.2.0",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.2.0",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.2.0",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.1.2",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.1.2",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.1.2",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.1.1",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.1.1",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.1.1",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.1.0",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.1.0",
      "cuda": "12.1"
    },
    {
      "python": "3.11",
      "torch": "2.1.0",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.0.1",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.0.1",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.0.1",
      "cuda": "11.7"
    },
    {
      "python": "3.11",
      "torch": "2.0.0",
      "cuda": "cpu"
    },
    {
      "python": "3.11",
      "torch": "2.0.0",
      "cuda": "11.8"
    },
    {
      "python": "3.11",
      "torch": "2.0.0",
      "cuda": "11.7"
    },
    {
      "python": "3.10",
      "torch": "2.7.0",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.7.0",
      "cuda": "12.8"
    },
    {
      "python": "3.10",
      "torch": "2.7.0",
      "cuda": "12.6"
    },
    {
      "python": "3.10",
      "torch": "2.7.0",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.6.0",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.6.0",
      "cuda": "12.6"
    },
    {
      "python": "3.10",
      "torch": "2.6.0",
      "cuda": "12.4"
    },
    {
      "python": "3.10",
      "torch": "2.6.0",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.5.1",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.5.1",
      "cuda": "12.4"
    },
    {
      "python": "3.10",
      "torch": "2.5.1",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.5.1",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.5.0",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.5.0",
      "cuda": "12.4"
    },
    {
      "python": "3.10",
      "torch": "2.5.0",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.5.0",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.4.1",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.4.1",
      "cuda": "12.4"
    },
    {
      "python": "3.10",
      "torch": "2.4.1",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.4.1",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.4.0",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.4.0",
      "cuda": "12.4"
    },
    {
      "python": "3.10",
      "torch": "2.4.0",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.4.0",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.3.1",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.3.1",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.3.1",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.3.0",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.3.0",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.3.0",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.2.2",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.2.2",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.2.2",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.2.1",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.2.1",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.2.1",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.2.0",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.2.0",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.2.0",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.1.2",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.1.2",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.1.2",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.1.1",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.1.1",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.1.1",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.1.0",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.1.0",
      "cuda": "12.1"
    },
    {
      "python": "3.10",
      "torch": "2.1.0",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.0.1",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.0.1",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.0.1",
      "cuda": "11.7"
    },
    {
      "python": "3.10",
      "torch": "2.0.0",
      "cuda": "cpu"
    },
    {
      "python": "3.10",
      "torch": "2.0.0",
      "cuda": "11.8"
    },
    {
      "python": "3.10",
      "torch": "2.0.0",
      "cuda": "11.7"
    },
    {
      "python": "3.9",
      "torch": "2.7.0",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.7.0",
      "cuda": "12.8"
    },
    {
      "python": "3.9",
      "torch": "2.7.0",
      "cuda": "12.6"
    },
    {
      "python": "3.9",
      "torch": "2.7.0",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.6.0",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.6.0",
      "cuda": "12.6"
    },
    {
      "python": "3.9",
      "torch": "2.6.0",
      "cuda": "12.4"
    },
    {
      "python": "3.9",
      "torch": "2.6.0",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.5.1",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.5.1",
      "cuda": "12.4"
    },
    {
      "python": "3.9",
      "torch": "2.5.1",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.5.1",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.5.0",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.5.0",
      "cuda": "12.4"
    },
    {
      "python": "3.9",
      "torch": "2.5.0",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.5.0",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.4.1",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.4.1",
      "cuda": "12.4"
    },
    {
      "python": "3.9",
      "torch": "2.4.1",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.4.1",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.4.0",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.4.0",
      "cuda": "12.4"
    },
    {
      "python": "3.9",
      "torch": "2.4.0",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.4.0",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.3.1",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.3.1",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.3.1",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.3.0",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.3.0",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.3.0",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.2.2",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.2.2",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.2.2",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.2.1",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.2.1",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.2.1",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.2.0",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.2.0",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.2.0",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.1.2",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.1.2",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.1.2",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.1.1",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.1.1",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.1.1",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.1.0",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.1.0",
      "cuda": "12.1"
    },
    {
      "python": "3.9",
      "torch": "2.1.0",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.0.1",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.0.1",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.0.1",
      "cuda": "11.7"
    },
    {
      "python": "3.9",
      "torch": "2.0.0",
      "cuda": "cpu"
    },
    {
      "python": "3.9",
      "torch": "2.0.0",
      "cuda": "11.8"
    },
    {
      "python": "3.9",
      "torch": "2.0.0",
      "cuda": "11.7"
    },
    {
      "python": "3.8",
      "torch": "2.4.1",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.4.1",
      "cuda": "12.4"
    },
    {
      "python": "3.8",
      "torch": "2.4.1",
      "cuda": "12.1"
    },
    {
      "python": "3.8",
      "torch": "2.4.1",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.4.0",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.4.0",
      "cuda": "12.4"
    },
    {
      "python": "3.8",
      "torch": "2.4.0",
      "cuda": "12.1"
    },
    {
      "python": "3.8",
      "torch": "2.4.0",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.3.1",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.3.1",
      "cuda": "12.1"
    },
    {
      "python": "3.8",
      "torch": "2.3.1",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.3.0",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.3.0",
      "cuda": "12.1"
    },
    {
      "python": "3.8",
      "torch": "2.3.0",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.2.2",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.2.2",
      "cuda": "12.1"
    },
    {
      "python": "3.8",
      "torch": "2.2.2",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.2.1",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.2.1",
      "cuda": "12.1"
    },
    {
      "python": "3.8",
      "torch": "2.2.1",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.2.0",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.2.0",
      "cuda": "12.1"
    },
    {
      "python": "3.8",
      "torch": "2.2.0",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.1.2",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.1.2",
      "cuda": "12.1"
    },
    {
      "python": "3.8",
      "torch": "2.1.2",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.1.1",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.1.1",
      "cuda": "12.1"
    },
    {
      "python": "3.8",
      "torch": "2.1.1",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.1.0",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.1.0",
      "cuda": "12.1"
    },
    {
      "python": "3.8",
      "torch": "2.1.0",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.0.1",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.0.1",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.0.1",
      "cuda": "11.7"
    },
    {
      "python": "3.8",
      "torch": "2.0.0",
      "cuda": "cpu"
    },
    {
      "python": "3.8",
      "torch": "2.0.0",
      "cuda": "11.8"
    },
    {
      "python": "3.8",
      "torch": "2.0.0",
      "cuda": "11.7"
    }
  ]
}`))
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(MonobaseMatrixHostVarName, url.Host)
	t.Setenv(MonobaseMatrixSchemeVarName, url.Scheme)

	// Setup mock command
	command := dockertest.NewMockCommand()

	// Setup http client
	httpClient, err := r8HTTP.ProvideHTTPClient(t.Context(), command)
	require.NoError(t, err)

	monobaseMatrix, err := NewMonobaseMatrix(httpClient)
	require.NoError(t, err)

	defaultCuda := monobaseMatrix.DefaultCUDAVersion("2.7.0")
	require.Equal(t, defaultCuda, "12.8")
}
