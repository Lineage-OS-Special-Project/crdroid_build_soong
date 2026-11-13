// Copyright 2019 Google Inc. All rights reserved.
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
	"android/soong/ui/status"
	"fmt"
	"io"
)

type simpleStatusOutput struct {
	writer             io.Writer
	formatter          formatter
	keepANSI           bool
	skipActionProgress bool
	suppressOutput     bool
}

// NewSimpleStatusOutput returns a StatusOutput that represents the
// current build status similarly to Ninja's built-in terminal
// output.
func NewSimpleStatusOutput(w io.Writer, formatter formatter, keepANSI bool,
	skipActionProgress bool, suppressOutput bool) status.StatusOutput {
	return &simpleStatusOutput{
		writer:             w,
		formatter:          formatter,
		keepANSI:           keepANSI,
		skipActionProgress: skipActionProgress,
		suppressOutput:     suppressOutput,
	}
}

func (s *simpleStatusOutput) Message(level status.MsgLevel, message string) {
	if level >= status.StatusLvl {
		if isConsoleStartupNotice(level, message) || isConsoleNinjaSuccessSummary(level, message) || isSisoMemoryStallNotice(level, message) {
			return
		}
		output := s.formatter.message(level, message)
		if !s.keepANSI {
			output = string(stripAnsiEscapes([]byte(output)))
		}
		fmt.Fprintln(s.writer, output)
	}
}

func (s *simpleStatusOutput) PhaseStatus(phase status.BuildPhase, description string) {
	s.Message(status.StatusLvl, "["+string(phase)+"] "+description)
}

func (s *simpleStatusOutput) StartAction(action *status.Action, counts status.Counts) {
}

func (s *simpleStatusOutput) FinishAction(result status.ActionResult, counts status.Counts) {
	str := result.Description
	if str == "" {
		str = result.Command
	}

	progress := ""
	if !s.skipActionProgress {
		progress = s.formatter.progress(counts)
	}

	output := s.formatter.result(result)
	if !s.keepANSI {
		output = string(stripAnsiEscapes([]byte(output)))
	}

	if output != "" && (!s.suppressOutput || result.Error != nil) {
		fmt.Fprint(s.writer, progress+str, "\n", output)
	} else if !s.skipActionProgress {
		fmt.Fprintln(s.writer, progress+str)
	}
}

func (s *simpleStatusOutput) Flush() {}

func (s *simpleStatusOutput) Write(p []byte) (int, error) {
	fmt.Fprint(s.writer, string(p))
	return len(p), nil
}
