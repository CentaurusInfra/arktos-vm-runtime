// Copyright 2016 Ayke van Laethem.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.txt file.

package osfs

import (
	"testing"
)

func TestReadLive(t *testing.T) {
	t.Parallel()
	mounts, err := Read()
	if err != nil {
		t.Error("could not read mount points:", err)
	}

	mount, err := mounts.GetPath(".")
	if err != nil {
		t.Fatal("could not stat the current directory:", err)
	}
	if mount == nil {
		t.Error("could not find mount point for the current directory")
	}

	fs := mount.Filesystem()
	if fs != defaultFilesystem() {
		t.Error("current directory not on the default filesystem")
		if mount != nil {
			t.Errorf("(this may not be an issue if %#v is not a POSIX filesystem)", mount.Type)
		}
	}
}

func TestParseNumber(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		s string
		n uint64
	}{
		{"0", 0},
		{"1", 1},
		{"10", 10},
		{"123", 123},
		{"12382374891237498123742342312412431234123412342342", 0},
		{"abc", 0},
		{"0xff", 0},
		{"012", 12},
		{"-0", 0},
		{"-1", 0},
		{"-10", 0},
	} {
		if n := parseInt(tc.s); uint64(n) != tc.n {
			t.Error("parseInt: expected number %d but got %d for input string %#v", tc.n, n, tc.s)
		}
		if n := parseUint64(tc.s); n != tc.n {
			t.Error("parseUint64: expected number %d but got %d for input string %#v", tc.n, n, tc.s)
		}
	}
}
