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

	"android/soong/ui/status"
)

var sisoMemoryStallNotice = regexp.MustCompile(`^E[0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]+[[:space:]]+[0-9]+ trace_sys_linux\.go:[0-9]+\] memory stall [0-9]+(\.[0-9]+)?ms/s\r?$`)

func isSisoMemoryStallNotice(level status.MsgLevel, message string) bool {
	return level == status.PrintLvl && sisoMemoryStallNotice.MatchString(message)
}

// NewStatusOutput returns a StatusOutput that represents the
// current build status similarly to Ninja's built-in terminal
// output.
//
// statusFormat takes nearly all the same options as NINJA_STATUS.
// %c is currently unsupported.
func NewStatusOutput(w io.Writer, statusFormat string, forceSimpleOutput, quietBuild,
	forceKeepANSI bool, skipActionProgress bool, suppressOutput bool) status.StatusOutput {

	canUseSmartFormatting := !forceSimpleOutput && isSmartTerminal(w)
	formatter := newFormatter(canUseSmartFormatting, statusFormat, quietBuild)
	colorMode := ColorMode(w, forceSimpleOutput, forceKeepANSI)
	formatter.colorize = colorMode != "none" && (canUseSmartFormatting || forceKeepANSI)
	formatter.phaseColorize = formatter.colorize
	formatter.trueColor = formatter.colorize && colorMode == "truecolor"
	formatter.diagnosticColorize = colorMode != "none"
	formatter.diagnosticTrueColor = colorMode == "truecolor"

	if canUseSmartFormatting {
		return NewSmartStatusOutput(w, formatter, suppressOutput)
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
