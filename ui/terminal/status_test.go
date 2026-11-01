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
	"bytes"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"android/soong/ui/status"
)

func TestPhaseStatusColorPolicy(t *testing.T) {
	const description = "Blueprint bootstrap and graph analysis"
	unsetTestEnv(t, "NO_COLOR")

	t.Run("smart terminal uses cyan marker only", func(t *testing.T) {
		unsetTestEnv(t, "NO_COLOR")
		t.Setenv("TERM", "xterm-256color")
		smart := &fakeSmartTerminal{termWidth: 100}
		statusOutput := NewStatusOutput(smart, "", false, false, false, false, false)
		statusOutput.(*smartStatusOutput).PhaseStatus(status.SoongPhase, description)
		statusOutput.Flush()

		got := smart.String()
		want := "\x1b[36m[Soong]\x1b[0m\x1b[1m " + description
		if !strings.Contains(got, want) {
			t.Fatalf("smart status does not contain colored phase marker %q: %q", want, got)
		}
		if strings.Count(got, "\x1b[36m") != 1 {
			t.Fatalf("expected only one cyan sequence around marker, got %q", got)
		}
		if plain := string(stripAnsiEscapes([]byte(got))); !strings.Contains(plain, "[Soong] "+description) {
			t.Fatalf("plain rendering = %q", plain)
		}
	})

	t.Run("NO_COLOR disables phase color", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		t.Setenv("TERM", "xterm-256color")
		smart := &fakeSmartTerminal{termWidth: 100}
		statusOutput := NewStatusOutput(smart, "", false, false, false, false, false)
		statusOutput.(*smartStatusOutput).PhaseStatus(status.KatiPhase, "Makefile and packaging generation")
		statusOutput.Flush()

		got := smart.String()
		if strings.Contains(got, "\x1b[36m") {
			t.Fatalf("NO_COLOR output has cyan color sequence: %q", got)
		}
		if plain := string(stripAnsiEscapes([]byte(got))); !strings.Contains(plain, "[Kati] Makefile and packaging generation") {
			t.Fatalf("plain rendering = %q", plain)
		}
	})

	t.Run("redirected output is plain", func(t *testing.T) {
		unsetTestEnv(t, "NO_COLOR")
		var redirected bytes.Buffer
		statusOutput := NewStatusOutput(&redirected, "", false, false, false, false, false)
		statusOutput.(*simpleStatusOutput).PhaseStatus(status.NinjaPhase, "Starting Ninja/Siso execution")
		statusOutput.Flush()

		if got, want := redirected.String(), "[Build] Starting Ninja/Siso execution\n"; got != want {
			t.Fatalf("redirected output = %q, want %q", got, want)
		}
	})
}

func TestLOSPColorPolicy(t *testing.T) {
	tests := []struct {
		name, colorTerm, noColor, term string
		color, failure                 string
	}{
		{"truecolor", "truecolor", "", "xterm", "38;2;16;200;200m", "38;2;201;87;87m"},
		{"24bit", "24bit", "", "xterm", "38;2;16;200;200m", "38;2;201;87;87m"},
		{"ansi", "", "", "xterm", "36m", "31m"},
		{"no color", "truecolor", "1", "xterm", "", ""},
		{"dumb", "truecolor", "", "dumb", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetTestEnv(t, "NO_COLOR")
			unsetTestEnv(t, "COLORTERM")
			if tt.noColor != "" {
				t.Setenv("NO_COLOR", tt.noColor)
			}
			if tt.colorTerm != "" {
				t.Setenv("COLORTERM", tt.colorTerm)
			}
			t.Setenv("TERM", tt.term)
			f := newFormatter(true, "", false)
			_, noColor := os.LookupEnv("NO_COLOR")
			f.colorize = tt.term != "dumb" && !noColor
			f.phaseColorize = f.colorize
			f.trueColor = f.colorize && (tt.colorTerm == "truecolor" || tt.colorTerm == "24bit")
			phase := f.phaseStatus(status.SoongPhase, "Blueprint bootstrap")
			if tt.color != "" && !strings.Contains(phase, tt.color) || !strings.Contains(string(stripAnsiEscapes([]byte(phase))), "[Soong] Blueprint bootstrap") {
				t.Fatalf("phase = %q", phase)
			}
			failed := f.failedString()
			if (tt.failure != "" && !strings.Contains(failed, tt.failure)) || !strings.Contains(failed, "FAILED:") {
				t.Fatalf("failure prefix = %q", failed)
			}
			body := "\x1b[35mcompiler diagnostic\x1b[0m"
			if got := f.message(status.ErrorLvl, body); !strings.HasSuffix(got, body) {
				t.Fatalf("diagnostic body changed: %q", got)
			}
		})
	}

	var redirected bytes.Buffer
	stat := NewStatusOutput(&redirected, "", false, false, false, false, false)
	stat.(interface {
		PhaseStatus(status.BuildPhase, string)
	}).PhaseStatus(status.SoongPhase, "Blueprint bootstrap")
	stat.Message(status.ErrorLvl, "diagnostic")
	stat.Flush()
	if strings.Contains(redirected.String(), "\x1b[") {
		t.Fatalf("redirected output contains ANSI: %q", redirected.String())
	}
}

func TestLOSPProgressFormat(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	f := newFormatter(true, lospStatusFormat, false)
	f.colorize, f.trueColor = true, true
	f.start = time.Now().Add(-time.Second)
	got := f.progress(status.Counts{FinishedActions: 3, TotalActions: 10})
	for _, sequence := range []string{"38;2;69;169;120m", "38;2;16;200;200m", "38;2;79;134;168m", "38;2;201;150;50m"} {
		if !strings.Contains(got, sequence) {
			t.Errorf("progress missing %q: %q", sequence, got)
		}
	}
	if strings.Contains(got, `\033`) || !strings.Contains(got, "%") {
		t.Fatalf("invalid progress rendering: %q", got)
	}
	plain := newFormatter(false, lospStatusFormat, false).progress(status.Counts{FinishedActions: 3, TotalActions: 10})
	if strings.Contains(plain, "\x1b") {
		t.Fatalf("plain progress contains ANSI: %q", plain)
	}
}

func unsetTestEnv(t *testing.T, key string) {
	t.Helper()
	value, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestStatusOutput(t *testing.T) {
	unsetTestEnv(t, "NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	tests := []struct {
		name   string
		calls  func(stat status.StatusOutput)
		smart  string
		simple string
	}{
		{
			name:   "two actions",
			calls:  twoActions,
			smart:  "\r\x1b[1m[  0% 0/2] action1\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action1\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action2\x1b[0m\x1b[K\r\x1b[1m[100% 2/2] action2\x1b[0m\x1b[K\n",
			simple: "[ 50% 1/2] action1\n[100% 2/2] action2\n",
		},
		{
			name:   "two parallel actions",
			calls:  twoParallelActions,
			smart:  "\r\x1b[1m[  0% 0/2] action1\x1b[0m\x1b[K\r\x1b[1m[  0% 0/2] action2\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action1\x1b[0m\x1b[K\r\x1b[1m[100% 2/2] action2\x1b[0m\x1b[K\n",
			simple: "[ 50% 1/2] action1\n[100% 2/2] action2\n",
		},
		{
			name:   "action with output",
			calls:  actionsWithOutput,
			smart:  "\r\x1b[1m[  0% 0/3] action1\x1b[0m\x1b[K\r\x1b[1m[ 33% 1/3] action1\x1b[0m\x1b[K\r\x1b[1m[ 33% 1/3] action2\x1b[0m\x1b[K\r\x1b[1m[ 66% 2/3] action2\x1b[0m\x1b[K\noutput1\noutput2\n\r\x1b[1m[ 66% 2/3] action3\x1b[0m\x1b[K\r\x1b[1m[100% 3/3] action3\x1b[0m\x1b[K\n",
			simple: "[ 33% 1/3] action1\n[ 66% 2/3] action2\noutput1\noutput2\n[100% 3/3] action3\n",
		},
		{
			name:   "action with output without newline",
			calls:  actionsWithOutputWithoutNewline,
			smart:  "\r\x1b[1m[  0% 0/3] action1\x1b[0m\x1b[K\r\x1b[1m[ 33% 1/3] action1\x1b[0m\x1b[K\r\x1b[1m[ 33% 1/3] action2\x1b[0m\x1b[K\r\x1b[1m[ 66% 2/3] action2\x1b[0m\x1b[K\noutput1\noutput2\n\r\x1b[1m[ 66% 2/3] action3\x1b[0m\x1b[K\r\x1b[1m[100% 3/3] action3\x1b[0m\x1b[K\n",
			simple: "[ 33% 1/3] action1\n[ 66% 2/3] action2\noutput1\noutput2\n[100% 3/3] action3\n",
		},
		{
			name:   "action with error",
			calls:  actionsWithError,
			smart:  "\r\x1b[1m[  0% 0/3] action1\x1b[0m\x1b[K\r\x1b[1m[ 33% 1/3] action1\x1b[0m\x1b[K\r\x1b[1m[ 33% 1/3] action2\x1b[0m\x1b[K\r\x1b[1m[ 66% 2/3] action2\x1b[0m\x1b[K\n\x1b[31m\x1b[1mFAILED:\x1b[0m f1 f2\ntouch f1 f2\nerror1\nerror2\n\r\x1b[1m[ 66% 2/3] action3\x1b[0m\x1b[K\r\x1b[1m[100% 3/3] action3\x1b[0m\x1b[K\n",
			simple: "[ 33% 1/3] action1\n[ 66% 2/3] action2\nFAILED: f1 f2\ntouch f1 f2\nerror1\nerror2\n[100% 3/3] action3\n",
		},
		{
			name:   "action with empty description",
			calls:  actionWithEmptyDescription,
			smart:  "\r\x1b[1m[  0% 0/1] command1\x1b[0m\x1b[K\r\x1b[1m[100% 1/1] command1\x1b[0m\x1b[K\n",
			simple: "[100% 1/1] command1\n",
		},
		{
			name:   "messages",
			calls:  actionsWithMessages,
			smart:  "\r\x1b[1m[  0% 0/2] action1\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action1\x1b[0m\x1b[K\r\x1b[1mstatus\x1b[0m\x1b[K\r\x1b[Kprint\n\x1b[31m\x1b[1mFAILED:\x1b[0m error\n\r\x1b[1m[ 50% 1/2] action2\x1b[0m\x1b[K\r\x1b[1m[100% 2/2] action2\x1b[0m\x1b[K\n",
			simple: "[ 50% 1/2] action1\nstatus\nprint\nFAILED: error\n[100% 2/2] action2\n",
		},
		{
			name:   "action with long description",
			calls:  actionWithLongDescription,
			smart:  "\r\x1b[1m[  0% 0/2] action with very long descrip\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action with very long descrip\x1b[0m\x1b[K\n",
			simple: "[ 50% 1/2] action with very long description to test eliding\n",
		},
		{
			name:   "action with output with ansi codes",
			calls:  actionWithOutputWithAnsiCodes,
			smart:  "\r\x1b[1m[  0% 0/1] action1\x1b[0m\x1b[K\r\x1b[1m[100% 1/1] action1\x1b[0m\x1b[K\n\x1b[31mcolor\x1b[0m\n\x1b[31mcolor message\x1b[0m\n",
			simple: "[100% 1/1] action1\ncolor\ncolor message\n",
		},
	}

	os.Setenv(tableHeightEnVar, "")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			t.Run("smart", func(t *testing.T) {
				smart := &fakeSmartTerminal{termWidth: 40}
				stat := NewStatusOutput(smart, "", false, false, false, false, false)
				tt.calls(stat)
				stat.Flush()

				if g, w := smart.String(), tt.smart; g != w {
					t.Errorf("want:\n%q\ngot:\n%q", w, g)
				}
			})

			t.Run("simple", func(t *testing.T) {
				simple := &bytes.Buffer{}
				stat := NewStatusOutput(simple, "", false, false, false, false, false)
				tt.calls(stat)
				stat.Flush()

				if g, w := simple.String(), tt.simple; g != w {
					t.Errorf("want:\n%q\ngot:\n%q", w, g)
				}
			})

			t.Run("force simple", func(t *testing.T) {
				smart := &fakeSmartTerminal{termWidth: 40}
				stat := NewStatusOutput(smart, "", true, false, false, false, false)
				tt.calls(stat)
				stat.Flush()

				if g, w := smart.String(), tt.simple; g != w {
					t.Errorf("want:\n%q\ngot:\n%q", w, g)
				}
			})
		})
	}
}

type runner struct {
	counts status.Counts
	stat   status.StatusOutput
}

func newRunner(stat status.StatusOutput, totalActions int) *runner {
	return &runner{
		counts: status.Counts{TotalActions: totalActions},
		stat:   stat,
	}
}

func (r *runner) startAction(action *status.Action) {
	r.counts.StartedActions++
	r.counts.RunningActions++
	r.stat.StartAction(action, r.counts)
}

func (r *runner) finishAction(result status.ActionResult) {
	r.counts.FinishedActions++
	r.counts.RunningActions--
	r.stat.FinishAction(result, r.counts)
}

func (r *runner) finishAndStartAction(result status.ActionResult, action *status.Action) {
	r.counts.FinishedActions++
	r.stat.FinishAction(result, r.counts)

	r.counts.StartedActions++
	r.stat.StartAction(action, r.counts)
}

var (
	action1 = &status.Action{Description: "action1"}
	result1 = status.ActionResult{Action: action1}
	action2 = &status.Action{Description: "action2"}
	result2 = status.ActionResult{Action: action2}
	action3 = &status.Action{Description: "action3"}
	result3 = status.ActionResult{Action: action3}
)

func twoActions(stat status.StatusOutput) {
	runner := newRunner(stat, 2)
	runner.startAction(action1)
	runner.finishAction(result1)
	runner.startAction(action2)
	runner.finishAction(result2)
}

func twoParallelActions(stat status.StatusOutput) {
	runner := newRunner(stat, 2)
	runner.startAction(action1)
	runner.startAction(action2)
	runner.finishAction(result1)
	runner.finishAction(result2)
}

func actionsWithOutput(stat status.StatusOutput) {
	result2WithOutput := status.ActionResult{Action: action2, Output: "output1\noutput2\n"}

	runner := newRunner(stat, 3)
	runner.startAction(action1)
	runner.finishAction(result1)
	runner.startAction(action2)
	runner.finishAction(result2WithOutput)
	runner.startAction(action3)
	runner.finishAction(result3)
}

func actionsWithOutputWithoutNewline(stat status.StatusOutput) {
	result2WithOutputWithoutNewline := status.ActionResult{Action: action2, Output: "output1\noutput2"}

	runner := newRunner(stat, 3)
	runner.startAction(action1)
	runner.finishAction(result1)
	runner.startAction(action2)
	runner.finishAction(result2WithOutputWithoutNewline)
	runner.startAction(action3)
	runner.finishAction(result3)
}

func actionsWithError(stat status.StatusOutput) {
	action2WithError := &status.Action{Description: "action2", Outputs: []string{"f1", "f2"}, Command: "touch f1 f2"}
	result2WithError := status.ActionResult{Action: action2WithError, Output: "error1\nerror2\n", Error: fmt.Errorf("error1")}

	runner := newRunner(stat, 3)
	runner.startAction(action1)
	runner.finishAction(result1)
	runner.startAction(action2WithError)
	runner.finishAction(result2WithError)
	runner.startAction(action3)
	runner.finishAction(result3)
}

func actionWithEmptyDescription(stat status.StatusOutput) {
	action1 := &status.Action{Command: "command1"}
	result1 := status.ActionResult{Action: action1}

	runner := newRunner(stat, 1)
	runner.startAction(action1)
	runner.finishAction(result1)
}

func actionsWithMessages(stat status.StatusOutput) {
	runner := newRunner(stat, 2)

	runner.startAction(action1)
	runner.finishAction(result1)

	stat.Message(status.VerboseLvl, "verbose")
	stat.Message(status.StatusLvl, "status")
	stat.Message(status.PrintLvl, "print")
	stat.Message(status.ErrorLvl, "error")

	runner.startAction(action2)
	runner.finishAction(result2)
}

func actionWithLongDescription(stat status.StatusOutput) {
	action1 := &status.Action{Description: "action with very long description to test eliding"}
	result1 := status.ActionResult{Action: action1}

	runner := newRunner(stat, 2)

	runner.startAction(action1)

	runner.finishAction(result1)
}

func actionWithOutputWithAnsiCodes(stat status.StatusOutput) {
	result1WithOutputWithAnsiCodes := status.ActionResult{Action: action1, Output: "\x1b[31mcolor\x1b[0m"}

	runner := newRunner(stat, 1)
	runner.startAction(action1)
	runner.finishAction(result1WithOutputWithAnsiCodes)

	stat.Message(status.PrintLvl, "\x1b[31mcolor message\x1b[0m")
}

func TestSuppressOutput(t *testing.T) {
	simple := &bytes.Buffer{}
	formatter := newFormatter(false, "", false)
	stat := NewSimpleStatusOutput(simple, formatter, false, false, true)

	runner := newRunner(stat, 2)

	result1WithOutput := status.ActionResult{
		Action: action1,
		Output: "Output1",
	}
	result2WithError := status.ActionResult{
		Action: action2,
		Output: "Output2",
		Error:  fmt.Errorf("Error2"),
	}

	runner.startAction(action1)
	runner.finishAction(result1WithOutput)
	runner.startAction(action2)
	runner.finishAction(result2WithError)

	w := "[ 50% 1/2] action1\n[100% 2/2] action2\nFAILED: \nOutput2\n"
	if g := simple.String(); g != w {
		t.Errorf("want:\n%q\ngot:\n%q", w, g)
	}
}

func TestSkipActionProgress(t *testing.T) {
	simple := &bytes.Buffer{}
	formatter := newFormatter(false, "", false)
	stat := NewSimpleStatusOutput(simple, formatter, false, true, false)

	runner := newRunner(stat, 2)
	runner.startAction(action1)
	runner.finishAction(result1)
	runner.startAction(action2)
	runner.finishAction(status.ActionResult{Action: action2, Output: "output2"})
	stat.Flush()

	// action1 finished without output -> skipped
	// action2 finished with output -> printed progress + output
	w := "action2\noutput2\n"
	if g := simple.String(); g != w {
		t.Errorf("want:\n%q\ngot:\n%q", w, g)
	}
}

func TestSuppressOutputAndSkipActionProgress(t *testing.T) {
	simple := &bytes.Buffer{}
	formatter := newFormatter(false, "", false)
	stat := NewSimpleStatusOutput(simple, formatter, false, true, true)

	runner := newRunner(stat, 3)

	result3WithError := status.ActionResult{
		Action: action3,
		Output: "Output3",
		Error:  fmt.Errorf("Error3"),
	}

	runner.startAction(action1)
	runner.finishAction(result1)
	runner.startAction(action2)
	runner.finishAction(status.ActionResult{Action: action2, Output: "output2"})
	runner.startAction(action3)
	runner.finishAction(result3WithError)
	stat.Flush()

	// action1 finished without output -> skipped
	// action2 finished with output -> printed progress + output
	w := "action3\nFAILED: \nOutput3\n"
	if g := simple.String(); g != w {
		t.Errorf("want:\n%q\ngot:\n%q", w, g)
	}
}

func TestSmartStatusOutputWidthChange(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	os.Setenv(tableHeightEnVar, "")

	smart := &fakeSmartTerminal{termWidth: 40}
	stat := NewStatusOutput(smart, "", false, false, false, false, false)
	smartStat := stat.(*smartStatusOutput)
	smartStat.sigwinchHandled = make(chan bool)

	runner := newRunner(stat, 2)

	action := &status.Action{Description: "action with very long description to test eliding"}
	result := status.ActionResult{Action: action}

	runner.startAction(action)
	smart.termWidth = 30
	// Fake a SIGWINCH
	smartStat.sigwinch <- syscall.SIGWINCH
	<-smartStat.sigwinchHandled
	runner.finishAction(result)

	stat.Flush()

	w := "\r\x1b[1m[  0% 0/2] action with very long descrip\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action with very lo\x1b[0m\x1b[K\n"

	if g := smart.String(); g != w {
		t.Errorf("want:\n%q\ngot:\n%q", w, g)
	}
}

func TestSmartStatusDoesntHideAfterSucecss(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	os.Setenv(tableHeightEnVar, "")

	smart := &fakeSmartTerminal{termWidth: 40}
	stat := NewStatusOutput(smart, "", false, false, false, false, false)
	smartStat := stat.(*smartStatusOutput)
	smartStat.sigwinchHandled = make(chan bool)

	runner := newRunner(stat, 2)

	action1 := &status.Action{Description: "action1"}
	result1 := status.ActionResult{
		Action: action1,
		Output: "Output1",
	}

	action2 := &status.Action{Description: "action2"}
	result2 := status.ActionResult{
		Action: action2,
		Output: "Output2",
	}

	runner.startAction(action1)
	runner.startAction(action2)
	runner.finishAction(result1)
	runner.finishAction(result2)

	stat.Flush()

	w := "\r\x1b[1m[  0% 0/2] action1\x1b[0m\x1b[K\r\x1b[1m[  0% 0/2] action2\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action1\x1b[0m\x1b[K\nOutput1\n\r\x1b[1m[100% 2/2] action2\x1b[0m\x1b[K\nOutput2\n"

	if g := smart.String(); g != w {
		t.Errorf("want:\n%q\ngot:\n%q", w, g)
	}
}

func TestSmartStatusHideAfterFailure(t *testing.T) {
	unsetTestEnv(t, "NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	os.Setenv(tableHeightEnVar, "")

	smart := &fakeSmartTerminal{termWidth: 40}
	stat := NewStatusOutput(smart, "", false, false, false, false, false)
	smartStat := stat.(*smartStatusOutput)
	smartStat.sigwinchHandled = make(chan bool)

	runner := newRunner(stat, 2)

	action1 := &status.Action{Description: "action1"}
	result1 := status.ActionResult{
		Action: action1,
		Output: "Output1",
		Error:  fmt.Errorf("Error1"),
	}

	action2 := &status.Action{Description: "action2"}
	result2 := status.ActionResult{
		Action: action2,
		Output: "Output2",
	}

	runner.startAction(action1)
	runner.startAction(action2)
	runner.finishAction(result1)
	runner.finishAction(result2)

	stat.Flush()

	w := "\r\x1b[1m[  0% 0/2] action1\x1b[0m\x1b[K\r\x1b[1m[  0% 0/2] action2\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action1\x1b[0m\x1b[K\n\x1b[31m\x1b[1mFAILED:\x1b[0m \nOutput1\n\r\x1b[1m[100% 2/2] action2\x1b[0m\x1b[K\nThere was 1 action that completed after the action that failed. See verbose.log.gz for its output.\n"

	if g := smart.String(); g != w {
		t.Errorf("want:\n%q\ngot:\n%q", w, g)
	}
}

func TestSmartStatusHideAfterFailurePlural(t *testing.T) {
	unsetTestEnv(t, "NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	os.Setenv(tableHeightEnVar, "")

	smart := &fakeSmartTerminal{termWidth: 40}
	stat := NewStatusOutput(smart, "", false, false, false, false, false)
	smartStat := stat.(*smartStatusOutput)
	smartStat.sigwinchHandled = make(chan bool)

	runner := newRunner(stat, 2)

	action1 := &status.Action{Description: "action1"}
	result1 := status.ActionResult{
		Action: action1,
		Output: "Output1",
		Error:  fmt.Errorf("Error1"),
	}

	action2 := &status.Action{Description: "action2"}
	result2 := status.ActionResult{
		Action: action2,
		Output: "Output2",
	}

	action3 := &status.Action{Description: "action3"}
	result3 := status.ActionResult{
		Action: action3,
		Output: "Output3",
	}

	runner.startAction(action1)
	runner.startAction(action2)
	runner.startAction(action3)
	runner.finishAction(result1)
	runner.finishAction(result2)
	runner.finishAction(result3)

	stat.Flush()

	w := "\r\x1b[1m[  0% 0/2] action1\x1b[0m\x1b[K\r\x1b[1m[  0% 0/2] action2\x1b[0m\x1b[K\r\x1b[1m[  0% 0/2] action3\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action1\x1b[0m\x1b[K\n\x1b[31m\x1b[1mFAILED:\x1b[0m \nOutput1\n\r\x1b[1m[100% 2/2] action2\x1b[0m\x1b[K\r\x1b[1m[150% 3/2] action3\x1b[0m\x1b[K\nThere were 2 actions that completed after the action that failed. See verbose.log.gz for their output.\n"

	if g := smart.String(); g != w {
		t.Errorf("want:\n%q\ngot:\n%q", w, g)
	}
}

func TestSmartStatusDontHideErrorAfterFailure(t *testing.T) {
	unsetTestEnv(t, "NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	os.Setenv(tableHeightEnVar, "")

	smart := &fakeSmartTerminal{termWidth: 40}
	stat := NewStatusOutput(smart, "", false, false, false, false, false)
	smartStat := stat.(*smartStatusOutput)
	smartStat.sigwinchHandled = make(chan bool)

	runner := newRunner(stat, 2)

	action1 := &status.Action{Description: "action1"}
	result1 := status.ActionResult{
		Action: action1,
		Output: "Output1",
		Error:  fmt.Errorf("Error1"),
	}

	action2 := &status.Action{Description: "action2"}
	result2 := status.ActionResult{
		Action: action2,
		Output: "Output2",
		Error:  fmt.Errorf("Error1"),
	}

	runner.startAction(action1)
	runner.startAction(action2)
	runner.finishAction(result1)
	runner.finishAction(result2)

	stat.Flush()

	w := "\r\x1b[1m[  0% 0/2] action1\x1b[0m\x1b[K\r\x1b[1m[  0% 0/2] action2\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action1\x1b[0m\x1b[K\n\x1b[31m\x1b[1mFAILED:\x1b[0m \nOutput1\n\r\x1b[1m[100% 2/2] action2\x1b[0m\x1b[K\n\x1b[31m\x1b[1mFAILED:\x1b[0m \nOutput2\n"

	if g := smart.String(); g != w {
		t.Errorf("want:\n%q\ngot:\n%q", w, g)
	}
}

func TestSmartStatusSuppressOutput(t *testing.T) {
	unsetTestEnv(t, "NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	os.Setenv(tableHeightEnVar, "")

	smart := &fakeSmartTerminal{termWidth: 40}
	stat := NewStatusOutput(smart, "", false, false, false, false, true)
	smartStat := stat.(*smartStatusOutput)
	smartStat.sigwinchHandled = make(chan bool)

	runner := newRunner(stat, 2)

	action1 := &status.Action{Description: "action1"}
	result1 := status.ActionResult{
		Action: action1,
		Output: "Output1",
	}

	action2 := &status.Action{Description: "action2"}
	result2 := status.ActionResult{
		Action: action2,
		Output: "Output2",
		Error:  fmt.Errorf("Error1"),
	}

	runner.startAction(action1)
	runner.startAction(action2)
	runner.finishAction(result1)
	runner.finishAction(result2)

	stat.Flush()

	w := "\r\x1b[1m[  0% 0/2] action1\x1b[0m\x1b[K\r\x1b[1m[  0% 0/2] action2\x1b[0m\x1b[K\r\x1b[1m[ 50% 1/2] action1\x1b[0m\x1b[K\r\x1b[1m[100% 2/2] action2\x1b[0m\x1b[K\n\x1b[31m\x1b[1mFAILED:\x1b[0m \nOutput2\nThere was 1 action that completed after the action that failed. See verbose.log.gz for its output.\n"

	if g := smart.String(); g != w {
		t.Errorf("want:\n%q\ngot:\n%q", w, g)
	}
}
