package util

import (
	"io"
	"io/fs"
	"os"
)

func ReadDirPartial(path string) ([]fs.DirEntry, error) {
	dir, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	var entries []fs.DirEntry
	for {
		batch, err := dir.ReadDir(128)
		entries = append(entries, batch...)
		if err == io.EOF {
			return entries, nil
		}
		if err != nil {
			return entries, err
		}
	}
}
