package utils

import (
	"os"
	"time"
)

type ServerFileStat struct {
	FileName    string
	IsDirFile   bool
	FileSize    int64
	FileModTime time.Time
}

// base name of the file
func (s *ServerFileStat) Name() string {
	return s.FileName
}

// length in bytes for regular files; system-dependent for others
func (s *ServerFileStat) Size() int64 {
	return s.FileSize
}

// file mode bits
func (s *ServerFileStat) Mode() os.FileMode {
	if s.IsDirFile {
		return 0755
	}
	return 0644
}

// modification time
func (s *ServerFileStat) ModTime() time.Time {
	return s.FileModTime
}

// abbreviation for Mode().IsDir()
func (s *ServerFileStat) IsDir() bool {
	return s.IsDirFile
}

// underlying data source (can return nil)
func (s *ServerFileStat) Sys() interface{} {
	return nil
}
