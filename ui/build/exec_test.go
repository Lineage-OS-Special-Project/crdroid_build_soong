// Copyright 2026 The Android Open Source Project
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

package build

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"android/soong/ui/logger"
	"android/soong/ui/status"
	"android/soong/ui/terminal"
)

func TestRunAndStreamKeepsNinjaSuccessSummaryOutOfConsole(t *testing.T) {
	tests := []struct {
		name        string
		commandName string
		backend     ninjaCommandType
		steps       string
		message     string
		hideSummary bool
	}{
		{name: "Siso Blueprint bootstrap, one step", commandName: "soong bootstrap", backend: NINJA_SISO, steps: "1", hideSummary: true},
		{name: "Siso Blueprint bootstrap, zero steps", commandName: "soong bootstrap", backend: NINJA_SISO, steps: "0", hideSummary: true},
		{name: "main Siso build, one step", commandName: "ninja", backend: NINJA_SISO, steps: "1", hideSummary: true},
		{name: "main Siso build, zero steps", commandName: "ninja", backend: NINJA_SISO, steps: "0", hideSummary: true},
		{name: "Ninja bootstrap", commandName: "soong bootstrap", backend: NINJA_NINJA, steps: "1", hideSummary: true},
		{name: "bare bootstrap marker", commandName: "soong bootstrap", backend: NINJA_SISO, message: "Build Succeeded", hideSummary: true},
		{name: "bare main marker", commandName: "ninja", backend: NINJA_SISO, message: "Build Succeeded", hideSummary: true},
		{name: "unrelated bare success", commandName: "module generator", backend: NINJA_SISO, message: "Build Succeeded"},
		{name: "unrelated Ninja success message", commandName: "ninja", backend: NINJA_SISO, message: "Build Succeeded for module metadata"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := testContext()
			config := testConfig()
			config.ninjaCommand = test.backend
			var console bytes.Buffer
			ctx.Status.AddOutput(terminal.NewStatusOutput(&console, "", true, false, false, false, false))
			logPath := filepath.Join(t.TempDir(), "verbose.log")
			ctx.Status.AddOutput(status.NewVerboseLog(logger.New(io.Discard), logPath))
			message := test.message
			if message == "" {
				message = "ninja:  4m51.01s Build Succeeded: " + test.steps + " steps - 0.00/s"
			}

			cmd := Command(ctx, config, nil, test.commandName, "/bin/sh", "-c",
				"printf '"+message+"\\nwarning: keep bootstrap warning\\nerror: keep bootstrap error\\nFAILED: keep failure summary\\n'")
			cmd.RunAndStreamOrFatal()
			ctx.Status.Finish()

			consoleText := console.String()
			if got := strings.Contains(consoleText, "Build Succeeded"); got == test.hideSummary {
				t.Fatalf("console summary visibility = %t, hideSummary=%t: %q", got, test.hideSummary, consoleText)
			}
			for _, want := range []string{"warning: keep bootstrap warning", "error: keep bootstrap error", "FAILED: keep failure summary"} {
				if !strings.Contains(consoleText, want) {
					t.Errorf("console omitted %q: %q", want, consoleText)
				}
			}

			var logText []byte
			deadline := time.Now().Add(time.Second)
			for {
				file, err := os.Open(logPath + ".gz")
				if err != nil {
					t.Fatal(err)
				}
				reader, err := gzip.NewReader(file)
				if err == nil {
					logText, err = io.ReadAll(reader)
					_ = reader.Close()
				}
				_ = file.Close()
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("verbose log did not finish writing: %v", err)
				}
				time.Sleep(time.Millisecond)
			}
			if !strings.Contains(string(logText), "Build Succeeded") {
				t.Errorf("verbose log omitted Siso summary: %q", logText)
			}
		})
	}
}
