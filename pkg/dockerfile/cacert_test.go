package dockerfile

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testCertPEM = `-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAKHBfpegPjMCMA0GCSqGSIb3DQEBCwUAMBExDzANBgNVBAMMBlRl
c3RDQTAeFw0yNDAxMDEwMDAwMDBaFw0yNTAxMDEwMDAwMDBaMBExDzANBgNVBAMM
BlRlc3RDQTBcMA0GCSqGSIb3DQEBAQUAA0sAMEgCQQC7o96WzE5gvnMXvPjNdXjH
HwjE7F5Q4X5g5W5P5s5Q5Y5V5y5v5p5o5k5f5d5c5b5a5Z5X5W5U5T5S5R5P5N5L
AgMBAAGjUzBRMB0GA1UdDgQWBBQExample1234567890ABCDEFGHIJKLMN
MB8GA1UdIwQYMBaAFBQExample1234567890ABCDEFGHIJKLMNMA8GA1Ud
EwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADQQBExample1234567890
-----END CERTIFICATE-----`

const testCertPEM2 = `-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAKHBfpegPjMDMA0GCSqGSIb3DQEBCwUAMBExDzANBgNVBAMMBlRl
c3RDQTAeFw0yNDAxMDEwMDAwMDBaFw0yNTAxMDEwMDAwMDBaMBExDzANBgNVBAMM
BlRlc3RDQTBcMA0GCSqGSIb3DQEBAQUAA0sAMEgCQQC8p97XzF6hvoPYvQkOeYkI
HxkF8G6Q5Y6h6X6Q6Z6W6z6w6q6p6l6g6e6d6c6b6a6Y6X6W6V6U6T6S6R6Q6O6M
AgMBAAGjUzBRMB0GA1UdDgQWBBQExample2222222222ABCDEFGHIJKLMN
MB8GA1UdIwQYMBaAFBQExample2222222222ABCDEFGHIJKLMNMA8GA1Ud
EwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADQQBExample2222222222
-----END CERTIFICATE-----`

func TestReadCACert_NotSet(t *testing.T) {
	os.Unsetenv(CACertEnvVar)

	cert, err := ReadCACert()
	require.NoError(t, err)
	require.Nil(t, cert)
}

func TestReadCACert_FilePath(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "test.crt")
	require.NoError(t, os.WriteFile(certPath, []byte(testCertPEM), 0o644))

	t.Setenv(CACertEnvVar, certPath)

	cert, err := ReadCACert()
	require.NoError(t, err)
	require.NotNil(t, cert)
	require.Contains(t, string(cert), "-----BEGIN CERTIFICATE-----")
	require.Contains(t, string(cert), "-----END CERTIFICATE-----")
}

func TestReadCACert_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	// Write two cert files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "cert1.crt"), []byte(testCertPEM), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "cert2.pem"), []byte(testCertPEM2), 0o644))
	// Also write a non-cert file that should be ignored
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("ignore me"), 0o644))

	t.Setenv(CACertEnvVar, tmpDir)

	cert, err := ReadCACert()
	require.NoError(t, err)
	require.NotNil(t, cert)
	// Should contain both certificates
	require.Equal(t, 2, strings.Count(string(cert), "-----BEGIN CERTIFICATE-----"))
	require.Equal(t, 2, strings.Count(string(cert), "-----END CERTIFICATE-----"))
}

func TestReadCACert_InlinePEM(t *testing.T) {
	t.Setenv(CACertEnvVar, testCertPEM)

	cert, err := ReadCACert()
	require.NoError(t, err)
	require.NotNil(t, cert)
	require.Contains(t, string(cert), "-----BEGIN CERTIFICATE-----")
}

func TestReadCACert_Base64EncodedPEM(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(testCertPEM))
	t.Setenv(CACertEnvVar, encoded)

	cert, err := ReadCACert()
	require.NoError(t, err)
	require.NotNil(t, cert)
	require.Contains(t, string(cert), "-----BEGIN CERTIFICATE-----")
}

func TestReadCACert_InvalidPEM(t *testing.T) {
	t.Setenv(CACertEnvVar, "not a valid certificate")

	cert, err := ReadCACert()
	require.Error(t, err)
	require.Nil(t, cert)
	require.Contains(t, err.Error(), "invalid value")
}

func TestReadCACert_MissingFile(t *testing.T) {
	t.Setenv(CACertEnvVar, "/nonexistent/path/to/cert.crt")

	cert, err := ReadCACert()
	require.Error(t, err)
	require.Nil(t, cert)
	require.Contains(t, err.Error(), "invalid value")
}

func TestReadCACert_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(CACertEnvVar, tmpDir)

	cert, err := ReadCACert()
	require.Error(t, err)
	require.Nil(t, cert)
	require.Contains(t, err.Error(), "no .crt or .pem files found")
}

func TestReadCACert_InvalidFileInDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	// Write an invalid cert file
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bad.crt"), []byte("not a cert"), 0o644))

	t.Setenv(CACertEnvVar, tmpDir)

	cert, err := ReadCACert()
	require.Error(t, err)
	require.Nil(t, cert)
	require.Contains(t, err.Error(), "invalid certificate")
}

func TestReadCACert_TrimsWhitespace(t *testing.T) {
	// Test that whitespace is trimmed from the env var value
	t.Setenv(CACertEnvVar, "  "+testCertPEM+"  \n")

	cert, err := ReadCACert()
	require.NoError(t, err)
	require.NotNil(t, cert)
	require.True(t, strings.HasPrefix(string(cert), "-----BEGIN"))
}

func TestGenerateCACertInstall(t *testing.T) {
	var writtenFilename string
	var writtenContents []byte

	mockWriteTemp := func(filename string, contents []byte) ([]string, string, error) {
		writtenFilename = filename
		writtenContents = contents
		return []string{"COPY .cog/tmp/" + filename + " /tmp/" + filename}, "/tmp/" + filename, nil
	}

	result, err := GenerateCACertInstall([]byte(testCertPEM), mockWriteTemp)
	require.NoError(t, err)

	// Check that the cert was written
	require.Equal(t, CACertFilename, writtenFilename)
	require.Contains(t, string(writtenContents), "-----BEGIN CERTIFICATE-----")

	// Check the generated Dockerfile lines
	require.Contains(t, result, "COPY .cog/tmp/"+CACertFilename)
	require.Contains(t, result, "update-ca-certificates")
	require.Contains(t, result, "ENV SSL_CERT_FILE="+SystemCertBundle)
	require.Contains(t, result, "ENV REQUESTS_CA_BUNDLE="+SystemCertBundle)
}

func TestGenerateCACertInstall_EmptyData(t *testing.T) {
	mockWriteTemp := func(filename string, contents []byte) ([]string, string, error) {
		t.Fatal("writeTemp should not be called for empty data")
		return nil, "", nil
	}

	result, err := GenerateCACertInstall(nil, mockWriteTemp)
	require.NoError(t, err)
	require.Empty(t, result)

	result, err = GenerateCACertInstall([]byte{}, mockWriteTemp)
	require.NoError(t, err)
	require.Empty(t, result)
}
