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
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"android/soong/ui/status"
)

func TestPhaseStatusColorPolicy(t *testing.T) {
	const description = "Blueprint bootstrap and graph analysis"
	setTestColorEnv(t, "")

	t.Run("smart terminal colors marker and bolds description", func(t *testing.T) {
		setTestColorEnv(t, "")
		smart := &fakeSmartTerminal{termWidth: 100}
		statusOutput := NewStatusOutput(smart, "", false, false, false, false, false)
		statusOutput.(*smartStatusOutput).PhaseStatus(status.SoongPhase, description)
		statusOutput.Flush()

		got := smart.String()
		want := "\x1b[34m[Soong]\x1b[0m\x1b[1m " + description
		if !strings.Contains(got, want) {
			t.Fatalf("smart status does not contain colored phase marker %q: %q", want, got)
		}
		if strings.Count(got, "\x1b[34m") != 1 || strings.Count(got, "\x1b[1m") != 2 {
			t.Fatalf("expected one blue-violet and one bold sequence, got %q", got)
		}
		if plain := string(stripAnsiEscapes([]byte(got))); !strings.Contains(plain, "[Soong] "+description) {
			t.Fatalf("plain rendering = %q", plain)
		}
	})

	t.Run("NO_COLOR disables phase color", func(t *testing.T) {
		setTestColorEnv(t, "")
		t.Setenv("NO_COLOR", "1")
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
		setTestColorEnv(t, "")
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
	setTestColorEnv(t, "")
	tests := []struct {
		name, colorTerm, noColor, term string
		color, failure                 string
	}{
		{"truecolor", "truecolor", "", "xterm", "38;2;129;143;232m", "38;2;201;87;87m"},
		{"24bit", "24bit", "", "xterm", "38;2;129;143;232m", "38;2;201;87;87m"},
		{"ansi", "", "", "xterm", "34m", "31m"},
		{"no color", "truecolor", "1", "xterm", "", ""},
		{"dumb", "truecolor", "", "dumb", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestColorEnv(t, tt.colorTerm)
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
	setTestColorEnv(t, "truecolor")
	t.Setenv("COLORTERM", "truecolor")
	f := newFormatter(true, lospStatusFormat, false)
	f.colorize, f.trueColor = true, true
	f.start = time.Now().Add(-time.Second)
	got := f.progress(status.Counts{FinishedActions: 3, TotalActions: 10})
	for _, sequence := range []string{"38;2;16;200;200m", "38;2;129;143;232m", "38;2;201;150;50m"} {
		if !strings.Contains(got, sequence) {
			t.Errorf("progress missing %q: %q", sequence, got)
		}
	}
	if strings.Contains(got, "38;2;69;169;120m") || strings.Contains(got, "38;2;79;134;168m") {
		t.Fatalf("progress contains retired green/blue styling: %q", got)
	}
	if strings.Contains(got, `\033`) || !strings.Contains(got, "%") {
		t.Fatalf("invalid progress rendering: %q", got)
	}
	zero := f.progress(status.Counts{})
	zeroPlain := string(stripAnsiEscapes([]byte(zero)))
	if !strings.Contains(zeroPlain, "0/1") || !strings.Contains(zeroPlain, "  0%") {
		t.Fatalf("zero-action progress = %q", zero)
	}
	plain := newFormatter(false, lospStatusFormat, false).progress(status.Counts{FinishedActions: 3, TotalActions: 10})
	if strings.Contains(plain, "\x1b") {
		t.Fatalf("plain progress contains ANSI: %q", plain)
	}
}

func TestLOSPStatusThroughBuildEntryFormat(t *testing.T) {
	setTestColorEnv(t, "truecolor")
	t.Setenv("NINJA_STATUS", lospStatusFormat)

	output := &fakeSmartTerminal{termWidth: 200, termHeight: 3}
	statusOutput := NewStatusOutput(output, os.Getenv("NINJA_STATUS"), false, false, false, false, false)
	statusOutput.FinishAction(status.ActionResult{Action: &status.Action{Description: "compile module"}},
		status.Counts{FinishedActions: 3, TotalActions: 10})

	got := output.String()
	plain := string(stripAnsiEscapes([]byte(got)))
	if !strings.Contains(plain, "3/10 ( 30%) Elapsed:") {
		t.Fatalf("entry formatter omitted elapsed time: %q", got)
	}
	if !strings.Contains(got, "38;2;201;150;50m") {
		t.Fatalf("entry formatter omitted amber elapsed styling: %q", got)
	}
}

func TestSmartProgressAndPhasePresentation(t *testing.T) {
	setTestColorEnv(t, "truecolor")
	term := &fakeSmartTerminal{termWidth: 200, termHeight: 3}
	stat := NewStatusOutput(term, lospStatusFormat, false, false, false, false, false)
	stat.(interface {
		PhaseStatus(status.BuildPhase, string)
	}).PhaseStatus(status.SoongPhase, "Blueprint bootstrap and graph analysis")
	stat.StartAction(&status.Action{Description: "compile module"}, status.Counts{TotalActions: 10})
	stat.FinishAction(status.ActionResult{Action: &status.Action{Description: "compile module"}}, status.Counts{FinishedActions: 3, TotalActions: 10})
	stat.Flush()
	got := term.String()
	plain := string(stripAnsiEscapes([]byte(got)))
	for _, want := range []string{"[Soong] Blueprint bootstrap and graph analysis", "3/10 ( 30%) Elapsed:", "compile module"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("smart fixture missing %q in %q", want, plain)
		}
	}
	for _, sgr := range []string{"38;2;129;143;232m", "38;2;16;200;200m", "38;2;201;150;50m", "\\x1b[1m"} {
		sequence := sgr
		if sequence == "\\x1b[1m" {
			sequence = "\x1b[1m"
		}
		if !strings.Contains(got, sequence) {
			t.Fatalf("smart fixture missing %q in %q", sequence, got)
		}
	}
}

func TestNoColorDisablesSmartStatusStyling(t *testing.T) {
	setTestColorEnv(t, "")
	term := &fakeSmartTerminal{termWidth: 80, termHeight: 2}
	f := newFormatter(true, lospStatusFormat, false)
	f.colorize = false
	s := &smartStatusOutput{
		writer:      term,
		formatter:   f,
		termWidth:   80,
		termHeight:  2,
		tableHeight: 1,
		runningActions: []actionTableEntry{{
			action:    &status.Action{Description: "long action"},
			startTime: time.Now().Add(-40 * time.Second),
		}},
	}
	s.statusLine(f.progress(status.Counts{FinishedActions: 1, TotalActions: 2}) + " regular action")
	s.actionTable()
	got := term.String()
	for _, sgr := range []string{"\x1b[31m", "\x1b[33m", "38;2;"} {
		if strings.Contains(got, sgr) {
			t.Fatalf("NO_COLOR status contains SGR %q: %q", sgr, got)
		}
	}
}

func TestColorModeUsesStatusOutputPolicy(t *testing.T) {
	setTestColorEnv(t, "")
	tests := []struct {
		name, term, colorTerm, noColor string
		forceSimple, forceANSI         bool
		writerTTY                      bool
		want                           string
	}{
		{"interactive truecolor", "xterm-256color", "truecolor", "", false, false, true, "truecolor"},
		{"interactive ANSI fallback", "xterm-256color", "", "", false, false, true, "ansi"},
		{"redirected", "xterm-256color", "truecolor", "", false, false, false, "none"},
		{"simple output", "xterm-256color", "truecolor", "", true, false, true, "none"},
		{"explicit ANSI output", "xterm-256color", "", "", false, true, false, "ansi"},
		{"NO_COLOR wins", "xterm-256color", "truecolor", "1", false, true, true, "none"},
		{"dumb wins", "dumb", "truecolor", "", false, true, true, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestColorEnv(t, tt.colorTerm)
			if tt.noColor != "" {
				t.Setenv("NO_COLOR", tt.noColor)
			}
			if tt.colorTerm != "" {
				t.Setenv("COLORTERM", tt.colorTerm)
			}
			if tt.term != "" {
				t.Setenv("TERM", tt.term)
			}
			var writer io.Writer = &bytes.Buffer{}
			if tt.writerTTY {
				writer = &fakeSmartTerminal{termWidth: 80}
			}
			if got := ColorMode(writer, tt.forceSimple, tt.forceANSI); got != tt.want {
				t.Fatalf("ColorMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJavacDiagnosticsAreStyledAtReplay(t *testing.T) {
	setTestColorEnv(t, "")
	const command = "out/soong/host/linux-x86/bin/soong_javac_wrapper javac"
	output := "File.java:7: warning: warn details\n     ^\nFile.java:8: error: missing symbol\n    ^\n"
	result := status.ActionResult{
		Action: &status.Action{Command: command},
		Output: output,
	}

	trueColor := newFormatter(true, "", false)
	trueColor.diagnosticColorize = true
	trueColor.diagnosticTrueColor = true
	wantTrueColor := "File.java:7: \x1b[1m\x1b[38;2;201;150;50mwarning:\x1b[0m warn details\n     ^\n" +
		"File.java:8: \x1b[1m\x1b[38;2;201;87;87merror:\x1b[0m missing symbol\n    ^\n"
	if got := trueColor.result(result); got != wantTrueColor {
		t.Fatalf("true-color javac output = %q, want %q", got, wantTrueColor)
	}

	ansi := newFormatter(true, "", false)
	ansi.diagnosticColorize = true
	wantANSI := "File.java:7: \x1b[1m\x1b[33mwarning:\x1b[0m warn details\n     ^\n" +
		"File.java:8: \x1b[1m\x1b[31merror:\x1b[0m missing symbol\n    ^\n"
	if got := ansi.result(result); got != wantANSI {
		t.Fatalf("ANSI javac output = %q, want %q", got, wantANSI)
	}

	plain := newFormatter(false, "", false)
	if got := plain.result(result); got != output {
		t.Fatalf("plain javac output = %q, want %q", got, output)
	}

	// Similar compiler output stays untouched; this styling applies only to
	// action results whose recorded command invokes the javac wrapper.
	external := status.ActionResult{
		Action: &status.Action{Command: "clang++ -c test.cpp"},
		Output: "test.cpp:7: warning: compiler detail\n",
	}
	if got := trueColor.result(external); got != external.Output {
		t.Fatalf("external compiler output changed: %q", got)
	}
}

func TestJavacReplayHonorsTerminalPolicy(t *testing.T) {
	for _, tt := range []struct {
		name, term, colorTerm, noColor string
		forceANSI                      bool
		wantANSI                       bool
	}{
		{"redirected plain", "xterm-256color", "truecolor", "", false, false},
		{"explicit truecolor", "xterm-256color", "truecolor", "", true, true},
		{"explicit ANSI fallback", "xterm-256color", "", "", true, true},
		{"NO_COLOR overrides explicit", "xterm-256color", "truecolor", "1", true, false},
		{"TERM dumb overrides explicit", "dumb", "truecolor", "", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setTestColorEnv(t, tt.colorTerm)
			t.Setenv("TERM", tt.term)
			if tt.colorTerm != "" {
				t.Setenv("COLORTERM", tt.colorTerm)
			}
			if tt.noColor != "" {
				t.Setenv("NO_COLOR", tt.noColor)
			}
			var out bytes.Buffer
			stat := NewStatusOutput(&out, "", false, false, tt.forceANSI, false, false)
			stat.FinishAction(status.ActionResult{
				Action: &status.Action{Command: "soong_javac_wrapper javac", Description: "compile"},
				Output: "File.java:7: warning: detail\n",
			}, status.Counts{FinishedActions: 1, TotalActions: 1})
			stat.Flush()
			got := out.String()
			if strings.Contains(got, "\x1b[") != tt.wantANSI {
				t.Fatalf("output ANSI presence = %v, want %v: %q", strings.Contains(got, "\x1b["), tt.wantANSI, got)
			}
			if strings.Contains(got, "\x1b[") && !strings.Contains(got, "warning:") {
				t.Fatalf("colored diagnostic lost its label: %q", got)
			}
		})
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

func setTestColorEnv(t *testing.T, colorTerm string) {
	t.Helper()
	unsetTestEnv(t, "NO_COLOR")
	unsetTestEnv(t, "SOONG_UI_ANSI_OUTPUT")
	t.Setenv("TERM", "xterm-256color")
	if colorTerm == "" {
		unsetTestEnv(t, "COLORTERM")
	} else {
		t.Setenv("COLORTERM", colorTerm)
	}
}

func TestStatusOutput(t *testing.T) {
	setTestColorEnv(t, "")
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
	setTestColorEnv(t, "")
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
	setTestColorEnv(t, "")
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
	setTestColorEnv(t, "")
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
	setTestColorEnv(t, "")
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
	setTestColorEnv(t, "")
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
	setTestColorEnv(t, "")
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
