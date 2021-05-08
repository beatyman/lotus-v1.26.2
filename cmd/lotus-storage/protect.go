package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/gwaylib/errors"
)

func chattrLock(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.As(err)
	}
	out, err := exec.Command("chattr", "+a", "-R", path).CombinedOutput()
	if err != nil {
		return errors.As(err, string(out))
	}
	return nil
}
func chattrUnlock(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.As(err)
	}
	out, err := exec.Command("chattr", "-a", "-R", path).CombinedOutput()
	if err != nil {
		return errors.As(err, string(out))
	}
	return nil
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
				log.Info("create file:", event.Name)
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
