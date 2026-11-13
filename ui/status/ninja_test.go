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

package status

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"android/soong/ui/logger"
	"android/soong/ui/status/ninja_frontend"
)

// Tests that closing the ninja reader when nothing has opened the other end of the fifo is fast.
func TestNinjaReader_Close(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "ninja_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	stat := &Status{}
	sigNumFunc := func() os.Signal {
		var sig os.Signal
		return sig
	}
	nr := NewNinjaReader(logger.New(ioutil.Discard), stat.StartTool(), filepath.Join(tempDir, "fifo"), sigNumFunc)

	start := time.Now()

	nr.Close()

	if g, w := time.Since(start), NINJA_READER_CLOSE_TIMEOUT; g >= w {
		t.Errorf("nr.Close timed out, %s > %s", g, w)
	}
}

type ninjaMessageCapture struct {
	messages []capturedNinjaMessage
}

type capturedNinjaMessage struct {
	level MsgLevel
	text  string
}

func (*ninjaMessageCapture) SetTotalActions(int)        {}
func (*ninjaMessageCapture) SetEstimatedTime(time.Time) {}
func (*ninjaMessageCapture) StartAction(*Action)        {}
func (*ninjaMessageCapture) FinishAction(ActionResult)  {}
func (c *ninjaMessageCapture) Verbose(text string)      { c.add(VerboseLvl, text) }
func (c *ninjaMessageCapture) Status(text string)       { c.add(StatusLvl, text) }
func (c *ninjaMessageCapture) Print(text string)        { c.add(PrintLvl, text) }
func (c *ninjaMessageCapture) Error(text string)        { c.add(ErrorLvl, text) }
func (c *ninjaMessageCapture) add(level MsgLevel, text string) {
	c.messages = append(c.messages, capturedNinjaMessage{level, text})
}
func (*ninjaMessageCapture) Finish() {}

func TestNinjaReaderSummaryAndBareMarkerRouting(t *testing.T) {
	for _, test := range []struct {
		name  string
		steps string
	}{
		{name: "zero steps", steps: "0"},
		{name: "one step", steps: "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			fifo := filepath.Join(dir, "fifo")
			capture := &ninjaMessageCapture{}
			sigNumFunc := func() os.Signal { return nil }
			nr := NewNinjaReader(logger.New(ioutil.Discard), capture, fifo, sigNumFunc)
			f, err := os.OpenFile(fifo, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			write := func(level ninja_frontend.Status_Message_Level, message string) {
				t.Helper()
				payload, err := proto.Marshal(&ninja_frontend.Status{Message: &ninja_frontend.Status_Message{
					Level: &level, Message: &message,
				}})
				if err != nil {
					t.Fatal(err)
				}
				frame := protowire.AppendVarint(nil, uint64(len(payload)))
				frame = append(frame, payload...)
				if _, err := f.Write(frame); err != nil {
					t.Fatal(err)
				}
			}
			write(ninja_frontend.Status_Message_INFO, " 4m51.01s Build Succeeded: "+test.steps+" steps - 0.00/s")
			write(ninja_frontend.Status_Message_INFO, "Build Succeeded")
			write(ninja_frontend.Status_Message_INFO, "module status mentions Build Succeeded as text")
			write(ninja_frontend.Status_Message_WARNING, "preserve warning")
			write(ninja_frontend.Status_Message_ERROR, "preserve failure")
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			nr.Close()

			want := []capturedNinjaMessage{
				{StatusLvl, "ninja:  4m51.01s Build Succeeded: " + test.steps + " steps - 0.00/s"},
				{VerboseLvl, "ninja: Build Succeeded"},
				{StatusLvl, "ninja: module status mentions Build Succeeded as text"},
				{PrintLvl, "warning: ninja: preserve warning"},
				{ErrorLvl, "ninja: preserve failure"},
			}
			if !reflect.DeepEqual(capture.messages, want) {
				t.Fatalf("messages = %#v, want %#v", capture.messages, want)
			}
		})
	}
}

// Test that error hint is added to output if available
func TestNinjaReader_CorrectErrorHint(t *testing.T) {
	errorPattern1 := "pattern-1 in input"
	errorHint1 := "\n Fix by doing task 1"
	errorPattern2 := "pattern-2 in input"
	errorHint2 := "\n Fix by doing task 2"
	mockErrorHints := make(map[string]string)
	mockErrorHints[errorPattern1] = errorHint1
	mockErrorHints[errorPattern2] = errorHint2

	errorHintGenerator := *newErrorHintGenerator(mockErrorHints)
	testCases := []struct {
		rawOutput            string
		buildExitCode        int
		expectedFinalOutput  string
		testCaseErrorMessage string
	}{
		{
			rawOutput:            "ninja build was successful",
			buildExitCode:        0,
			expectedFinalOutput:  "ninja build was successful",
			testCaseErrorMessage: "raw output changed when build was successful",
		},
		{
			rawOutput:            "ninja build failed",
			buildExitCode:        1,
			expectedFinalOutput:  "ninja build failed",
			testCaseErrorMessage: "raw output changed even when no error hint pattern was found",
		},
		{
			rawOutput:            "ninja build failed: " + errorPattern1 + "some footnotes",
			buildExitCode:        1,
			expectedFinalOutput:  "ninja build failed: " + errorPattern1 + "some footnotes" + errorHint1,
			testCaseErrorMessage: "error hint not added despite pattern match",
		},
		{
			rawOutput:            "ninja build failed: " + errorPattern2 + errorPattern1,
			buildExitCode:        1,
			expectedFinalOutput:  "ninja build failed: " + errorPattern2 + errorPattern1 + errorHint2,
			testCaseErrorMessage: "error hint should be added for first pattern match in raw output",
		},
	}
	for _, testCase := range testCases {
		actualFinalOutput := errorHintGenerator.GetOutputWithErrorHint(testCase.rawOutput, testCase.buildExitCode)
		if actualFinalOutput != testCase.expectedFinalOutput {
			t.Errorf(testCase.testCaseErrorMessage+"\nexpected: %s\ngot: %s", testCase.expectedFinalOutput, actualFinalOutput)
		}
	}
}
