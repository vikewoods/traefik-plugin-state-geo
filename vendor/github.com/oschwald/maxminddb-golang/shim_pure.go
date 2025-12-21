package maxminddb

import (
	"fmt"
	"os"
)

// Open opens the MaxMind DB file. Since mmap is disabled, it reads the entire
// file into memory using standard OS calls.
func Open(file string) (*Reader, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return FromBytes(data)
}

func mmap(fd int, length int) ([]byte, error) {
	return nil, fmt.Errorf("mmap not supported in this environment")
}

func munmap(b []byte) error {
	return nil
}

// These allow the Reader to compile without reader_mmap.go
func (r *Reader) closeMmap() error {
	return nil
}
