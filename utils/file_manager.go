package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type FileInfo struct {
	Path   string
	Length int64
	Offset int64
}

type FileManager struct {
	Files         []FileInfo
	BaseDir       string
	Mu            sync.Mutex
	downloadsPath string
}

func NewFileManager(fileData []any, baseDir string) *FileManager {
	var currentOffset int64 = 0
	var files []FileInfo

	for _, f := range fileData {
		fileMap, ok := f.(map[string]any)
		rawPath, ok := fileMap["path"].([]interface{})
		if !ok {
			fmt.Printf("Error: 'path' is missing or not an array for file: %v\n", fileMap)
			continue
		}

		pathParts := make([]string, len(rawPath))
		for i, v := range rawPath {
			pathParts[i] = v.(string)
		}

		finalPath := filepath.Join(pathParts...)
		info := FileInfo{
			Path:   finalPath,
			Length: fileMap["length"].(int64),
			Offset: currentOffset, // This file starts where the last one ended
		}
		files = append(files, info)

		currentOffset += info.Length
	}

	return &FileManager{
		Files:   files,
		BaseDir: baseDir,
	}
}

func (fm *FileManager) WritePiece(pieceIndex int, pieceLength int64, data []byte) error {
	fm.Mu.Lock()
	defer fm.Mu.Unlock()

	globalStart := int64(pieceIndex) * pieceLength
	globalEnd := globalStart + int64(len(data))

	for _, file := range fm.Files {
		fileEnd := file.Offset + file.Length

		if globalStart < fileEnd && globalEnd > file.Offset {
			writeStart := max(globalStart, file.Offset)
			writeEnd := min(globalEnd, fileEnd)

			dataOffset := writeStart - globalStart
			fileOffset := writeStart - file.Offset
			bytesToWrite := writeEnd - writeStart

			err := fm.writeToDisk(file.Path, data[dataOffset:dataOffset+bytesToWrite], fileOffset)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (fm *FileManager) writeToDisk(path string, data []byte, offset int64) error {
	fullPath := filepath.Join(fm.BaseDir, path)
	fmt.Printf("Writing to disk at %s (offset %d, length %d)\n", fullPath, offset, len(data))

	// Ensure the folder exists
	os.MkdirAll(filepath.Dir(fullPath), 0755)

	// Open with Read/Write permissions
	f, err := os.OpenFile(fullPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteAt(data, offset)
	return err
}

func (fm *FileManager) ReadPiece(index int, pieceLength int64, begin int64) ([]byte, error) {
	pieceStart := int64(index)*pieceLength + begin
	pieceEnd := pieceStart + pieceLength

	pieceData := make([]byte, 0, pieceLength)

	var currentOffset int64 = 0
	for _, f := range fm.Files {
		fileStart := currentOffset
		fileEnd := currentOffset + f.Length

		if pieceStart < fileEnd && pieceEnd > fileStart {
			readStart := max(pieceStart, fileStart) - fileStart
			readEnd := min(pieceEnd, fileEnd) - fileStart

			fPath := filepath.Join(fm.BaseDir, f.Path)

			data, err := fm.readFileRange(fPath, readStart, readEnd-readStart)
			if err != nil {
				return nil, err
			}
			pieceData = append(pieceData, data...)
		}
		currentOffset += f.Length
	}
	return pieceData, nil
}

func (fm *FileManager) readFileRange(path string, offset int64, length int64) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	buf := make([]byte, length)
	_, err = file.ReadAt(buf, offset)
	return buf, err
}
