// Copyright 2018 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package terminal

import (
	"io"
	"os"
	"regexp"
	"strings"

	"android/soong/ui/status"
)

var sisoMemoryStallNotice = regexp.MustCompile(`^E[0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]+[[:space:]]+[0-9]+ trace_sys_linux\.go:[0-9]+\] memory stall [0-9]+(\.[0-9]+)?ms/s\r?$`)
var sisoExperimentsNoticeLine = regexp.MustCompile(`^SISO_EXPERIMENTS=[[:alnum:]_.,:-]+ enabled$`)

func isSisoMemoryStallNotice(level status.MsgLevel, message string) bool {
	return level == status.PrintLvl && sisoMemoryStallNotice.MatchString(message)
}

func isConsoleStartupNotice(level status.MsgLevel, message string) bool {
	if level != status.StatusLvl {
		return false
	}
	message = strings.TrimSpace(message)
	return message == "ninja:" ||
		message == "ninja: ignore missing out error in depfile" ||
		message == "No need to regenerate ninja file" ||
		isSisoExperimentsNotice(level, message)
}

func isSisoExperimentsNotice(level status.MsgLevel, message string) bool {
	if level != status.StatusLvl {
		return false
	}
	message = strings.TrimSpace(string(stripAnsiEscapes([]byte(message))))
	lines := strings.Split(message, "\n")
	if len(lines) == 0 {
		return false
	}
	lines[0] = strings.TrimSpace(lines[0])
	lines[0] = strings.TrimPrefix(lines[0], "ninja: ")
	for _, line := range lines {
		if !sisoExperimentsNoticeLine.MatchString(strings.TrimSpace(line)) {
			return false
		}
	}
	return true
}

func isConsoleNinjaSuccessSummary(level status.MsgLevel, message string) bool {
	if level != status.StatusLvl && level != status.PrintLvl {
		return false
	}
	message = strings.TrimSpace(string(stripAnsiEscapes([]byte(message))))
	return status.IsSisoSuccessSummary(message)
}

// NewStatusOutput returns a StatusOutput that represents the
// current build status similarly to Ninja's built-in terminal
// output.
//
// statusFormat takes nearly all the same options as NINJA_STATUS.
// %c is currently unsupported.
func NewStatusOutput(w io.Writer, statusFormat string, forceSimpleOutput, quietBuild,
	forceKeepANSI bool, skipActionProgress bool, suppressOutput bool) status.StatusOutput {
	return newStatusOutput(w, statusFormat, forceSimpleOutput, quietBuild, forceKeepANSI,
		skipActionProgress, suppressOutput, true)
}

// NewStatusOutputWithoutActionTable creates a status output that keeps smart
// live status lines but does not reserve rows for a running-action table.
func NewStatusOutputWithoutActionTable(w io.Writer, statusFormat string, forceSimpleOutput, quietBuild,
	forceKeepANSI bool, skipActionProgress bool, suppressOutput bool) status.StatusOutput {
	return newStatusOutput(w, statusFormat, forceSimpleOutput, quietBuild, forceKeepANSI,
		skipActionProgress, suppressOutput, false)
}

func newStatusOutput(w io.Writer, statusFormat string, forceSimpleOutput, quietBuild,
	forceKeepANSI bool, skipActionProgress bool, suppressOutput, useActionTable bool) status.StatusOutput {

	canUseSmartFormatting := !forceSimpleOutput && isSmartTerminal(w)
	formatter := newFormatter(canUseSmartFormatting, statusFormat, quietBuild)
	colorMode := ColorMode(w, forceSimpleOutput, forceKeepANSI)
	formatter.colorize = colorMode != "none" && (canUseSmartFormatting || forceKeepANSI)
	formatter.phaseColorize = formatter.colorize
	formatter.trueColor = formatter.colorize && colorMode == "truecolor"
	formatter.diagnosticColorize = colorMode != "none"
	formatter.diagnosticTrueColor = colorMode == "truecolor"

	if canUseSmartFormatting {
		return newSmartStatusOutput(w, formatter, suppressOutput, useActionTable)
	} else {
		return NewSimpleStatusOutput(w, formatter, forceKeepANSI, skipActionProgress,
			suppressOutput)
	}
}

// ColorMode reports the color policy for build actions whose output is captured
// before it is replayed through the terminal status writer. The returned mode
// is deliberately a single, explicit value for that output pipeline.
func ColorMode(w io.Writer, forceSimpleOutput, forceANSI bool) string {
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return "none"
	}
	if term, ok := os.LookupEnv("TERM"); ok && term == "dumb" {
		return "none"
	}
	if !forceANSI && (forceSimpleOutput || !isSmartTerminal(w)) {
		return "none"
	}
	if colorTerm, ok := os.LookupEnv("COLORTERM"); ok && (colorTerm == "truecolor" || colorTerm == "24bit") {
		return "truecolor"
	}
	return "ansi"
}

// CleanStatusColorFor returns the LOSP teal styling for an interactive
// terminal, using the same color policy as the status output.
func CleanStatusColorFor(w io.Writer, forceSimpleOutput, forceANSI bool) string {
	// Installclean output remains plain when redirected, even when ANSI output
	// is explicitly enabled for captured action diagnostics.
	if !isSmartTerminal(w) {
		return ""
	}
	mode := ColorMode(w, forceSimpleOutput, forceANSI)
	if mode == "none" {
		return ""
	}
	return ansi.teal(mode == "truecolor")
}
