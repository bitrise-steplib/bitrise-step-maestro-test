package step

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-steputils/v2/testasset"
	"github.com/bitrise-io/go-steputils/v2/testreport"
)

const (
	manifestFileName         = "manifest.json"
	attachmentPropertyPrefix = "attachment_"
)

var attachedArtifactKinds = map[string]bool{
	"SCREENSHOT":             true,
	"TAKE_SCREENSHOT":        true,
	"SCREEN_RECORDING":       true,
	"START_SCREEN_RECORDING": true,
	"MAESTRO_LOG":            true,
	"DEVICE_LOG":             true,
	"CRASH_REPORT":           true,
	"ANR_REPORT":             true,
}

type artifactManifest struct {
	Entries []struct {
		Kind         string `json:"kind"`
		RelativePath string `json:"relativePath"`
	} `json:"entries"`
}

// flowBundle is one flow's Maestro output folder and the attachable files in it, relative to the folder.
type flowBundle struct {
	Dir   string
	Files []string
}

// findFlowBundles reads the per-flow folders Maestro writes under <test-output-dir>/<timestamp>/<flow>/,
// keyed by folder name.
func findFlowBundles(testOutputDir string) (map[string]flowBundle, error) {
	manifests, err := filepath.Glob(filepath.Join(testOutputDir, "*", "*", manifestFileName))
	if err != nil {
		return nil, err
	}

	bundles := map[string]flowBundle{}
	for _, manifestPath := range manifests {
		flowDir := filepath.Dir(manifestPath)
		files, err := attachableFiles(flowDir)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", manifestPath, err)
		}
		if len(files) > 0 {
			bundles[filepath.Base(flowDir)] = flowBundle{Dir: flowDir, Files: files}
		}
	}
	return bundles, nil
}

func attachableFiles(flowDir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(flowDir, manifestFileName))
	if err != nil {
		return nil, err
	}
	var manifest artifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var files []string
	for _, entry := range manifest.Entries {
		if !attachedArtifactKinds[entry.Kind] {
			continue
		}
		rel := filepath.Clean(entry.RelativePath)
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}

		err := filepath.WalkDir(filepath.Join(flowDir, rel), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !testasset.IsSupportedAssetType(p) {
				return nil
			}
			relToFlow, err := filepath.Rel(flowDir, p)
			if err != nil {
				return err
			}
			if !seen[relToFlow] {
				seen[relToFlow] = true
				files = append(files, relToFlow)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	slices.Sort(files)
	return files, nil
}

type attachmentLinkResult struct {
	// Linked maps a flow folder name to the bundle whose files are referenced from the report.
	Linked   map[string]flowBundle
	Warnings []string
}

// linkAttachments adds an attachment_N property per bundle file to the test case of the flow. The value
// is the file's path relative to the report's folder: <flow folder>/<path inside the flow folder>.
func linkAttachments(report *testreport.TestReport, bundles map[string]flowBundle) attachmentLinkResult {
	testCasesByFolder := map[string][]suiteTestCase{}
	for suite := range report.TestSuites {
		forEachTestCaseInSuite(&report.TestSuites[suite], func(testCase *testreport.TestCase) {
			folder := flowFolderName(testCase.Name)
			testCasesByFolder[folder] = append(testCasesByFolder[folder], suiteTestCase{TestCase: testCase, Suite: suite})
		})
	}

	result := attachmentLinkResult{Linked: map[string]flowBundle{}}
	for _, folder := range slices.Sorted(maps.Keys(bundles)) {
		if match := collisionFolderSuffix.FindStringSubmatch(folder); match != nil && len(testCasesByFolder[match[1]]) > 1 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("more than one flow is named %q, so Maestro output folder %q may belong to any of them, its files are not attached", testCasesByFolder[match[1]][0].Name, folder))
			continue
		}

		testCases := testCasesByFolder[folder]
		if match := shardFolderSuffix.FindStringSubmatch(folder); len(testCases) == 0 && match != nil {
			testCases = testCasesByFolder[strings.TrimSuffix(folder, match[0])]
			shard, _ := strconv.Atoi(match[1])
			if inShard := inSuite(testCases, shard-1); len(inShard) == 1 {
				testCases = inShard
			}
		}
		switch {
		case len(testCases) == 0:
			result.Warnings = append(result.Warnings, fmt.Sprintf("Maestro output folder %q does not belong to a test case, its files are not attached", folder))
			continue
		case len(testCases) > 1:
			result.Warnings = append(result.Warnings, fmt.Sprintf("more than one flow is named %q, their files are not attached", testCases[0].Name))
			continue
		}

		bundle := bundles[folder]
		addAttachmentProperties(testCases[0].TestCase, folder, bundle.Files)
		result.Linked[folder] = bundle
	}
	return result
}

type suiteTestCase struct {
	*testreport.TestCase
	Suite int
}

func inSuite(testCases []suiteTestCase, suite int) []suiteTestCase {
	return slices.DeleteFunc(slices.Clone(testCases), func(testCase suiteTestCase) bool { return testCase.Suite != suite })
}

var (
	// Maestro suffixes the output folder of a flow in a sharded session (--shard-split, --shard-all) with -shard-N,
	// and writes one top-level test suite per shard, in shard order.
	shardFolderSuffix = regexp.MustCompile(`-shard-(\d+)$`)
	// When a flow's folder name is taken, Maestro suffixes it with -N. With duplicate flow names, such a folder
	// can belong to any of them, or to another flow that is named like the suffixed folder.
	collisionFolderSuffix = regexp.MustCompile(`^(.+)-\d+$`)
)

// flowFolderName mirrors how Maestro names a flow's output folder.
func flowFolderName(flowName string) string {
	return strings.ReplaceAll(flowName, "/", "_")
}

func addAttachmentProperties(testCase *testreport.TestCase, folder string, files []string) {
	if testCase.Properties == nil {
		testCase.Properties = &testreport.Properties{}
	}

	used := map[string]bool{}
	for _, property := range testCase.Properties.Property {
		used[property.Name] = true
	}

	n := 0
	for _, file := range files {
		name := fmt.Sprintf("%s%d", attachmentPropertyPrefix, n)
		for used[name] {
			n++
			name = fmt.Sprintf("%s%d", attachmentPropertyPrefix, n)
		}
		used[name] = true
		testCase.Properties.Property = append(testCase.Properties.Property, testreport.Property{
			Name:  name,
			Value: path.Join(folder, filepath.ToSlash(file)),
		})
	}
}

func forEachTestCase(report *testreport.TestReport, fn func(*testreport.TestCase)) {
	for i := range report.TestSuites {
		forEachTestCaseInSuite(&report.TestSuites[i], fn)
	}
}

func forEachTestCaseInSuite(suite *testreport.TestSuite, fn func(*testreport.TestCase)) {
	for i := range suite.TestCases {
		fn(&suite.TestCases[i])
	}
	for i := range suite.TestSuites {
		forEachTestCaseInSuite(&suite.TestSuites[i], fn)
	}
}
