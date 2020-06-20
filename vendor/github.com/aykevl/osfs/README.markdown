# Request filesystem capabilities

This is a small package that can determine on what kind of filesystem a file
resides. On Linux, it reads this information from `/proc/self/mountinfo`.

Example:

```go
package main

import (
	"log"
	"os"

	"github.com/aykevl/osfs"
)

func main() {
	path := "/some/file"

	// Get the os.FileInfo of any file or directory.
	st, err := os.Stat(path)
	if err != nil {
		log.Fatal("could not stat file:", err)
	}

	// Ignoring the error is valid and will not crash your program.
	// But you can, for example, print a warning that something went wrong.
	info, _ := osfs.Read()
	mount := info.Get(path, st)

	if mount != nil {
		log.Print("Filesystem root:", mount.Root)
	}

	// It is valid to call Filesystem() on a nil mount: it will return the
	// capabilities of the default or most common filesystem on your OS.
	if mount.Filesystem().Hardlink {
		log.Print("This filesystem supports hardlinks!")
	}
}
```

Documentation:
[![GoDoc](https://godoc.org/github.com/aykevl/osfs?status.svg)](https://godoc.org/github.com/aykevl/osfs)

The package is designed to be easy to use and resilient to unknown systems. It
does what looks like the best possible action.

Supported systems:

  * For the time being, only **Linux** is supported. It will probably work on
    other systems, but will act like filesystems support nothing.
