package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getTestProceduresPath() string {
	_, b, _, _ := runtime.Caller(0)
	basePath := filepath.Dir(filepath.Dir(filepath.Dir(b)))
	return filepath.Join(basePath, "python", "tests", "procedures")
}

func TestPrepareProcedureSourceURL(t *testing.T) {
	t.Parallel()

	t.Run("local file URLs", func(t *testing.T) {
		t.Parallel()

		t.Run("invalid path returns error", func(t *testing.T) {
			t.Parallel()

			badDir, err := PrepareProcedureSourceURL("file:///foo/bar", GenerateRunnerID().String())
			require.ErrorContains(t, err, "no such file or directory")
			assert.Empty(t, badDir)
		})

		t.Run("valid directory is copied correctly", func(t *testing.T) {
			t.Parallel()

			proceduresPath := getTestProceduresPath()
			fooDir := filepath.Join(proceduresPath, "foo")
			srcDir := fmt.Sprintf("file://%s", fooDir)

			fooDst, err := PrepareProcedureSourceURL(srcDir, GenerateRunnerID().String())
			require.NoError(t, err)
			assert.DirExists(t, fooDst)
			assert.FileExists(t, filepath.Join(fooDst, "cog.yaml"))

			fooPy := filepath.Join(fooDst, "predict.py")
			assert.FileExists(t, fooPy)
			fooPyContents, err := os.ReadFile(fooPy)
			require.NoError(t, err)
			assert.Contains(t, string(fooPyContents), "'predicting foo'")
		})

		t.Run("different slots create different directories", func(t *testing.T) {
			t.Parallel()

			proceduresPath := getTestProceduresPath()
			fooDir := filepath.Join(proceduresPath, "foo")
			srcDir := fmt.Sprintf("file://%s", fooDir)

			fooDst, err := PrepareProcedureSourceURL(srcDir, GenerateRunnerID().String())
			require.NoError(t, err)

			fooDst2, err := PrepareProcedureSourceURL(srcDir, GenerateRunnerID().String())
			require.NoError(t, err)

			assert.NotEqual(t, fooDst, fooDst2)
		})
	})

	t.Run("remote HTTP URLs", func(t *testing.T) {
		t.Parallel()

		proceduresPath := getTestProceduresPath()
		fooTar := createMemTarGzFile(t, filepath.Join(proceduresPath, "foo"))
		barTar := createMemTarGzFile(t, filepath.Join(proceduresPath, "bar"))

		testFS := fstest.MapFS{
			"foo.tar.gz": {
				Data: fooTar,
			},
			"bar.tar.gz": {
				Data: barTar,
			},
		}
		fileServer := httptest.NewServer(http.FileServerFS(testFS))
		t.Cleanup(fileServer.Close)

		t.Run("foo tarball is extracted correctly", func(t *testing.T) {
			t.Parallel()

			fooURL := fileServer.URL + "/foo.tar.gz"
			fooDst, err := PrepareProcedureSourceURL(fooURL, GenerateRunnerID().String())
			require.NoError(t, err)
			assert.DirExists(t, fooDst)
			assert.FileExists(t, filepath.Join(fooDst, "cog.yaml"))

			fooPy := filepath.Join(fooDst, "predict.py")
			assert.FileExists(t, fooPy)
			fooPyContents, err := os.ReadFile(fooPy)
			require.NoError(t, err)
			assert.Contains(t, string(fooPyContents), "'predicting foo'")
		})

		t.Run("bar tarball is extracted correctly", func(t *testing.T) {
			t.Parallel()

			barURL := fileServer.URL + "/bar.tar.gz"
			barDst, err := PrepareProcedureSourceURL(barURL, GenerateRunnerID().String())
			require.NoError(t, err)
			assert.DirExists(t, barDst)
			assert.FileExists(t, filepath.Join(barDst, "cog.yaml"))

			barPy := filepath.Join(barDst, "predict.py")
			assert.FileExists(t, barPy)
			barPyContents, err := os.ReadFile(barPy)
			require.NoError(t, err)
			assert.Contains(t, string(barPyContents), "'predicting bar'")
		})

		t.Run("different slots create different directories for same URL", func(t *testing.T) {
			t.Parallel()

			fooURL := fileServer.URL + "/foo.tar.gz"
			fooDst, err := PrepareProcedureSourceURL(fooURL, GenerateRunnerID().String())
			require.NoError(t, err)

			fooDst2, err := PrepareProcedureSourceURL(fooURL, GenerateRunnerID().String())
			require.NoError(t, err)

			assert.NotEqual(t, fooDst2, fooDst)

			barURL := fileServer.URL + "/bar.tar.gz"
			barDst, err := PrepareProcedureSourceURL(barURL, GenerateRunnerID().String())
			require.NoError(t, err)

			barDst2, err := PrepareProcedureSourceURL(barURL, GenerateRunnerID().String())
			require.NoError(t, err)

			assert.NotEqual(t, barDst2, barDst)
		})
	})
}

func createMemTarGzFile(t *testing.T, root string) []byte {
	t.Helper()

	fi, err := os.Stat(root)
	require.NoError(t, err)
	require.True(t, fi.IsDir())

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	require.NoError(t, err)

	twCloseErr := tw.Close()
	gzCloseErr := gz.Close()
	require.NoError(t, twCloseErr)
	require.NoError(t, gzCloseErr)

	return buf.Bytes()
}
