package storage

import (
	"io"
	"os"
)

func CopyFile(src *os.File, dst *os.File) (int64, error) {
	return io.Copy(dst, src)
}
