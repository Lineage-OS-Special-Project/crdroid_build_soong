// Copyright 2020 Google Inc. All rights reserved.
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
	"android/soong/ui/logger"
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestInstallCleanOutput(t *testing.T) {
	root := t.TempDir()
	outDir := filepath.Join(root, "out")
	productOut := filepath.Join(outDir, "target", "product", "test")
	dataFile := filepath.Join(productOut, "data", "example")
	logFile := filepath.Join(outDir, "verbose.log.gz")
	imageFile := filepath.Join(productOut, "system.img")
	stagingFile := filepath.Join(productOut, "install", "system.img")
	for _, path := range []string{dataFile, logFile, imageFile, stagingFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	output := &bytes.Buffer{}
	ctx := testContext()
	ctx.Logger = logger.New(output)
	ctx.Writer = output
	config := Config{&configImpl{
		environ:      (&Environment{}),
		targetDevice: "test",
	}}
	config.environ.Set("OUT_DIR", outDir)

	installClean(ctx, config)

	got := output.String()
	want := "Install clean completed: data, logs, images and staging removed."
	if !strings.Contains(got, want) || strings.Count(got, "\n") != 1 || strings.Count(got, want) != 1 {
		t.Fatalf("installClean output = %q, want exactly one summary line containing %q", got, want)
	}
	for _, path := range []string{dataFile, logFile, imageFile, stagingFile} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("installClean left %s: stat error = %v", path, err)
		}
	}
}

func TestDataCleanOutput(t *testing.T) {
	root := t.TempDir()
	productOut := filepath.Join(root, "target", "product", "test")
	dataFile := filepath.Join(productOut, "data", "example")
	if err := os.MkdirAll(filepath.Dir(dataFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	output := &bytes.Buffer{}
	ctx := testContext()
	ctx.Logger = logger.New(output)
	ctx.Writer = output
	config := Config{&configImpl{environ: (&Environment{}), targetDevice: "test"}}
	config.environ.Set("OUT_DIR", root)

	dataClean(ctx, config)

	if got, want := output.String(), "Entire data directory removed.\n"; !strings.HasSuffix(got, want) || strings.Count(got, "Entire data directory removed.") != 1 {
		t.Fatalf("dataClean output = %q, want one success message ending in %q", got, want)
	}
	if _, err := os.Stat(dataFile); !os.IsNotExist(err) {
		t.Errorf("dataClean left %s: stat error = %v", dataFile, err)
	}
}

func TestInstallCleanDoesNotReportSuccessAfterGlobFailure(t *testing.T) {
	output := &bytes.Buffer{}
	ctx := testContext()
	ctx.Logger = logger.New(output)
	ctx.Writer = output
	config := Config{&configImpl{environ: &Environment{}, targetDevice: "test"}}
	config.environ.Set("OUT_DIR", filepath.Join(t.TempDir(), "invalid["))

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("installClean did not propagate the invalid glob failure")
		}
		if strings.Contains(output.String(), "Install clean completed:") {
			t.Fatalf("installClean printed success after failure: %q", output.String())
		}
	}()

	installClean(ctx, config)
}

func TestCleanOldFiles(t *testing.T) {
	dir, err := ioutil.TempDir("", "testcleanoldfiles")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	ctx := testContext()
	logBuf := &bytes.Buffer{}
	ctx.Logger = logger.New(logBuf)

	touch := func(names ...string) {
		for _, name := range names {
			if f, err := os.Create(filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			} else {
				f.Close()
			}
		}
	}
	runCleanOldFiles := func(names ...string) {
		data := []byte(strings.Join(names, " "))
		if err := ioutil.WriteFile(filepath.Join(dir, ".installed"), data, 0666); err != nil {
			t.Fatal(err)
		}

		cleanOldFiles(ctx, dir, ".installed")
	}

	assertFileList := func(names ...string) {
		t.Helper()

		sort.Strings(names)

		var foundNames []string
		if foundFiles, err := ioutil.ReadDir(dir); err == nil {
			for _, fi := range foundFiles {
				foundNames = append(foundNames, fi.Name())
			}
		} else {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(names, foundNames) {
			t.Errorf("Expected a different list of files:\nwant: %v\n got: %v", names, foundNames)
			t.Error("Log: ", logBuf.String())
			logBuf.Reset()
		}
	}

	// Initial list of potential files
	runCleanOldFiles("foo", "bar")
	touch("foo", "bar", "baz")
	assertFileList("foo", "bar", "baz", ".installed.previous")

	// This should be a no-op, as the list hasn't changed
	runCleanOldFiles("foo", "bar")
	assertFileList("foo", "bar", "baz", ".installed", ".installed.previous")

	// This should be a no-op, as only a file was added
	runCleanOldFiles("foo", "bar", "foo2")
	assertFileList("foo", "bar", "baz", ".installed.previous")

	// "bar" should be removed, foo2 should be ignored as it was never there
	runCleanOldFiles("foo")
	assertFileList("foo", "baz", ".installed.previous")

	// Recreate bar, and create foo2. Ensure that they aren't removed
	touch("bar", "foo2")
	runCleanOldFiles("foo", "baz")
	assertFileList("foo", "bar", "baz", "foo2", ".installed.previous")
}
