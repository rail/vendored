// Copyright 2019 The LevelDB-Go and Pebble Authors. All rights reserved. Use
// of this source code is governed by a BSD-style license that can be found in
// the LICENSE file.

package base

import (
	"sync"
	"time"

	"github.com/cockroachdb/pebble/vfs"
)

// Cleaner cleans obsolete files.
type Cleaner interface {
	Clean(fs vfs.FS, fileType FileType, path string) error
}

// NeedsFileContents is implemented by a cleaner that needs the contents of the
// files that it is being asked to clean.
type NeedsFileContents interface {
	needsFileContents()
}

// DeleteCleaner deletes file.
type DeleteCleaner struct{}

// Clean removes file.
func (DeleteCleaner) Clean(fs vfs.FS, fileType FileType, path string) error {
	return fs.Remove(path)
}

func (DeleteCleaner) String() string {
	return "delete"
}

// ArchiveCleaner archives file instead delete.
type ArchiveCleaner struct{}

var _ NeedsFileContents = ArchiveCleaner{}

// Clean archives file.
func (ArchiveCleaner) Clean(fs vfs.FS, fileType FileType, path string) error {
	switch fileType {
	case FileTypeLog, FileTypeManifest, FileTypeTable:
		destDir := fs.PathJoin(fs.PathDir(path), "archive")

		if err := fs.MkdirAll(destDir, 0755); err != nil {
			return err
		}

		destPath := fs.PathJoin(destDir, fs.PathBase(path))
		return fs.Rename(path, destPath)

	default:
		return fs.Remove(path)
	}
}

func (ArchiveCleaner) String() string {
	return "archive"
}

func (ArchiveCleaner) needsFileContents() {
}

// DelayedCleaner deletes files but at a delay.
type DelayedCleaner struct {
	Dur time.Duration

	mu     sync.Mutex
	last   time.Time
	delete []string
	accum  []string
}

var _ NeedsFileContents = DelayedCleaner{}

// Clean deletes the file, at a delay.
func (d DelayedCleaner) Clean(fs vfs.FS, fileType FileType, path string) error {
	switch fileType {
	case FileTypeLog, FileTypeManifest, FileTypeTable:
		d.mu.Lock()
		defer d.mu.Unlock()

		d.accum = append(d.accum, path)

		// If it's been d.Dur since we deleted files, move `accum` to
		// `delete`, and asynchronously remove all of the files that used to
		// be in `delete`. This negates deletion pacing but that's okay for
		// debugging.
		if time.Now().After(d.last.Add(d.Dur)) {
			d.last = time.Now()
			del := d.delete
			d.delete = d.accum
			d.accum = make([]string, 0, len(d.delete))
			go func() {
				for _, f := range del {
					_ = fs.Remove(f)
				}
			}()
		}
		return nil

	default:
		return fs.Remove(path)
	}
}

func (DelayedCleaner) String() string {
	return "delayed"
}

func (DelayedCleaner) needsFileContents() {
	// turn off WAL-recycling
}
