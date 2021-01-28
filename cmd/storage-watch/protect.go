package main

import (
	"os/exec"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

func chattrLock(path string) error {
	return exec.Command("chattr", "+a", "-R", path).Run()
}
func chattrUnlock(path string) error {
	return exec.Command("chattr", "-a", "-R", path).Run()
}

func protectPath(paths []string) error {
	for _, p := range paths {
		if err := chattrLock(filepath.Join(p, "cache")); err != nil {
			return errors.As(err, p)
		}
		if err := chattrLock(filepath.Join(p, "sealed")); err != nil {
			return errors.As(err, p)
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.As(err, paths)
	}
	defer watcher.Close()

	done := make(chan error, 1)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					done <- errors.New("channel closed")
					return
				}
				//log.Println("event:", event)
				if event.Op&fsnotify.Create != fsnotify.Create {
					continue
				}
				log.Println("create file:", event.Name)
				// TODO: protect
				if err := chattrLock(event.Name); err != nil {
					done <- errors.As(err)
					return
				}
				continue
			case err, ok := <-watcher.Errors:
				if !ok {
					done <- errors.New("channel closed")
					return
				}
				done <- errors.As(err)
				return
			}
		}
	}()

	for _, p := range paths {
		if err := watcher.Add(filepath.Join(p, "cache")); err != nil {
			return errors.As(err)
		}
		if err := watcher.Add(filepath.Join(p, "sealed")); err != nil {
			return errors.As(err)
		}
	}

	// watching
	return <-done
}
