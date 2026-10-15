package cfgadapter

import (
	"errors"
	"os"
	"path/filepath"
)

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmpPath, perm)
	}
	if err == nil {
		err = os.Rename(tmpPath, path)
	}
	if err != nil {
		return errors.Join(err, os.Remove(tmpPath))
	}
	return nil
}
