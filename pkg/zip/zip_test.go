package zip

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mholt/archiver/v3"
	"github.com/stretchr/testify/require"
)

func TestCachingZip(t *testing.T) {
	cacheRootDir, err := os.MkdirTemp("/tmp", "cache")
	require.NoError(t, err)
	defer os.RemoveAll(cacheRootDir)
	dataDir, err := os.MkdirTemp("/tmp", "data")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)
	workDir, err := os.MkdirTemp("/tmp", "work")
	require.NoError(t, err)
	defer os.RemoveAll(workDir)
	tempDir, err := os.MkdirTemp("/tmp", "temp")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	unzipDir1, err := os.MkdirTemp("/tmp", "unzip1")
	require.NoError(t, err)
	defer os.RemoveAll(unzipDir1)
	unzipDir2, err := os.MkdirTemp("/tmp", "unzip2")
	require.NoError(t, err)
	defer os.RemoveAll(unzipDir2)
	unzipDir3, err := os.MkdirTemp("/tmp", "unzip3")
	require.NoError(t, err)
	defer os.RemoveAll(unzipDir3)
	unzipDir4, err := os.MkdirTemp("/tmp", "unzip4")
	require.NoError(t, err)
	defer os.RemoveAll(unzipDir4)

	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "my/dir"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "anotherdir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "foo.txt"), []byte("foo"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "my/dir/bar.txt"), []byte("bar"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "anotherdir/baz.txt"), []byte("baz"), 0644))

	z := NewCachingZip()

	outPath := filepath.Join(workDir, "myzip.zip")
	out, err := os.Create(outPath)
	require.NoError(t, err)

	err = z.WriterArchive(dataDir + "/", out, []string{})
	require.NoError(t, err)
	require.NoError(t, out.Close())

	err = new(archiver.Zip).Unarchive(outPath, unzipDir1)
	require.NoError(t, err)
	requireUnzippedCorrectly(t, unzipDir1, "foo", "bar", "baz")

	cacheDir := filepath.Join(cacheRootDir, "my-repo")
	fs, err := NewCacheFileSystem(cacheDir)
	require.NoError(t, err)

	hashes, err := fs.GetHashes()
	require.NoError(t, err)
	require.ElementsMatch(t, hashes, []string{})

	file, err := os.Open(outPath)
	require.NoError(t, err)
	stat, err := file.Stat()
	require.NoError(t, err)
	err = z.ReaderUnarchive(file, stat.Size(), unzipDir2 + "/", fs)
	require.NoError(t, err)
	requireUnzippedCorrectly(t, unzipDir2, "foo", "bar", "baz")

	fs, err = NewCacheFileSystem(cacheDir)
	require.NoError(t, err)
	hashes, err = fs.GetHashes()
	require.NoError(t, err)
	require.ElementsMatch(t, hashes, []string{
		"fcde2b2edba56bf408601fb721fe9b5c338d10ee429ea04fae5511b68fbf8fb9",
		"baa5a0964d3320fbc0c6a922140453c8513ea24ab8fd0577034804a967248096",
		"2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
	})

	outPath2 := filepath.Join(workDir, "myzip2.zip")
	out2, err := os.Create(outPath2)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "anotherdir/baz.txt"), []byte("changed-baz"), 0644))

	err = z.WriterArchive(dataDir + "/", out2, hashes)

	err = new(archiver.Zip).Unarchive(outPath2, unzipDir3)
	require.NoError(t, err)
	requireUnzippedCorrectly(t, unzipDir3, "cogcache2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae", "cogcachefcde2b2edba56bf408601fb721fe9b5c338d10ee429ea04fae5511b68fbf8fb9", "changed-baz")

	fs, err = NewCacheFileSystem(cacheDir)
	require.NoError(t, err)

	file, err = os.Open(outPath2)
	require.NoError(t, err)
	stat, err = file.Stat()
	require.NoError(t, err)
	err = z.ReaderUnarchive(file, stat.Size(), unzipDir4 + "/", fs)
	require.NoError(t, err)
	requireUnzippedCorrectly(t, unzipDir4, "foo", "bar", "changed-baz")

	fs, err = NewCacheFileSystem(cacheDir)
	require.NoError(t, err)
	hashes, err = fs.GetHashes()
	require.NoError(t, err)
	require.ElementsMatch(t, hashes, []string{
		"fcde2b2edba56bf408601fb721fe9b5c338d10ee429ea04fae5511b68fbf8fb9",
		"baa5a0964d3320fbc0c6a922140453c8513ea24ab8fd0577034804a967248096",
		"2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
		"59c1af9b47dd31426dd3dbdfa66869b5e5f0bcde4052d2d6d560976fa3291895",
	})
}

func requireUnzippedCorrectly(t *testing.T, dir string, foo string, bar string, baz string) {
	contents, err := os.ReadFile(filepath.Join(dir, "foo.txt"))
	require.NoError(t, err)
	require.Equal(t, foo, string(contents))
	contents, err = os.ReadFile(filepath.Join(dir, "my/dir/bar.txt"))
	require.NoError(t, err)
	require.Equal(t, bar, string(contents))
	contents, err = os.ReadFile(filepath.Join(dir, "anotherdir/baz.txt"))
	require.NoError(t, err)
	require.Equal(t, baz, string(contents))
}
