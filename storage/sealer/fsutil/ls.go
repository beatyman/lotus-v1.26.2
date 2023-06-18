package fsutil

import (
	"io/fs"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type dirEntry struct {
	name string
}

func (d *dirEntry) Name() string {
	return d.name
}
func (d *dirEntry) IsDir() bool {
	panic("TODO")
}
func (d *dirEntry) Type() fs.FileMode {
	panic("TODO")
}
func (d *dirEntry) Info() (fs.FileInfo, error) {
	panic("TODO")
}

func ReadDir(path string) ([]os.DirEntry, error) {
	content, err := exec.Command("ls", path).CombinedOutput()
	if err != nil {
		return nil, err
	}
	names := strings.Split(string(content), "\n")
	dirs := make([]os.DirEntry, len(names))
	index := 0
	for _, name := range names {
		if len(name) == 0 {
			continue
		}
		dirs[index] = &dirEntry{name: name}
		index++
	}

	sort.Slice(dirs[:index], func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	return dirs[:index], nil
}
