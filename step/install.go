package step

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/filedownloader"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
)

const (
	defaultReleaseBaseURL = "https://github.com/mobile-dev-inc/maestro/releases/download"
	archiveName           = "maestro.zip"
	checksumsName         = "checksums_sha256.txt"
)

type Installation struct {
	BinaryPath string
}

type Installer interface {
	Install(version string) (Installation, error)
}

type unzipper interface {
	UnZip(zipPth, intoDir string) error
}

type installer struct {
	logger      log.Logger
	downloader  filedownloader.Downloader
	unzipper    unzipper
	pathChecker pathutil.PathChecker
	baseURL     string
	rootDir     string
}

func NewInstaller(logger log.Logger, downloader filedownloader.Downloader, unzipper unzipper, pathChecker pathutil.PathChecker) Installer {
	return installer{
		logger:      logger,
		downloader:  downloader,
		unzipper:    unzipper,
		pathChecker: pathChecker,
		baseURL:     defaultReleaseBaseURL,
		rootDir:     filepath.Join(os.TempDir(), "bitrise-maestro-cli"),
	}
}

func (i installer) Install(version string) (Installation, error) {
	installDir := filepath.Join(i.rootDir, version)
	installation := Installation{BinaryPath: filepath.Join(installDir, "maestro", "bin", "maestro")}

	// Look for the versioned jar instead of running `maestro --version`, which starts a JVM.
	installed, err := i.pathChecker.IsPathExists(filepath.Join(installDir, "maestro", "lib", fmt.Sprintf("maestro-cli-%s.jar", version)))
	if err != nil {
		return Installation{}, err
	}
	if installed {
		i.logger.Donef("Maestro CLI %s is already installed", version)
		return installation, nil
	}

	start := time.Now()
	releaseURL := fmt.Sprintf("%s/cli-%s", i.baseURL, version)

	expectedChecksum, err := i.fetchChecksum(releaseURL + "/" + checksumsName)
	if err != nil {
		return Installation{}, err
	}

	stagingDir, err := os.MkdirTemp("", "maestro-download")
	if err != nil {
		return Installation{}, err
	}
	defer os.RemoveAll(stagingDir) //nolint:errcheck

	archivePath := filepath.Join(stagingDir, archiveName)
	checksum, err := i.download(releaseURL+"/"+archiveName, archivePath)
	if err != nil {
		return Installation{}, err
	}
	if checksum != expectedChecksum {
		return Installation{}, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", archiveName, expectedChecksum, checksum)
	}

	extractDir := filepath.Join(stagingDir, "extracted")
	if err := i.unzipper.UnZip(archivePath, extractDir); err != nil {
		return Installation{}, fmt.Errorf("extract %s: %w", archiveName, err)
	}

	if err := os.MkdirAll(i.rootDir, 0755); err != nil {
		return Installation{}, err
	}
	if err := os.RemoveAll(installDir); err != nil {
		return Installation{}, err
	}
	if err := os.Rename(extractDir, installDir); err != nil {
		return Installation{}, err
	}

	i.logger.Donef("Installed Maestro CLI %s in %s", version, time.Since(start).Round(100*time.Millisecond))
	return installation, nil
}

func (i installer) fetchChecksum(url string) (string, error) {
	body, err := i.downloader.Get(context.Background(), url)
	if err != nil {
		return "", err
	}
	defer body.Close() //nolint:errcheck

	return parseChecksum(body, archiveName)
}

func (i installer) download(url, dst string) (string, error) {
	body, err := i.downloader.Get(context.Background(), url)
	if err != nil {
		return "", err
	}
	defer body.Close() //nolint:errcheck

	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hash), body); err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func parseChecksum(r io.Reader, fileName string) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == fileName {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no checksum found for %s", fileName)
}
