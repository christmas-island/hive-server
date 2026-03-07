//go:build !nolog

// Package log provides logging functionality.
package log

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// lock supports multithreaded/concurrent logging
var lock sync.Mutex

// level is the current log level... 0 = trace, 1 = debug, 2 = info, 3 = error
var level uint8

// writer is the current logging destination
var writer io.Writer

// orig is the original logging destination
var orig []io.Writer

func init() {
	// Set the default log level to trace
	level = 0

	// Set the default logging destination to stdout
	writer = os.Stdout

	// Initialize the stack of original writers
	orig = make([]io.Writer, 0, 64)
}

// log is the main logging function
func log(l uint8, v ...any) {
	if l >= level {
		lock.Lock()
		defer lock.Unlock()

		fmt.Fprint(writer, time.Now().UTC().Format("2006-01-02T15:04:05.002Z"), " ")
		fmt.Fprint(writer, GetLevelName(), ": ")
		fmt.Fprint(writer, v...)
		fmt.Fprint(writer, "\n")
	}
}

// To is a convenience function for readability that is a wrapper around
// [SetWriter].
func To(w io.Writer) func() {
	// Set the current logging destination
	return SetWriter(w)
}

// Reset is a helper that clears the stack of original writers and resets the
// writer to the original value.
func Reset() {
	lock.Lock()
	defer lock.Unlock()
	if len(orig) > 0 {
		writer = orig[0]
	} else {
		writer = os.Stdout
	}
	orig = make([]io.Writer, 0, 64)
}

// SetWriter sets the current logging destination, and returns a function to
// reset the writer.
func SetWriter(w io.Writer) func() {
	lock.Lock()
	defer lock.Unlock()

	// Check if the writer is the same as the current writer and skip it if so.
	if writer == w {
		return func() {}
	}

	// This will panic if the stack is full, but that's okay, it should never be
	// used that way.
	orig = append(orig, writer)
	writer = w

	// Return a function to reset the writer
	return func() {
		// Take the lock
		lock.Lock()
		defer lock.Unlock()

		// Iterate through the stack and remove 'w'
		for i, o := range orig {
			if o == w {
				orig = append(orig[:i], orig[i+1:]...)
				break
			}
		}

		// Reset the writer to whatever's last on the stack
		if len(orig) > 0 {
			writer = orig[len(orig)-1]
			orig = orig[:len(orig)-1]
		} else {
			// Or do a full reset, which probably should never happen, but at
			// least it will ensure that we don't end up in a bad state
			Reset()
		}
	}
}

// GetLevel returns the current log level
func GetLevel() int { return int(level) }

// GetLevelName returns the current log level as a string
func GetLevelName() string {
	switch level {
	case 0:
		return "trace"
	case 1:
		return "debug"
	case 2:
		return " info"
	case 3:
		return "error"
	case 4:
		return "  off"
	default:
		return "unknw"
	}
}

// SetLevel sets the current log level
func SetLevel(l any) {
	v := fmt.Sprint(l)

	lock.Lock()
	defer lock.Unlock()
	switch v {
	case "trace":
		level = 0
	case "debug":
		level = 1
	case "info":
		level = 2
	case "error":
		level = 3
	case "off":
		level = 4
	case "nolog":
		level = 4
	case "0":
		level = 0
	case "1":
		level = 1
	case "2":
		level = 2
	case "3":
		level = 3
	case "4":
		level = 4
	default:
		panic("invalid log level")
	}
}

func Trace(v ...any) { log(0, v...) }
func Debug(v ...any) { log(1, v...) }
func Info(v ...any)  { log(2, v...) }
func Error(v ...any) { log(3, v...) }
