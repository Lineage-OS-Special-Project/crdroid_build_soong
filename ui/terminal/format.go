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
	"regexp"
	"strings"
	"time"

	"android/soong/ui/status"
)

type formatter struct {
	colorize            bool
	phaseColorize       bool
	trueColor           bool
	diagnosticColorize  bool
	diagnosticTrueColor bool
	format              string
	quiet               bool
	start               time.Time
}

const lospStatusFormat = "__LOSP_NINJA_STATUS_V1__"

var (
	javacWarningRe = regexp.MustCompile(`^([-.\w/\\]+\.java:[0-9]+: )?(warning:) .*$`)
	javacErrorRe   = regexp.MustCompile(`^([-.\w/\\]+\.java:[0-9]+: )?(error:) .*$`)
)

// newFormatter returns a formatter for formatting output to
// the terminal in a format similar to Ninja.
// format takes nearly all the same options as NINJA_STATUS.
// %c is currently unsupported.
func newFormatter(colorize bool, format string, quiet bool) formatter {
	return formatter{
		colorize: colorize,
		format:   format,
		quiet:    quiet,
		start:    time.Now(),
	}
}

func (s formatter) message(level status.MsgLevel, message string) string {
	if level >= status.ErrorLvl {
		return fmt.Sprintf("%s %s", s.failedString(), message)
	} else if level > status.StatusLvl {
		return fmt.Sprintf("%s%s", level.Prefix(), message)
	} else if level == status.StatusLvl {
		return message
	}
	return ""
}

func (s formatter) phaseStatus(phase status.BuildPhase, description string) string {
	marker := "[" + string(phase) + "]"
	if !s.phaseColorize {
		return marker + " " + description
	}
	return ansi.blueViolet(s.trueColor) + marker + ansi.regular() + ansi.bold() + " " + description
}

func remainingTimeString(t time.Time) string {
	now := time.Now()
	if t.After(now) {
		return t.Sub(now).Round(time.Duration(time.Second)).String()
	}
	return time.Duration(0).Round(time.Duration(time.Second)).String()
}
func (s formatter) progress(counts status.Counts) string {
	if s.format == lospStatusFormat && counts.TotalActions == 0 {
		counts.TotalActions = 1
	}
	if s.format == lospStatusFormat {
		var b strings.Builder
		b.WriteString("[")
		b.WriteString(s.progressColor(ansi.teal(s.trueColor)))
		fmt.Fprintf(&b, "%d", counts.FinishedActions)
		b.WriteString(s.progressColor(ansi.regular()))
		b.WriteString("/")
		b.WriteString(s.progressColor(ansi.teal(s.trueColor)))
		fmt.Fprintf(&b, "%d", counts.TotalActions)
		b.WriteString(s.progressColor(ansi.regular()))
		b.WriteString(" (")
		b.WriteString(s.progressColor(ansi.blueViolet(s.trueColor)))
		fmt.Fprintf(&b, "%3d%%", 100*counts.FinishedActions/counts.TotalActions)
		b.WriteString(s.progressColor(ansi.regular()))
		b.WriteString(") Elapsed: ")
		b.WriteString(s.progressColor(ansi.yellowColor(s.trueColor)))
		fmt.Fprintf(&b, "%.3f", time.Since(s.start).Seconds())
		b.WriteString(s.progressColor(ansi.regular()))
		b.WriteString("] ")
		return b.String()
	}
	if s.format == "" {
		output := fmt.Sprintf("[%3d%% %d/%d", 100*counts.FinishedActions/counts.TotalActions, counts.FinishedActions, counts.TotalActions)

		if !counts.EstimatedTime.IsZero() {
			output += fmt.Sprintf(" %s remaining", remainingTimeString(counts.EstimatedTime))
		}
		output += "] "
		return output
	}

	buf := &strings.Builder{}
	for i := 0; i < len(s.format); i++ {
		c := s.format[i]
		if c != '%' {
			buf.WriteByte(c)
			continue
		}

		i = i + 1
		if i == len(s.format) {
			buf.WriteByte(c)
			break
		}

		c = s.format[i]
		switch c {
		case '%':
			buf.WriteByte(c)
		case 's':
			fmt.Fprintf(buf, "%d", counts.StartedActions)
		case 't':
			fmt.Fprintf(buf, "%d", counts.TotalActions)
		case 'r':
			fmt.Fprintf(buf, "%d", counts.RunningActions)
		case 'u':
			fmt.Fprintf(buf, "%d", counts.TotalActions-counts.StartedActions)
		case 'f':
			fmt.Fprintf(buf, "%d", counts.FinishedActions)
		case 'o':
			fmt.Fprintf(buf, "%.1f", float64(counts.FinishedActions)/time.Since(s.start).Seconds())
		case 'c':
			// TODO: implement?
			buf.WriteRune('?')
		case 'p':
			fmt.Fprintf(buf, "%3d%%", 100*counts.FinishedActions/counts.TotalActions)
		case 'e':
			fmt.Fprintf(buf, "%.3f", time.Since(s.start).Seconds())
		case 'l':
			if counts.EstimatedTime.IsZero() {
				// No esitimated data
				buf.WriteRune('?')
			} else {
				fmt.Fprintf(buf, "%s", remainingTimeString(counts.EstimatedTime))
			}
		default:
			buf.WriteString("unknown placeholder '")
			buf.WriteByte(c)
			buf.WriteString("'")
		}
	}
	return buf.String()
}

func (s formatter) progressColor(color string) string {
	if s.colorize {
		return color
	}
	return ""
}

func (s formatter) result(result status.ActionResult) string {
	output := result.Output
	if s.diagnosticColorize && strings.Contains(result.Command, "soong_javac_wrapper") {
		output = colorJavacDiagnostics(output, s.diagnosticTrueColor)
	}

	var ret string
	if result.Error != nil {
		targets := strings.Join(result.Outputs, " ")
		if s.quiet || result.Command == "" {
			ret = fmt.Sprintf("%s %s\n%s", s.failedString(), targets, output)
		} else {
			ret = fmt.Sprintf("%s %s\n%s\n%s", s.failedString(), targets, result.Command, output)
		}
	} else if output != "" {
		ret = output
	}

	if len(ret) > 0 && ret[len(ret)-1] != '\n' {
		ret += "\n"
	}

	return ret
}

func colorJavacDiagnostics(output string, trueColor bool) string {
	lines := strings.SplitAfter(output, "\n")
	for i, line := range lines {
		ending := ""
		if strings.HasSuffix(line, "\n") {
			line, ending = strings.TrimSuffix(line, "\n"), "\n"
		}
		for _, diagnostic := range []struct {
			pattern *regexp.Regexp
			color   string
		}{
			{javacWarningRe, ansi.yellowColor(trueColor)},
			{javacErrorRe, ansi.redColor(trueColor)},
		} {
			match := diagnostic.pattern.FindStringSubmatchIndex(line)
			if match == nil {
				continue
			}
			start, end := match[4], match[5]
			line = line[:start] + ansi.bold() + diagnostic.color + line[start:end] + ansi.regular() + line[end:]
			break
		}
		lines[i] = line + ending
	}
	return strings.Join(lines, "")
}

func (s formatter) failedString() string {
	failed := "FAILED:"
	if s.colorize {
		failed = ansi.redColor(s.trueColor) + ansi.bold() + failed + ansi.regular()
	}
	return failed
}
