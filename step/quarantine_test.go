package step

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-steputils/v2/testquarantine"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindFlowFiles(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "login.yaml"), "appId: org.wikipedia\nname: Login\n---\n- launchApp\n")
	writeFile(t, filepath.Join(folder, "numbered.yaml"), "appId: org.wikipedia\nproperties:\n  junitClassname: 42\n---\n- launchApp\n")
	writeFile(t, filepath.Join(folder, "search.yml"), "appId: org.wikipedia\nproperties:\n  junitClassname: Search tests\n---\n- launchApp\n")
	writeFile(t, filepath.Join(folder, "subflows", "onboarding.yaml"), "appId: org.wikipedia\n---\n- tapOn: Skip\n")
	writeFile(t, filepath.Join(folder, "config.yaml"), "flows:\n  - '*'\n")
	writeFile(t, filepath.Join(folder, "subflows", "config.yml"), "flows:\n  - '*'\n")
	writeFile(t, filepath.Join(folder, "workspace.yaml"), "flows:\n  - '*'\n")
	writeFile(t, filepath.Join(folder, "scripts", "query.js"), "output.result = 'Bitrise'\n")
	file := filepath.Join(t.TempDir(), "settings.yaml")
	writeFile(t, file, "appId: org.wikipedia\n---\n- launchApp\n")

	flows, err := findFlowFiles([]string{folder, file}, filepath.Join(folder, "workspace.yaml"))
	require.NoError(t, err)
	assert.Equal(t, []flowFile{
		{Path: filepath.Join(folder, "login.yaml"), Folder: folder, Name: "Login", ClassName: "Login"},
		{Path: filepath.Join(folder, "numbered.yaml"), Folder: folder, Name: "numbered", ClassName: "42"},
		{Path: filepath.Join(folder, "search.yml"), Folder: folder, Name: "search", ClassName: "Search tests"},
		{Path: filepath.Join(folder, "subflows", "onboarding.yaml"), Folder: folder, Name: "onboarding", ClassName: "onboarding"},
		{Path: file, Name: "settings", ClassName: "settings"},
	}, flows)
}

func TestQuarantinedFlows(t *testing.T) {
	login := flowFile{Path: "flows/login.yaml", Name: "Login", ClassName: "Login"}
	search := flowFile{Path: "flows/search.yaml", Name: "search", ClassName: "Search tests"}
	settings := flowFile{Path: "flows/settings.yaml", Name: "settings", ClassName: "settings"}

	quarantined := quarantinedFlows([]testquarantine.QuarantinedTest{
		{TestCaseName: "Login", ClassName: "Login", TestSuiteName: []string{"Test Suite"}},
		{TestCaseName: "search", TestSuiteName: []string{"Test Suite"}},
		{TestCaseName: "settings", ClassName: "com.example.SettingsTest", TestSuiteName: []string{"app"}},
		{TestCaseName: "testSomething", ClassName: "com.example.OtherTest"},
	}, []flowFile{login, search, settings})

	assert.Equal(t, []flowFile{login, search}, quarantined,
		"an entry without a class name matches by name, one of another step's test does not match")
}

func TestWriteQuarantineConfig(t *testing.T) {
	folder := filepath.Join("flows")
	quarantined := []flowFile{
		{Path: filepath.Join(folder, "login.yaml"), Folder: folder},
		{Path: filepath.Join(folder, "nested", "[wip] search.yaml"), Folder: folder},
		{Path: filepath.Join(folder, "checkout{v2}.yaml"), Folder: folder},
		{Path: "other/settings.yaml"},
	}

	dst := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, writeQuarantineConfig("", dst, quarantined))
	assert.Equal(t, `flows:
  - '*'
  - '!login.yaml'
  - '!nested/[[]wip] search.yaml'
  - '!checkout[{]v2}.yaml'
`, readFileString(t, dst), "a flow file given in flow_path is left out of the command instead")

	nullFlows := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, nullFlows, "flows:\n")
	require.NoError(t, writeQuarantineConfig(nullFlows, dst, quarantined[:1]))
	assert.Equal(t, "flows:\n  - '*'\n  - '!login.yaml'\n", readFileString(t, dst), "Maestro runs every flow when flows is empty")

	base := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, base, "flows: \"smoke/*\"\nexecutionOrder:\n  flowsOrder:\n    - Login\nincludeTags: [smoke]\n")
	require.NoError(t, writeQuarantineConfig(base, dst, quarantined[:1]))
	assert.Equal(t, `flows:
  - "smoke/*"
  - '!login.yaml'
executionOrder:
  flowsOrder:
    - Login
includeTags: [smoke]
`, readFileString(t, dst), "the rest of the user's config is kept")
}

func TestRun_SkipsQuarantinedFlowsOfAFolder(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "login.yaml"), "appId: org.wikipedia\nname: Login\n---\n- launchApp\n")
	writeFile(t, filepath.Join(folder, "search.yaml"), "appId: org.wikipedia\n---\n- launchApp\n")
	userConfig := filepath.Join(t.TempDir(), "maestro.yaml")
	writeFile(t, userConfig, "platform:\n  android:\n    disableAnimations: true\n")
	maestro := newFakeMaestro(t, flowBPassesReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	config := retryConfig(t, 0)
	config.FlowPaths = []string{folder}
	config.AdditionalArgs = []string{"--config", userConfig, "--no-reinstall-driver"}
	config.Quarantined = []testquarantine.QuarantinedTest{{TestCaseName: "Login", ClassName: "Login"}}
	result, err := s.Run(config, Installation{BinaryPath: maestro.binary})
	require.NoError(t, err)

	invocations := maestro.invocations(t)
	require.Len(t, invocations, 1)
	generated := filepath.Join(result.WorkDir, "quarantine-config.yaml")
	assert.Contains(t, invocations[0], "--config "+generated+" --no-reinstall-driver "+folder, "the generated config replaces the user's")
	assert.Equal(t, "platform:\n  android:\n    disableAnimations: true\nflows:\n  - '*'\n  - '!login.yaml'\n", readFileString(t, generated))
}

func TestRun_SkipsMaestroWhenEveryFlowIsQuarantined(t *testing.T) {
	flow := filepath.Join(t.TempDir(), "login.yaml")
	writeFile(t, flow, "appId: org.wikipedia\nname: Login\n---\n- launchApp\n")
	maestro := newFakeMaestro(t, flowBPassesReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	config := retryConfig(t, 0)
	config.FlowPaths = []string{flow}
	config.Quarantined = []testquarantine.QuarantinedTest{{TestCaseName: "Login"}}
	result, err := s.Run(config, Installation{BinaryPath: maestro.binary})
	require.NoError(t, err)
	assert.Empty(t, result.Attempts)
	assert.NoFileExists(t, filepath.Join(maestro.dir, "invocations"))
}

func TestRun_SkipsMaestroWhenEveryFlowOfAFolderIsQuarantined(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "login.yaml"), "appId: org.wikipedia\nname: Login\n---\n- launchApp\n")
	writeFile(t, filepath.Join(folder, "search.yaml"), "appId: org.wikipedia\n---\n- launchApp\n")
	maestro := newFakeMaestro(t, flowBPassesReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	config := retryConfig(t, 0)
	config.FlowPaths = []string{folder}
	config.Quarantined = []testquarantine.QuarantinedTest{{TestCaseName: "Login"}, {TestCaseName: "search"}}
	result, err := s.Run(config, Installation{BinaryPath: maestro.binary})
	require.NoError(t, err)
	assert.Empty(t, result.Attempts)
	assert.NoFileExists(t, filepath.Join(maestro.dir, "invocations"))
}

func TestRun_RunsMaestroWhenAFlowOfAnotherFolderIsNotQuarantined(t *testing.T) {
	quarantinedFolder, otherFolder := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(quarantinedFolder, "login.yaml"), "appId: org.wikipedia\nname: Login\n---\n- launchApp\n")
	writeFile(t, filepath.Join(otherFolder, "search.yaml"), "appId: org.wikipedia\n---\n- launchApp\n")
	maestro := newFakeMaestro(t, flowBPassesReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	config := retryConfig(t, 0)
	config.FlowPaths = []string{quarantinedFolder, otherFolder}
	config.Quarantined = []testquarantine.QuarantinedTest{{TestCaseName: "Login"}}
	_, err := s.Run(config, Installation{BinaryPath: maestro.binary})
	require.NoError(t, err)
	require.Len(t, maestro.invocations(t), 1)
}

func TestRun_WarnsWhenAQuarantinePatternMatchesAFlowOfAnotherFolder(t *testing.T) {
	smoke, regression := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(smoke, "login.yaml"), "appId: org.wikipedia\nname: Smoke login\n---\n- launchApp\n")
	writeFile(t, filepath.Join(regression, "login.yaml"), "appId: org.wikipedia\nname: Regression login\n---\n- launchApp\n")
	writeFile(t, filepath.Join(regression, "search.yaml"), "appId: org.wikipedia\n---\n- launchApp\n")
	maestro := newFakeMaestro(t, flowBPassesReport)
	var logs bytes.Buffer
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	s.logger = log.NewLogger(log.WithOutput(&logs))

	config := retryConfig(t, 0)
	config.FlowPaths = []string{smoke, regression}
	config.Quarantined = []testquarantine.QuarantinedTest{{TestCaseName: "Smoke login"}}
	result, err := s.Run(config, Installation{BinaryPath: maestro.binary})
	require.NoError(t, err)

	assert.Contains(t, logs.String(), "Regression login ("+filepath.Join(regression, "login.yaml")+") is not quarantined, but it is skipped too")
	assert.NotContains(t, logs.String(), "Smoke login ("+filepath.Join(smoke, "login.yaml")+") is not quarantined")
	assert.Equal(t, "flows:\n  - '*'\n  - '!login.yaml'\n", readFileString(t, filepath.Join(result.WorkDir, "quarantine-config.yaml")), "the pattern is written once")
}

func TestRun_DoesNotWarnWhenNoOtherFlowMatchesTheQuarantinePattern(t *testing.T) {
	smoke, regression := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(smoke, "login.yaml"), "appId: org.wikipedia\nname: Smoke login\n---\n- launchApp\n")
	writeFile(t, filepath.Join(regression, "search.yaml"), "appId: org.wikipedia\n---\n- launchApp\n")
	maestro := newFakeMaestro(t, flowBPassesReport)
	var logs bytes.Buffer
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	s.logger = log.NewLogger(log.WithOutput(&logs))

	config := retryConfig(t, 0)
	config.FlowPaths = []string{smoke, regression}
	config.Quarantined = []testquarantine.QuarantinedTest{{TestCaseName: "Smoke login"}}
	_, err := s.Run(config, Installation{BinaryPath: maestro.binary})
	require.NoError(t, err)

	assert.NotContains(t, logs.String(), "is not quarantined")
}

func TestRun_DoesNotWarnAboutPatternMatchesWhenMaestroIgnoresThePatterns(t *testing.T) {
	smoke, regression := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(smoke, "login.yaml"), "appId: org.wikipedia\nname: Smoke login\n---\n- launchApp\n")
	writeFile(t, filepath.Join(regression, "login.yaml"), "appId: org.wikipedia\nname: Regression login\n---\n- launchApp\n")
	maestro := newFakeMaestro(t, flowBPassesReport)
	var logs bytes.Buffer
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	s.logger = log.NewLogger(log.WithOutput(&logs))

	config := retryConfig(t, 0)
	config.MaestroVersion = "2.8.0"
	config.FlowPaths = []string{smoke, regression}
	config.Quarantined = []testquarantine.QuarantinedTest{{TestCaseName: "Smoke login"}}
	_, err := s.Run(config, Installation{BinaryPath: maestro.binary})
	require.NoError(t, err)

	assert.NotContains(t, logs.String(), "is not quarantined")
	require.Len(t, maestro.invocations(t), 1, "no flow is skipped, so Maestro runs")
}

func TestRun_SkipsMaestroWhenQuarantinePatternsLeaveNoFlow(t *testing.T) {
	smoke, regression := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(smoke, "login.yaml"), "appId: org.wikipedia\nname: Smoke login\n---\n- launchApp\n")
	writeFile(t, filepath.Join(regression, "login.yaml"), "appId: org.wikipedia\nname: Regression login\n---\n- launchApp\n")
	maestro := newFakeMaestro(t, flowBPassesReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	config := retryConfig(t, 0)
	config.FlowPaths = []string{smoke, regression}
	config.Quarantined = []testquarantine.QuarantinedTest{{TestCaseName: "Smoke login"}}
	result, err := s.Run(config, Installation{BinaryPath: maestro.binary})
	require.NoError(t, err)
	assert.Empty(t, result.Attempts)
	assert.NoFileExists(t, filepath.Join(maestro.dir, "invocations"))
}

func TestConfigFromInput_Quarantined(t *testing.T) {
	input := testInput()
	input.Quarantined = `[{"testCaseName":"Login","className":"Login","testSuiteName":["Test Suite"]}]`
	config, err := configFromInput(input)
	require.NoError(t, err)
	assert.Equal(t, []testquarantine.QuarantinedTest{{TestCaseName: "Login", ClassName: "Login", TestSuiteName: []string{"Test Suite"}}}, config.Quarantined)

	input.Quarantined = "not json"
	_, err = configFromInput(input)
	require.ErrorContains(t, err, "quarantined_tests")
}

func readFileString(t *testing.T, pth string) string {
	data, err := os.ReadFile(pth)
	require.NoError(t, err)
	return string(data)
}
