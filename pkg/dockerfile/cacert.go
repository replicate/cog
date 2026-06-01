package dockerfile

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/util/console"
)

const (
	// CACertEnvVar is the environment variable that specifies the CA certificate to inject
	CACertEnvVar = "COG_CA_CERT"

	// CACertFilename is the filename used for the CA cert in the build context and container
	CACertFilename = "cog-ca-cert.crt"

	// CACertContainerPath is where the cert is installed in the container
	CACertContainerPath = "/usr/local/share/ca-certificates/" + CACertFilename

	// SystemCertBundle is the path to the system certificate bundle after update-ca-certificates
	SystemCertBundle = "/etc/ssl/certs/ca-certificates.crt"
)

// ReadCACert reads the CA certificate from the COG_CA_CERT environment variable.
// It supports multiple input formats:
//   - File path: /path/to/cert.crt
//   - Directory: /path/to/certs/ (concatenates all *.crt and *.pem files)
//   - Inline PEM: -----BEGIN CERTIFICATE-----...
//   - Base64-encoded PEM: LS0tLS1CRUdJTi...
//
// Returns:
//   - (nil, nil) if COG_CA_CERT is not set (no-op case)
//   - (certBytes, nil) if a valid certificate was found
//   - (nil, error) if the input is invalid
func ReadCACert() ([]byte, error) {
	value := os.Getenv(CACertEnvVar)
	if value == "" {
		return nil, nil
	}

	value = strings.TrimSpace(value)

	// Check if it's a file path
	if info, err := os.Stat(value); err == nil { //nolint:gosec // G703: path from trusted COG_CA_CERT env var
		if info.IsDir() {
			return readCACertDirectory(value)
		}
		return readCACertFile(value)
	}

	// Check if it's inline PEM
	if strings.HasPrefix(value, "-----BEGIN") {
		return validatePEM([]byte(value))
	}

	// Try base64 decoding
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err == nil && strings.HasPrefix(string(decoded), "-----BEGIN") {
		return validatePEM(decoded)
	}

	return nil, fmt.Errorf("%s: invalid value - must be a file path, directory, PEM certificate, or base64-encoded PEM", CACertEnvVar)
}

// readCACertFile reads a single certificate file
func readCACertFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G703: path from trusted COG_CA_CERT env var
	if err != nil {
		return nil, fmt.Errorf("%s: failed to read file %s: %w", CACertEnvVar, path, err)
	}
	return validatePEM(data)
}

// readCACertDirectory reads all .crt and .pem files from a directory and concatenates them
func readCACertDirectory(dir string) ([]byte, error) {
	var certs []byte

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to read directory %s: %w", CACertEnvVar, dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".crt" && ext != ".pem" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path) //nolint:gosec // G703: path from trusted COG_CA_CERT env var directory
		if err != nil {
			return nil, fmt.Errorf("%s: failed to read file %s: %w", CACertEnvVar, path, err)
		}

		// Validate each cert
		if _, err := validatePEM(data); err != nil {
			return nil, fmt.Errorf("%s: invalid certificate in %s: %w", CACertEnvVar, path, err)
		}

		if len(certs) > 0 && !strings.HasSuffix(string(certs), "\n") {
			certs = append(certs, '\n')
		}
		certs = append(certs, data...)
	}

	if len(certs) == 0 {
		return nil, fmt.Errorf("%s: no .crt or .pem files found in directory %s", CACertEnvVar, dir)
	}

	return certs, nil
}

// validatePEM checks that the data looks like a valid PEM certificate
func validatePEM(data []byte) ([]byte, error) {
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "-----BEGIN CERTIFICATE-----") {
		return nil, fmt.Errorf("invalid PEM: must start with '-----BEGIN CERTIFICATE-----'")
	}
	if !strings.Contains(content, "-----END CERTIFICATE-----") {
		return nil, fmt.Errorf("invalid PEM: must contain '-----END CERTIFICATE-----'")
	}
	return []byte(content + "\n"), nil
}

// GenerateCACertInstall generates the Dockerfile lines to install a CA certificate.
// It writes the cert to the build context and returns the Dockerfile lines.
//
// The returned lines:
//  1. COPY the cert to /usr/local/share/ca-certificates/
//  2. RUN update-ca-certificates
//  3. Set SSL_CERT_FILE and REQUESTS_CA_BUNDLE env vars
//
// Parameters:
//   - certData: The PEM-encoded certificate data
//   - writeTemp: Function to write a file to the build context (returns COPY lines and container path)
//
// Returns the Dockerfile lines to add, or error
func GenerateCACertInstall(certData []byte, writeTemp func(filename string, contents []byte) ([]string, string, error)) (string, error) {
	if len(certData) == 0 {
		return "", nil
	}

	console.Infof("Injecting CA certificate from %s", CACertEnvVar)

	// Write cert to build context
	copyLines, _, err := writeTemp(CACertFilename, certData)
	if err != nil {
		return "", fmt.Errorf("failed to write CA certificate to build context: %w", err)
	}

	lines := []string{}
	lines = append(lines, copyLines...)

	// Copy to system CA directory, update the certificate store, and set env vars.
	// Also append the cert directly to the bundle file as a fallback for images
	// where update-ca-certificates may not work as expected.
	lines = append(lines,
		fmt.Sprintf("RUN cp /tmp/%s %s && update-ca-certificates && cat /tmp/%s >> %s", CACertFilename, CACertContainerPath, CACertFilename, SystemCertBundle),
		fmt.Sprintf("ENV SSL_CERT_FILE=%s", SystemCertBundle),
		fmt.Sprintf("ENV REQUESTS_CA_BUNDLE=%s", SystemCertBundle),
	)

	return strings.Join(lines, "\n"), nil
}
