package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

func init() {
	RegisterHandle("/delete", deleteHandler)
}

func deleteHandler(w http.ResponseWriter, r *http.Request) error {
	sectors := r.FormValue("sectors")
	if len(sectors) == 0 {
		return writeMsg(w, 404, "sector not found")
	}
	sids := strings.Split(sectors, ",")

	for _, repo := range repos {
		func() {
			// stop protect.
			if err := chattrUnlock(filepath.Join(repo, "cache")); err != nil {
				log.Warn(errors.As(err))
			}
			if err := chattrUnlock(filepath.Join(repo, "sealed")); err != nil {
				log.Warn(errors.As(err))
			}
			if err := chattrUnlock(filepath.Join(repo, "unsealed")); err != nil {
				log.Warn(errors.As(err))
			}
			defer func() {
				if err := chattrLock(filepath.Join(repo, "cache")); err != nil {
					log.Warn(errors.As(err))
				}
				if err := chattrLock(filepath.Join(repo, "sealed")); err != nil {
					log.Warn(errors.As(err))
				}
				if err := chattrLock(filepath.Join(repo, "unsealed")); err != nil {
					log.Warn(errors.As(err))
				}
			}()

			for _, sid := range sids {
				cacheFile := filepath.Join(repo, "cache", sid)
				sealedFile := filepath.Join(repo, "sealed", sid)
				unsealedFile := filepath.Join(repo, "unsealed", sid)

				if err := os.RemoveAll(cacheFile); err != nil {
					if !os.IsNotExist(err) {
						log.Warn(errors.As(err))
						return
					}
				} else {
					log.Warnf("delete file:%s", cacheFile)
				}
				if err := os.Remove(sealedFile); err != nil {
					if !os.IsNotExist(err) {
						log.Warn(errors.As(err))
						return
					}
				} else {
					log.Warnf("delete file:%s", sealedFile)
				}
				if err := os.Remove(unsealedFile); err != nil {
					if !os.IsNotExist(err) {
						log.Warn(errors.As(err))
						return
					}
				} else {
					log.Warnf("delete file:%s", unsealedFile)
				}
			}
		}()
	}
	return writeMsg(w, 200, "OK")
}
