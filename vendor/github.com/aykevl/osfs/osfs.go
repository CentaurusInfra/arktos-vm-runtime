// Copyright 2016 Ayke van Laethem.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.txt file.

// Package osfs determines filesystem capabilities based on os.FileInfo.
package osfs

import (
	"os"
	"path/filepath"
	"strconv"
)

// Default has the filesystem capabilities of the common filesystem(s) on the
// current OS.
var Default = defaultFilesystem()

// Filesystem contains capabilities of one filesystem. The zero value means it
// has no special features (and is not a typical POSIX filesystem).
type Filesystem struct {
	Permissions os.FileMode // supported permissions are set (e.g. 0777 on Linux)
	Symlink     bool
	Hardlink    bool
	Inode       bool
	Memory      bool // true if this is an in-memory filesystem (tmpfs)
	Special     bool // true if this is a filesystem like /proc
}

// MountPoint contains information on one mount point, for example one line of
// /proc/self/mountinfo.
//
// All fields have the zero value if they are unknown (e.g. ints are 0). Only
// Root is required to be set (when Type is empty, it is the default
// filesystem).
type MountPoint struct {
	devNumber uint64 // st_dev field (for now Linux-specific)
	FSRoot    string // root of the mount within the filesystem
	Root      string // mount point relative to the process's root
	Type      string // filesystem type, e.g. "ext4"
}

// Info lists all filesystems on the current system. A specific filesystem can
// be fetched using Get().
type Info struct {
	mountPaths   map[string]*MountPoint
	mountNumbers map[uint64]*MountPoint
}

// Len returns the number of mount points found.
func (info *Info) Len() int {
	// mountPaths and mountNumbers must be the same length.
	return len(info.mountPaths)
}

// GetPath returns the mount point based on a path. It does a os.Stat and
// Info.Get on the file. It is a shorthand for Get(path, stat).
func (info *Info) GetPath(path string) (*MountPoint, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return info.Get(path, st)
}

// Get returns the mount point based on a path and stat result. The path does
// not have to be absolute and may contain symlinks.
func (info *Info) Get(path string, fileInfo os.FileInfo) (*MountPoint, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	return info.GetReal(path, fileInfo), nil
}

// parseInt returns the positive parsed number if parsing succeeded, or 0 if it
// failed. It is a helper to parse the mountinfo file.
func parseInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// parseUint64 is similar to parseInt, but returns an uint64.
func parseUint64(s string) uint64 {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// Filesystem returns capabilities of the filesystem for this mount point. The
// results are more like an educated guess, but should give correct results for
// the vast majority of detected filesystems. It has a reasonable default (e.g.
// on Linux a standard POSIX filesystem with hardlinks and inodes).
func (p *MountPoint) Filesystem() Filesystem {
	fs := defaultFilesystem()
	if p == nil {
		return fs
	}
	switch p.Type {
	case "ext2", "ext3", "ext4", "btrfs":
		// These are regular Linux filesystems. Don't change the defaults.
	case "sysfs", "proc", "devtmpfs":
		// These are special filesystems, namely /sys, /proc and /dev.
		fs.Memory = true
		fs.Special = true
	case "vfat":
		// FAT filesystems support basically nothing interesting.
		fs = Filesystem{}
	case "fuseblk":
		// This is a difficult one. Many different types of filesystems can be
		// behind FUSE.
		// I am not sure whether NTFS (a comman FUSE filesystem) uses stable
		// inode numbers (stable across reboots etc.). Until that's verified,
		// set it to false.
		// TODO: get the filesystem type from something like
		// /run/blkid/blkid.tab?
		fs.Inode = false
	case "tmpfs":
		// tmpfs has all the benefits of a POSIX filesystem, but is implemented
		// in memory.
		fs.Memory = true
	case "fuse.sshfs":
		// We don't know what kind of filesystem the other host has, but it's
		// most likely a POSIX system, so keep the defaults.
	}
	return fs
}
