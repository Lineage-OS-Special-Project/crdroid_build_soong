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
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"android/soong/ui/status"
)

const tableHeightEnVar = "SOONG_UI_TABLE_HEIGHT"

type actionTableEntry struct {
	action    *status.Action
	startTime time.Time
}

type smartStatusOutput struct {
	writer         io.Writer
	formatter      formatter
	suppressOutput bool

	lock sync.Mutex

	haveBlankLine bool

	tableMode             bool
	tableHeight           int
	tableRowsDrawn        int
	requestedTableHeight  int
	termWidth, termHeight int

	runningActions  []actionTableEntry
	lastCounts      status.Counts
	ticker          *time.Ticker
	done            chan bool
	sigwinch        chan os.Signal
	sigwinchHandled chan bool

	// Once there is a failure, we stop printing command output so the error
	// is easier to find
	haveFailures bool
	tableStarted bool
	flushing     bool
	// If we are dropping errors, then at the end, we report a message to go
	// look in the verbose log if you want that command output.
	postFailureActionCount int
}

// NewSmartStatusOutput returns a StatusOutput that represents the
// current build status similarly to Ninja's built-in terminal
// output.
func NewSmartStatusOutput(w io.Writer,
	formatter formatter,
	suppressOutput bool) status.StatusOutput {
	return newSmartStatusOutput(w, formatter, suppressOutput, true)
}

func newSmartStatusOutput(w io.Writer, formatter formatter, suppressOutput, useActionTable bool) status.StatusOutput {
	s := &smartStatusOutput{
		writer:         w,
		formatter:      formatter,
		suppressOutput: suppressOutput,

		haveBlankLine: true,

		tableMode: useActionTable,

		done:     make(chan bool),
		sigwinch: make(chan os.Signal),
	}

	if env, ok := os.LookupEnv(tableHeightEnVar); ok {
		h, _ := strconv.Atoi(env)
		s.tableMode = s.tableMode && h > 0
		s.requestedTableHeight = h
	}

	if w, h, ok := termSize(s.writer); ok {
		s.termWidth, s.termHeight = w, h
		s.computeTableHeight()
	} else {
		s.tableMode = false
	}

	s.startSigwinch()

	return s
}

func (s *smartStatusOutput) Message(level status.MsgLevel, message string) {
	if level < status.StatusLvl {
		return
	}
	if isConsoleStartupNotice(level, message) || isConsoleNinjaSuccessSummary(level, message) || isSisoMemoryStallNotice(level, message) {
		return
	}
	if message == "Running globs..." {
		s.startupMessage(s.formatter.message(level, message), true)
		return
	}

	str := s.formatter.message(level, message)

	s.lock.Lock()
	defer s.lock.Unlock()

	if level > status.StatusLvl {
		s.printAndRedraw(str)
	} else if s.tableStarted {
		// Status messages replace the ordinary live row. Print them as output,
		// then restore the counts-and-elapsed row below the message.
		s.printAndRedraw(str)
	} else {
		s.statusLine(str)
	}
}

func (s *smartStatusOutput) PhaseStatus(phase status.BuildPhase, description string) {
	str := s.formatter.phaseStatus(phase, description)
	if phase == status.SoongPhase && description == "Blueprint bootstrap and graph analysis" {
		s.startupMessage(str, false)
		return
	}
	if phase == status.KatiPhase && description == "Makefile and packaging generation" ||
		phase == status.NinjaPhase && description == "Starting Ninja/Siso execution" {
		s.startupMessage(str, true)
		return
	}
	s.Message(status.StatusLvl, str)
}

func (s *smartStatusOutput) startupMessage(str string, trailingBlankLine bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.printAndRedrawWithBlankLine(str, trailingBlankLine)
}

func (s *smartStatusOutput) StartAction(action *status.Action, counts status.Counts) {
	startTime := time.Now()

	str := action.Description
	if str == "" {
		str = action.Command
	}

	progress := s.formatter.progress(counts)

	s.lock.Lock()
	defer s.lock.Unlock()

	s.runningActions = append(s.runningActions, actionTableEntry{
		action:    action,
		startTime: startTime,
	})
	s.lastCounts = counts
	// Start lazily on the first actual build action. This also covers builds
	// that do not write a header through this status writer first.
	s.startActionTable()

	if !s.tableMode {
		progress += str
	}
	s.statusLine(progress)
	if s.tableStarted {
		s.actionTable()
	}
}

func (s *smartStatusOutput) FinishAction(result status.ActionResult, counts status.Counts) {
	str := result.Description
	if str == "" {
		str = result.Command
	}

	output := s.formatter.result(result)

	progress := s.formatter.progress(counts) + str
	if s.tableMode {
		// Keep the live row identical to timer redraws. Action context belongs
		// in the running-action list or on the diagnostic block heading.
		progress = s.formatter.progress(counts)
		if output != "" {
			output = str + "\n" + output
		}
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	for i, runningAction := range s.runningActions {
		if runningAction.action == result.Action {
			s.runningActions = append(s.runningActions[:i], s.runningActions[i+1:]...)
			break
		}
	}
	s.lastCounts = counts

	s.statusLine(progress)

	// Stop printing when there are failures, but don't skip actions that also have their own errors.
	if output != "" {
		if (!s.haveFailures && !s.suppressOutput) || result.Error != nil {
			s.clearActionTable()
			if s.tableStarted && !s.haveBlankLine {
				// The completed row is transient status, not part of the
				// diagnostic block. Remove it before advancing to output.
				fmt.Fprint(s.writer, "\r", ansi.clearToEndOfLine())
			}
			s.requestLine()
			s.print(output)
			if s.tableStarted {
				s.statusLine(progress)
			}
		} else {
			s.postFailureActionCount++
		}
	}
	if s.tableStarted {
		s.actionTable()
	}

	if result.Error != nil {
		s.haveFailures = true
	}
}

func (s *smartStatusOutput) Flush() {
	s.lock.Lock()
	s.flushing = true
	tableStarted := s.tableStarted
	s.lock.Unlock()

	if tableStarted {
		// Stop the action table tick outside of the lock to avoid lock ordering issues between s.done and
		// s.lock, the goroutine in startActionTableTick can get blocked on the lock and be unable to read
		// from the channel.
		s.stopActionTableTick()
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	s.stopSigwinch()
	if s.tableStarted {
		// Clear rows below the live progress cursor before advancing it or
		// printing a final failure summary there.
		s.clearActionTable()
	}
	if s.tableStarted && !s.haveBlankLine {
		// Remove the final live progress row before handing the terminal back
		// to the shell wrapper or prompt.
		fmt.Fprint(s.writer, "\r", ansi.clearToEndOfLine())
	}

	if s.postFailureActionCount > 0 {
		s.requestLine()
		if s.postFailureActionCount == 1 {
			s.print(fmt.Sprintf("There was 1 action that completed after the action that failed. See verbose.log.gz for its output."))
		} else {
			s.print(fmt.Sprintf("There were %d actions that completed after the action that failed. See verbose.log.gz for their output.", s.postFailureActionCount))
		}
	}
	s.requestLine()

	s.runningActions = nil

	if s.tableStarted {
		// Turn the cursor back on
		fmt.Fprintf(s.writer, ansi.showCursor())
	}
}

func (s *smartStatusOutput) Write(p []byte) (int, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.tableStarted {
		s.printAndRedraw(string(p))
	} else {
		s.print(string(p))
		s.startActionTable()
	}
	return len(p), nil
}

func (s *smartStatusOutput) printAndRedraw(str string) {
	s.printAndRedrawWithBlankLine(str, false)
}

func (s *smartStatusOutput) printAndRedrawWithBlankLine(str string, trailingBlankLine bool) {
	s.clearActionTable()
	s.print(str)
	if trailingBlankLine {
		fmt.Fprintln(s.writer)
	}
	if s.tableStarted && !s.flushing {
		s.statusLine(s.formatter.progress(s.lastCounts))
		s.actionTable()
	}
}

func (s *smartStatusOutput) startActionTable() {
	if !s.tableMode || s.tableStarted || s.flushing {
		return
	}
	s.tableStarted = true
	fmt.Fprint(s.writer, ansi.hideCursor())
	s.actionTable()
	s.startActionTableTick()
}

func (s *smartStatusOutput) clearActionTable() {
	if !s.tableStarted || s.tableRowsDrawn == 0 {
		return
	}
	for row := 0; row < s.tableRowsDrawn; row++ {
		fmt.Fprint(s.writer, "\n\r", ansi.clearToEndOfLine())
	}
	fmt.Fprint(s.writer, ansi.cursorUp(s.tableRowsDrawn), "\r")
	s.tableRowsDrawn = 0
}

func (s *smartStatusOutput) requestLine() {
	if !s.haveBlankLine {
		fmt.Fprintln(s.writer)
		s.haveBlankLine = true
	}
}

func (s *smartStatusOutput) print(str string) {
	if !s.haveBlankLine {
		fmt.Fprint(s.writer, "\r", ansi.clearToEndOfLine())
		s.haveBlankLine = true
	}
	fmt.Fprint(s.writer, str)
	if len(str) == 0 || str[len(str)-1] != '\n' {
		fmt.Fprint(s.writer, "\n")
	}
}

func (s *smartStatusOutput) statusLine(str string) {
	idx := strings.IndexRune(str, '\n')
	if idx != -1 {
		str = str[0:idx]
	}

	// Limit line width to the terminal width, otherwise we'll wrap onto
	// another line and we won't delete the previous line.
	str = elide(str, s.termWidth)

	// Keep Ninja status lines bold, then clear the rest of the line.
	start := "\r" + ansi.bold()
	end := ansi.regular() + ansi.clearToEndOfLine()
	fmt.Fprint(s.writer, start, str, end)
	s.haveBlankLine = false
}

func elide(str string, width int) string {
	if width <= 0 {
		return str
	}

	// ANSI control sequences do not occupy terminal columns. Count only
	// printable bytes so colored status lines are elided at the same visible
	// width as uncolored lines.
	var b strings.Builder
	visibleBytes := 0
	for i := 0; i < len(str); {
		if str[i] == '\x1b' && i+1 < len(str) && str[i+1] == '[' {
			start := i
			i += 2
			for i < len(str) && (str[i] < 0x40 || str[i] > 0x7e) {
				i++
			}
			if i < len(str) {
				i++ // Include the CSI final byte.
			}
			b.WriteString(str[start:i])
			continue
		}
		if visibleBytes >= width {
			break
		}
		b.WriteByte(str[i])
		i++
		visibleBytes++
	}
	return b.String()
}

func (s *smartStatusOutput) startActionTableTick() {
	s.ticker = time.NewTicker(time.Second)
	go func() {
		for {
			select {
			case <-s.ticker.C:
				s.lock.Lock()
				s.statusLine(s.formatter.progress(s.lastCounts))
				s.actionTable()
				s.lock.Unlock()
			case <-s.done:
				return
			}
		}
	}()
}

func (s *smartStatusOutput) stopActionTableTick() {
	s.ticker.Stop()
	s.done <- true
}

func (s *smartStatusOutput) startSigwinch() {
	signal.Notify(s.sigwinch, syscall.SIGWINCH)
	go func() {
		for _ = range s.sigwinch {
			s.lock.Lock()
			s.updateTermSize()
			if s.tableStarted {
				s.actionTable()
			}
			s.lock.Unlock()
			if s.sigwinchHandled != nil {
				s.sigwinchHandled <- true
			}
		}
	}()
}

func (s *smartStatusOutput) stopSigwinch() {
	signal.Stop(s.sigwinch)
	close(s.sigwinch)
}

// computeTableHeight recomputes s.tableHeight based on s.termHeight and s.requestedTableHeight.
func (s *smartStatusOutput) computeTableHeight() {
	tableHeight := s.requestedTableHeight
	if tableHeight == 0 {
		tableHeight = s.termHeight / 4
		if tableHeight < 1 {
			tableHeight = 1
		} else if tableHeight > 10 {
			tableHeight = 10
		}
	}
	if tableHeight > s.termHeight-1 {
		tableHeight = s.termHeight - 1
	}
	s.tableHeight = tableHeight
}

// updateTermSize recomputes the table height after a SIGWINCH and pans any existing text if
// necessary.
func (s *smartStatusOutput) updateTermSize() {
	if w, h, ok := termSize(s.writer); ok {
		s.termWidth, s.termHeight = w, h

		if s.tableMode {
			s.computeTableHeight()
		}
	}
}

func (s *smartStatusOutput) actionTable() {
	// Line feeds create the rows below progress and scroll the screen when the
	// live row is near the bottom. Cursor-down alone is clipped at the bottom,
	// which would make the first action row overwrite Elapsed.

	rows := s.tableHeight
	if s.tableRowsDrawn > rows {
		rows = s.tableRowsDrawn
	}
	for tableLine := 0; tableLine < rows; tableLine++ {
		fmt.Fprint(s.writer, "\n\r")

		if tableLine < s.tableHeight && tableLine < len(s.runningActions) {
			runningAction := s.runningActions[tableLine]

			seconds := int(time.Since(runningAction.startTime).Round(time.Second).Seconds())

			desc := runningAction.action.Description
			if desc == "" {
				desc = runningAction.action.Command
			}

			color := ""
			if s.formatter.colorize {
				if seconds >= 60 {
					color = ansi.redColor(s.formatter.trueColor) + ansi.bold()
				} else if seconds >= 30 {
					color = ansi.yellowColor(s.formatter.trueColor) + ansi.bold()
				}
			}

			durationStr := fmt.Sprintf("   %2d:%02d ", seconds/60, seconds%60)
			desc = elide(desc, s.termWidth-len(durationStr))
			if color != "" {
				durationStr = color + durationStr + ansi.regular()
			}
			fmt.Fprint(s.writer, durationStr, desc)
		}
		fmt.Fprint(s.writer, ansi.clearToEndOfLine())
	}

	fmt.Fprint(s.writer, ansi.cursorUp(rows), "\r")
	s.tableRowsDrawn = s.tableHeight
}

var ansi = ansiImpl{}

type ansiImpl struct{}

func (ansiImpl) clearToEndOfLine() string {
	return "\x1b[K"
}

func (ansiImpl) cursorUp(rows int) string {
	return fmt.Sprintf("\x1b[%dA", rows)
}

func (ansiImpl) redColor(trueColor bool) string {
	if trueColor {
		return "\x1b[38;2;201;87;87m"
	}
	return "\x1b[31m"
}

func (ansiImpl) teal(trueColor bool) string {
	if trueColor {
		return "\x1b[38;2;16;200;200m"
	}
	return "\x1b[36m"
}

func (ansiImpl) blueViolet(trueColor bool) string {
	if trueColor {
		return "\x1b[38;2;129;143;232m"
	}
	return "\x1b[34m"
}

func (ansiImpl) yellowColor(trueColor bool) string {
	if trueColor {
		return "\x1b[38;2;201;150;50m"
	}
	return "\x1b[33m"
}

func (ansiImpl) bold() string {
	return "\x1b[1m"
}

func (ansiImpl) regular() string {
	return "\x1b[0m"
}

func (ansiImpl) showCursor() string {
	return "\x1b[?25h"
}

func (ansiImpl) hideCursor() string {
	return "\x1b[?25l"
}
