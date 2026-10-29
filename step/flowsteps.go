package step

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	commandsFileName = "commands.json"
	maestroLogPath   = "logs/maestro.log"

	commandMetadataKind = "COMMAND_METADATA"
	maestroLogKind      = "MAESTRO_LOG"
)

var (
	logEntryStart    = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3} `)
	commandStartLine = regexp.MustCompile(`(?s)^\d{2}:\d{2}:\d{2}\.\d{3} .*?onCommandStart: (.+) RUNNING$`)
)

// flowStep is one command Maestro ran in a flow, in the order it ran.
type flowStep struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMs int64  `json:"durationMs"`
	Depth      int    `json:"depth"`
	// Message and Details explain a failure.
	Message string `json:"message,omitempty"`
	Details string `json:"details,omitempty"`
}

// commandRecord is a commands.json entry without its "command" object and without metadata.evaluatedCommand. Both
// hold the flow's env, which can carry secrets passed with -e (evaluatedCommand already resolved), so neither is read.
type commandRecord struct {
	Metadata struct {
		Status   string `json:"status"`
		Duration int64  `json:"duration"`
		Depth    int    `json:"depth"`
		Error    *struct {
			Message      string `json:"message"`
			DebugMessage string `json:"debugMessage"`
		} `json:"error"`
	} `json:"metadata"`
}

// flowFilePaths returns where commands.json and maestro.log are in the flow's folder, relative to it. The manifest
// names them, and the default paths are used for what it does not.
func flowFilePaths(flowDir string) (commandsPath, logPath string) {
	commandsPath, logPath = commandsFileName, maestroLogPath

	data, err := os.ReadFile(filepath.Join(flowDir, manifestFileName))
	if err != nil {
		return commandsPath, logPath
	}
	var manifest artifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return commandsPath, logPath
	}

	for _, entry := range manifest.Entries {
		rel := filepath.Clean(filepath.FromSlash(entry.RelativePath))
		if entry.RelativePath == "" || !filepath.IsLocal(rel) {
			continue
		}
		switch entry.Kind {
		case commandMetadataKind:
			commandsPath = rel
		case maestroLogKind:
			logPath = rel
		}
	}
	return commandsPath, logPath
}

// readFlowSteps takes the result, duration, depth and failure of each step from commands.json, and its name from
// maestro.log. Both list every command that started, in order, so the n-th name in the log belongs to the n-th record.
// The steps are returned only when the two files list the same number of commands, because a wrong name is worse than
// no list.
func readFlowSteps(flowDir string) ([]flowStep, error) {
	commandsPath, logPath := flowFilePaths(flowDir)

	data, err := os.ReadFile(filepath.Join(flowDir, commandsPath))
	if err != nil {
		return nil, err
	}
	var records []commandRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.ToSlash(commandsPath), err)
	}

	names, err := readCommandNames(filepath.Join(flowDir, logPath))
	if err != nil {
		return nil, err
	}
	if len(names) != len(records) {
		return nil, fmt.Errorf("%s has %d commands but %s names %d", filepath.ToSlash(commandsPath), len(records), filepath.ToSlash(logPath), len(names))
	}

	steps := make([]flowStep, len(records))
	for i, record := range records {
		steps[i] = flowStep{
			Name:       names[i],
			Status:     record.Metadata.Status,
			DurationMs: record.Metadata.Duration,
			Depth:      record.Metadata.Depth,
		}
		if record.Metadata.Error != nil {
			steps[i].Message = record.Metadata.Error.Message
			steps[i].Details = record.Metadata.Error.DebugMessage
		}
	}
	return steps, nil
}

// readCommandNames lists the names of the commands in the order they started.
func readCommandNames(logPath string) ([]string, error) {
	entries, err := readCommandStartEntries(logPath)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if match := commandStartLine.FindStringSubmatch(entry); match != nil {
			names = append(names, strings.TrimRight(match[1], "\n"))
		}
	}
	return names, nil
}

// readCommandStartEntries returns the entries of the log that report a command starting. An entry starts with a time
// of day and goes on over the lines that do not, as the text of a command does when it has several lines.
func readCommandStartEntries(logPath string) ([]string, error) {
	file, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck

	var entries []string
	inCommandStart := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if logEntryStart.MatchString(line) {
			inCommandStart = strings.Contains(line, "onCommandStart: ")
			if inCommandStart {
				entries = append(entries, line)
			}
			continue
		}
		if inCommandStart {
			entries[len(entries)-1] += "\n" + line
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(logPath), err)
	}
	return entries, nil
}
