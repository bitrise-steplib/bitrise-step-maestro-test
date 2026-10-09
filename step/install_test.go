package step

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bitrise-io/go-utils/v2/filedownloader"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/bitrise-io/go-utils/v2/ziputil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseChecksum(t *testing.T) {
	checksum, err := parseChecksum(strings.NewReader("ABC123  maestro.zip\n"), "maestro.zip")
	require.NoError(t, err)
	assert.Equal(t, "abc123", checksum)

	_, err = parseChecksum(strings.NewReader("abc123  other.zip\n"), "maestro.zip")
	require.Error(t, err)
}

func TestInstall(t *testing.T) {
	archive := fakeMaestroArchive(t, "2.11.0")
	server, downloads := releaseServer(t, "2.11.0", archive, sha256Hex(archive))

	inst := testInstaller(t, server.URL)

	installation, err := inst.Install("2.11.0")
	require.NoError(t, err)

	info, err := os.Stat(installation.BinaryPath)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0100, "maestro binary must stay executable")

	_, err = inst.Install("2.11.0")
	require.NoError(t, err)
	assert.Equal(t, int32(1), downloads.Load(), "an installed version must not be downloaded again")
}

func TestInstall_ChecksumMismatch(t *testing.T) {
	archive := fakeMaestroArchive(t, "2.11.0")
	server, _ := releaseServer(t, "2.11.0", archive, strings.Repeat("0", 64))

	_, err := testInstaller(t, server.URL).Install("2.11.0")
	require.ErrorContains(t, err, "checksum mismatch")
}

func TestInstall_UnknownVersion(t *testing.T) {
	archive := fakeMaestroArchive(t, "2.11.0")
	server, _ := releaseServer(t, "2.11.0", archive, sha256Hex(archive))

	_, err := testInstaller(t, server.URL).Install("9.9.9")
	require.ErrorContains(t, err, "404")
}

func testInstaller(t *testing.T, baseURL string) installer {
	logger := log.NewLogger()
	pathChecker := pathutil.NewPathChecker()
	return installer{
		logger:      logger,
		downloader:  filedownloader.NewDownloader(logger),
		unzipper:    ziputil.NewZipManager(pathChecker),
		pathChecker: pathChecker,
		baseURL:     baseURL,
		rootDir:     t.TempDir(),
	}
}

func releaseServer(t *testing.T, version string, archive []byte, checksum string) (*httptest.Server, *atomic.Int32) {
	var downloads atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/cli-%s/maestro.zip", version), func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		_, _ = w.Write(archive)
	})
	mux.HandleFunc(fmt.Sprintf("/cli-%s/checksums_sha256.txt", version), func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  maestro.zip\n", checksum)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, &downloads
}

func fakeMaestroArchive(t *testing.T, version string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	bin, err := zw.CreateHeader(executableHeader("maestro/bin/maestro"))
	require.NoError(t, err)
	_, err = bin.Write([]byte("#!/bin/sh\necho " + version + "\n"))
	require.NoError(t, err)

	jar, err := zw.Create(filepath.Join("maestro", "lib", fmt.Sprintf("maestro-cli-%s.jar", version)))
	require.NoError(t, err)
	_, err = jar.Write([]byte("jar"))
	require.NoError(t, err)

	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func executableHeader(name string) *zip.FileHeader {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0755)
	return header
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
