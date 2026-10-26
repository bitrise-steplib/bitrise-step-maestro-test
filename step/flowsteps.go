package step

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	commandsFileName = "commands.json"
	maestroLogPath   = "logs/maestro.log"

	dayMs                = 24 * 60 * 60 * 1000
	startTimeToleranceMs = 250
)

var (
	logEntryStart  = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3} `)
	commandLogLine = regexp.MustCompile(`(?s)^(\d{2}):(\d{2}):(\d{2})\.(\d{3}) .*?onCommand(Start|Finished): (.+) (RUNNING|PENDING|COMPLETED|FAILED|WARNED|SKIPPED)$`)
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

// commandRecord is a commands.json entry without its "command" object. That object holds the flow's env, which can
// carry secrets passed with -e, so it is never read.
type commandRecord struct {
	Metadata struct {
		Status    string `json:"status"`
		Timestamp *int64 `json:"timestamp"`
		Duration  int64  `json:"duration"`
		Depth     int    `json:"depth"`
		Error     *struct {
			Message      string `json:"message"`
			DebugMessage string `json:"debugMessage"`
		} `json:"error"`
	} `json:"metadata"`
}

// loggedCommand is a command as maestro.log reports it.
type loggedCommand struct {
	name string
	// startMs is the time of day the command started at, in milliseconds.
	startMs int64
	// status comes from the line that reports the command finished, and is empty if the log has none.
	status string
}

// readFlowSteps takes the result, duration, depth and failure of each step from commands.json, and its name from
// maestro.log. Both list every command that started, in order, so the n-th log entry is the n-th record. The steps
// are returned only when the two files agree on every command, because a wrong name is worse than no list.
func readFlowSteps(flowDir string) ([]flowStep, error) {
	data, err := os.ReadFile(filepath.Join(flowDir, commandsFileName))
	if err != nil {
		return nil, err
	}
	var records []commandRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse %s: %w", commandsFileName, err)
	}

	logged, err := readLoggedCommands(filepath.Join(flowDir, filepath.FromSlash(maestroLogPath)))
	if err != nil {
		return nil, err
	}
	if err := checkSameCommands(logged, records); err != nil {
		return nil, err
	}

	steps := make([]flowStep, len(records))
	for i, record := range records {
		steps[i] = flowStep{
			Name:       logged[i].name,
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

// checkSameCommands compares the two files command by command: the number of commands, the final status of each,
// and the time each started at, relative to the first command (the log has only the time of day, no date).
// Equal counts alone would miss an extra line in one file balanced by a missing line in the other.
func checkSameCommands(logged []loggedCommand, records []commandRecord) error {
	if len(logged) != len(records) {
		return fmt.Errorf("%s has %d commands but %s names %d", commandsFileName, len(records), maestroLogPath, len(logged))
	}

	for i, record := range records {
		command := logged[i]
		if command.status != "" && command.status != record.Metadata.Status {
			return fmt.Errorf("command %d (%q) is %s in %s but %s in %s",
				i+1, command.name, command.status, maestroLogPath, record.Metadata.Status, commandsFileName)
		}
		if record.Metadata.Timestamp == nil || records[0].Metadata.Timestamp == nil {
			return fmt.Errorf("command %d in %s has no timestamp", i+1, commandsFileName)
		}

		logOffset := (command.startMs - logged[0].startMs + dayMs) % dayMs
		recordOffset := *record.Metadata.Timestamp - *records[0].Metadata.Timestamp
		if diff := logOffset - recordOffset; diff > startTimeToleranceMs || diff < -startTimeToleranceMs {
			return fmt.Errorf("command %d (%q) started %d ms after the first one in %s but %d ms in %s",
				i+1, command.name, logOffset, maestroLogPath, recordOffset, commandsFileName)
		}
	}
	return nil
}

// readLoggedCommands lists the commands in the order they started. A command that contains others, such as a
// subflow, finishes after them, so each "finished" line belongs to the innermost command still running.
func readLoggedCommands(logPath string) ([]loggedCommand, error) {
	entries, err := readCommandLogEntries(logPath)
	if err != nil {
		return nil, err
	}

	var commands []loggedCommand
	var running []int
	for _, entry := range entries {
		match := commandLogLine.FindStringSubmatch(entry)
		if match == nil {
			continue
		}
		name, status := strings.TrimRight(match[6], "\n"), match[7]

		if match[5] == "Start" {
			if status == "RUNNING" {
				commands = append(commands, loggedCommand{name: name, startMs: timeOfDayMs(match[1], match[2], match[3], match[4])})
				running = append(running, len(commands)-1)
			}
			continue
		}

		if len(running) == 0 {
			return nil, fmt.Errorf("%s reports %q finished, but no command is running", maestroLogPath, name)
		}
		last := running[len(running)-1]
		running = running[:len(running)-1]
		if commands[last].name != name {
			return nil, fmt.Errorf("%s reports %q finished while %q is the innermost running command", maestroLogPath, name, commands[last].name)
		}
		commands[last].status = status
	}
	return commands, nil
}

// readCommandLogEntries returns the entries of the log that report a command starting or finishing. An entry starts
// with a time of day and goes on over the lines that do not, as the text of a command does when it has several lines.
func readCommandLogEntries(logPath string) ([]string, error) {
	file, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck

	var entries []string
	inCommandEntry := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if logEntryStart.MatchString(line) {
			inCommandEntry = strings.Contains(line, "onCommandStart: ") || strings.Contains(line, "onCommandFinished: ")
			if inCommandEntry {
				entries = append(entries, line)
			}
			continue
		}
		if inCommandEntry {
			entries[len(entries)-1] += "\n" + line
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", maestroLogPath, err)
	}
	return entries, nil
}

func timeOfDayMs(hours, minutes, seconds, millis string) int64 {
	// The regular expression only lets digits through, so the conversions cannot fail.
	h, _ := strconv.ParseInt(hours, 10, 64)
	m, _ := strconv.ParseInt(minutes, 10, 64)
	s, _ := strconv.ParseInt(seconds, 10, 64)
	ms, _ := strconv.ParseInt(millis, 10, 64)
	return ((h*60+m)*60+s)*1000 + ms
}
