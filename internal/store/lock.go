package store

import (
	"context"
	"sync"
	"time"
)

// Acquire obtains an OS-owned exclusive lock. Keep the file after release so
// other processes never contend on different inodes for the same lock path.
func Acquire(ctx context.Context, lockPath string) (func() error, error) {
	if ctx == nil {
		return nil, errPrivate
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !safeTarget(lockPath) {
		return nil, errPrivate
	}
	file, err := nativeOpen(lockPath, false)
	if err != nil {
		return nil, errPrivate
	}
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		acquired, err := nativeTryLock(file)
		if err != nil {
			file.Close()
			return nil, errPrivate
		}
		if acquired {
			var once sync.Once
			var releaseErr error
			return func() error {
				once.Do(func() {
					unlockErr := nativeUnlock(file)
					closeErr := file.Close()
					if unlockErr != nil || closeErr != nil {
						releaseErr = errPrivate
					}
				})
				return releaseErr
			}, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
