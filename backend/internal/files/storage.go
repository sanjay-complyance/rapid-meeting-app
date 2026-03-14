package files

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type SavedFile struct {
	RelativePath string
	AbsolutePath string
	SizeBytes    int64
}

type LocalStorage struct {
	root string
}

func NewLocalStorage(root string) *LocalStorage {
	return &LocalStorage{root: root}
}

func (s *LocalStorage) SaveUpload(ctx context.Context, meetingID, filename string, reader io.Reader) (SavedFile, error) {
	safeName := sanitizeFilename(filename)
	relative := filepath.Join("uploads", meetingID, safeName)
	absolute := filepath.Join(s.root, relative)

	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return SavedFile{}, fmt.Errorf("create upload directory: %w", err)
	}

	file, err := os.Create(absolute)
	if err != nil {
		return SavedFile{}, fmt.Errorf("create upload file: %w", err)
	}
	defer file.Close()

	size, err := copyWithContext(ctx, file, reader)
	if err != nil {
		return SavedFile{}, fmt.Errorf("save upload: %w", err)
	}

	return SavedFile{
		RelativePath: filepath.ToSlash(relative),
		AbsolutePath: absolute,
		SizeBytes:    size,
	}, nil
}

func (s *LocalStorage) AbsolutePath(relative string) string {
	return filepath.Join(s.root, filepath.FromSlash(relative))
}

func sanitizeFilename(filename string) string {
	filename = filepath.Base(strings.TrimSpace(filename))
	filename = strings.ReplaceAll(filename, " ", "_")
	if filename == "." || filename == "" || filename == "/" {
		return "upload.bin"
	}
	return filename
}

func copyWithContext(ctx context.Context, writer io.Writer, reader io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		readBytes, readErr := reader.Read(buffer)
		if readBytes > 0 {
			written, writeErr := writer.Write(buffer[:readBytes])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}
