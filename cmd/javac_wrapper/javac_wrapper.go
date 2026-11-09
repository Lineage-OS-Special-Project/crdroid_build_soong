// Copyright 2017 Google Inc. All rights reserved.
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

// soong_javac_wrapper expects a javac command line and argments, executes
// it, and produces an ANSI colorized version of the output on stdout.
//
// It also hides the unhelpful and unhideable "warning there is a warning"
// messages.
//
// Each javac build statement has an order-only dependency on the
// soong_javac_wrapper tool, which means the javac command will not be rerun
// if soong_javac_wrapper changes.  That means that soong_javac_wrapper must
// not do anything that will affect the results of the build.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"syscall"
)

var warningCount = regexp.MustCompile(`^([0-9]+) warning(s)?$`)

func main() {
	exitCode, err := Main(os.Stdout, os.Args[0], os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
	os.Exit(exitCode)
}

func Main(out io.Writer, name string, args []string) (int, error) {
	if len(args) < 1 {
		return 1, fmt.Errorf("usage: %s javac ...", name)
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		return 1, fmt.Errorf("creating output pipe: %s", err)
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = pw
	cmd.Stderr = pw
	err = cmd.Start()
	if err != nil {
		return 1, fmt.Errorf("starting subprocess: %s", err)
	}

	pw.Close()

	proc := processor{}
	// Process subprocess stdout asynchronously
	errCh := make(chan error)
	go func() {
		errCh <- proc.process(pr, out)
	}()

	// Wait for subprocess to finish
	cmdErr := cmd.Wait()

	// Wait for asynchronous stdout processing to finish
	err = <-errCh

	// Check for subprocess exit code
	if cmdErr != nil {
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if status.Exited() {
					return status.ExitStatus(), nil
				} else if status.Signaled() {
					exitCode := 128 + int(status.Signal())
					return exitCode, nil
				} else {
					return 1, exitErr
				}
			} else {
				return 1, nil
			}
		}
	}

	if err != nil {
		return 1, err
	}

	return 0, nil
}

type processor struct {
	silencedWarnings int
}

func (proc *processor) process(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	// Some javac wrappers output the entire list of java files being
	// compiled on a single line, which can be very large, set the maximum
	// buffer size to 2MB.
	scanner.Buffer(nil, 2*1024*1024)
	for scanner.Scan() {
		proc.processLine(w, scanner.Text())
	}
	err := scanner.Err()
	if err != nil {
		return fmt.Errorf("scanning input: %s", err)
	}
	return nil
}

func (proc *processor) processLine(w io.Writer, line string) {
	for _, f := range warningFilters {
		if f.MatchString(line) {
			proc.silencedWarnings++
			return
		}
	}
	for _, f := range filters {
		if f.MatchString(line) {
			return
		}
	}
	if match := warningCount.FindStringSubmatch(line); match != nil {
		c, err := strconv.Atoi(match[1])
		if err == nil {
			c -= proc.silencedWarnings
			if c == 0 {
				return
			} else {
				line = fmt.Sprintf("%d warning", c)
				if c > 1 {
					line += "s"
				}
			}
		}
	}
	fmt.Fprintln(w, line)
}

var warningFilters = []*regexp.Regexp{
	regexp.MustCompile(`bootstrap class path not set in conjunction with -source`),
	regexp.MustCompile(`source value 8 is obsolete and will be removed in a future release`),
	regexp.MustCompile(`target value 8 is obsolete and will be removed in a future release`),
	regexp.MustCompile(`To suppress warnings about obsolete options, use -Xlint:-options.`),
}

var filters = []*regexp.Regexp{
	regexp.MustCompile(`Note: (Some input files|.*\.java) uses? or overrides? a deprecated API.`),
	regexp.MustCompile(`Note: Recompile with -Xlint:deprecation for details.`),
	regexp.MustCompile(`Note: (Some input files|.*\.java) uses? unchecked or unsafe operations.`),
	regexp.MustCompile(`Note: Recompile with -Xlint:unchecked for details.`),

	regexp.MustCompile(`javadoc: warning - The old Doclet and Taglet APIs in the packages`),
	regexp.MustCompile(`com.sun.javadoc, com.sun.tools.doclets and their implementations`),
	regexp.MustCompile(`are planned to be removed in a future JDK release. These`),
	regexp.MustCompile(`components have been superseded by the new APIs in jdk.javadoc.doclet.`),
	regexp.MustCompile(`Users are strongly recommended to migrate to the new APIs.`),

	regexp.MustCompile(`javadoc: option --boot-class-path not allowed with target 1.9`),
}
