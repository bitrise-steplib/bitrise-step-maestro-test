package step

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bitrise-io/go-steputils/v2/testquarantine"
	"github.com/bitrise-io/go-steputils/v2/testreport"
	"go.yaml.in/yaml/v3"
)

// flowFile is a flow with the name and class name Maestro writes for it into the report.
type flowFile struct {
	Path string
	// Folder is the flow_path folder the flow was found in, empty for a flow file given in flow_path.
	Folder    string
	Name      string
	ClassName string
}

// findFlowFiles returns the flow files in flowPaths the way Maestro tells them from other YAML files: by extension,
// leaving out the workspace config and every file named config. It does not apply the workspace config's flows
// patterns or the tags, Maestro does that.
func findFlowFiles(flowPaths []string, workspaceConfig string) ([]flowFile, error) {
	var flows []flowFile
	for _, flowPath := range flowPaths {
		info, err := os.Stat(flowPath)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			flows = append(flows, readFlowFile(flowPath, ""))
			continue
		}

		err = filepath.WalkDir(flowPath, func(pth string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !isFlowFileName(pth) || sameFile(pth, workspaceConfig) {
				return nil
			}
			flows = append(flows, readFlowFile(pth, flowPath))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return flows, nil
}

func isFlowFileName(pth string) bool {
	ext := filepath.Ext(pth)
	return (ext == ".yaml" || ext == ".yml") && strings.TrimSuffix(filepath.Base(pth), ext) != "config"
}

func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

// readFlowFile reads the name and the junitClassname property from the flow's config section, the part above ---.
// Maestro falls back to the file name for the name, and to the name for the class name.
func readFlowFile(pth, folder string) flowFile {
	flow := flowFile{Path: pth, Folder: folder, Name: strings.TrimSuffix(filepath.Base(pth), filepath.Ext(pth))}

	var config struct {
		Name       string            `yaml:"name"`
		Properties map[string]string `yaml:"properties"`
	}
	if data, err := os.ReadFile(pth); err == nil && bytes.Contains(data, []byte("\n---")) {
		if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&config); err == nil && config.Name != "" {
			flow.Name = config.Name
		}
	}
	flow.ClassName = flow.Name
	if className := config.Properties["junitClassname"]; className != "" {
		flow.ClassName = className
	}
	return flow
}

// reportedClassName is the class name the Step reports a flow with, so that a quarantine entry names one flow of one
// step wherever it was created: Test Reports and Insights both identify a test by its name and class name. It holds
// the platform, as an Android and an iOS step can run the same flow file, and the flow file's path the way Maestro
// writes it into the report: relative to the working directory when the file is inside it.
func reportedClassName(platform Platform, flowPath string) string {
	pth := flowPath
	if abs, err := filepath.Abs(flowPath); err == nil {
		pth = abs
		if wd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(wd, abs); err == nil && filepath.IsLocal(rel) {
				pth = rel
			}
		}
	}
	return string(platform) + ":" + filepath.ToSlash(pth)
}

// quarantinedFlows returns the flows a quarantine entry names. The list holds the quarantined tests of every step
// of the app, so most entries match no flow. An entry can also carry the class name Maestro writes, the flow's name or
// its junitClassname, when it was quarantined from a report the Step did not rewrite; it matches on either platform.
// The suite name is not compared: Maestro names it "Test Suite" unless additional_args sets --test-suite-name.
func quarantinedFlows(quarantined []testquarantine.QuarantinedTest, flows []flowFile, platform Platform) []flowFile {
	var matched []flowFile
	for _, flow := range flows {
		className := reportedClassName(platform, flow.Path)
		if slices.ContainsFunc(quarantined, func(test testquarantine.QuarantinedTest) bool {
			return test.TestCaseName == flow.Name &&
				(test.ClassName == "" || test.ClassName == className || test.ClassName == flow.ClassName)
		}) {
			matched = append(matched, flow)
		}
	}
	return matched
}

// setReportedClassNames replaces the class name Maestro writes, the flow's name or its junitClassname property, with
// the one the Step reports the flow with. A flow keeps Maestro's class name when Maestro does not write its file,
// before 2.6.0.
func setReportedClassNames(report *testreport.TestReport, platform Platform) {
	forEachTestCase(report, func(testCase *testreport.TestCase) {
		if testCase.File != "" {
			testCase.ClassName = reportedClassName(platform, testCase.File)
		}
	})
}

// patternMatchedFlows returns the flows that are not quarantined, but that the exclusion pattern of a quarantined flow
// found in a folder matches too. Maestro applies a pattern to the path inside every flow_path folder.
func patternMatchedFlows(quarantined, flows []flowFile) []flowFile {
	excluded := map[string]bool{}
	for _, flow := range quarantined {
		if rel, ok := pathInFolder(flow); ok {
			excluded[rel] = true
		}
	}

	var matched []flowFile
	for _, flow := range flows {
		if slices.ContainsFunc(quarantined, func(other flowFile) bool { return other.Path == flow.Path }) {
			continue
		}
		if rel, ok := pathInFolder(flow); ok && excluded[rel] {
			matched = append(matched, flow)
		}
	}
	return matched
}

func pathInFolder(flow flowFile) (string, bool) {
	if flow.Folder == "" {
		return "", false
	}
	rel, err := filepath.Rel(flow.Folder, flow.Path)
	return filepath.ToSlash(rel), err == nil
}

// writeQuarantineConfig writes a copy of the workspace config whose flows patterns leave out the quarantined flows
// found in folders. baseConfig is empty when the flows have no workspace config.
func writeQuarantineConfig(baseConfig, dst string, quarantined []flowFile) error {
	document := yaml.Node{Kind: yaml.DocumentNode}
	if baseConfig != "" {
		data, err := os.ReadFile(baseConfig)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(data, &document); err != nil {
			return err
		}
	}
	if len(document.Content) == 0 {
		document.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	mapping := document.Content[0]

	patterns := flowsPatterns(mapping)
	for _, flow := range quarantined {
		if flow.Folder == "" {
			continue
		}
		rel, err := filepath.Rel(flow.Folder, flow.Path)
		if err != nil {
			return err
		}
		patterns.Content = append(patterns.Content, stringNode("!"+escapeGlob(filepath.ToSlash(rel))))
	}

	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return err
	}
	return os.WriteFile(dst, out.Bytes(), 0644)
}

// flowsPatterns returns the flows list of the workspace config, adding Maestro's default when it has none.
func flowsPatterns(mapping *yaml.Node) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != "flows" {
			continue
		}
		value := mapping.Content[i+1]
		switch {
		case value.ShortTag() == "!!null":
			mapping.Content[i+1] = defaultFlowsPatterns()
		case value.Kind == yaml.ScalarNode:
			mapping.Content[i+1] = &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{value}}
		}
		return mapping.Content[i+1]
	}
	patterns := defaultFlowsPatterns()
	mapping.Content = append(mapping.Content, stringNode("flows"), patterns)
	return patterns
}

func defaultFlowsPatterns() *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{stringNode("*")}}
}

func stringNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// escapeGlob puts the glob characters of a file name into a bracket class. A backslash escape does not work, as
// Maestro doubles every backslash of a pattern. A closing ] or } is a literal on its own.
func escapeGlob(pth string) string {
	var escaped strings.Builder
	for _, r := range pth {
		if strings.ContainsRune("*?[{", r) {
			escaped.WriteString("[" + string(r) + "]")
		} else {
			escaped.WriteRune(r)
		}
	}
	return escaped.String()
}
