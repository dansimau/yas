package fsutil

import (
	"os"
	"time"
)

// Touch is a basic implementation of touch(1):
// https://man7.org/linux/man-pages/man1/touch.1.html
func Touch(path string) error {
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if os.IsNotExist(err) {
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		file.Close()
	} else {
		currentTime := time.Now().Local()
		err = os.Chtimes(path, currentTime, currentTime)
		if err != nil {
			return err
		}
	}

	return nil
}
