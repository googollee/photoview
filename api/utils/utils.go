package utils

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"path"
	"path/filepath"
)

func GenerateToken() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const length = 8

	b := make([]byte, length)
	for i := range b {
		n := rand.Intn(len(charset))
		b[i] = charset[n]
	}
	return string(b)
}

type PhotoviewError struct {
	message  string
	original error
}

func (e PhotoviewError) Error() string {
	return fmt.Sprintf("%s: %s", e.message, e.original)
}

func HandleError(message string, err error) PhotoviewError {
	log.Printf("ERROR: %s: %s", message, err)
	return PhotoviewError{
		message:  message,
		original: err,
	}
}

var test_face_recognition_models_path string = ""

func ConfigureTestFaceRecognitionModelsPath(path string) {
	test_face_recognition_models_path = path
}

func FaceRecognitionModelsPath() string {
	if test_face_recognition_models_path != "" {
		return test_face_recognition_models_path
	}

	if EnvFaceRecognitionModelsPath.GetValue() == "" {
		return path.Join("data", "models")
	}

	return EnvFaceRecognitionModelsPath.GetValue()
}

// IsDirSymlink checks that the given path is a symlink and resolves to a
// directory.
func IsDirSymlink(linkPath string) (bool, error) {

	fileInfo, err := os.Lstat(linkPath)
	if err != nil {
		return false, fmt.Errorf("cannot get fileinfo of the symlink %q: %w", linkPath, err)
	}

	// Resolve symlinks
	if fileInfo.Mode()&os.ModeSymlink == os.ModeSymlink {
		resolvedPath, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			return false, fmt.Errorf("cannot resolve symlink target for %q, skipping it: %w", linkPath, err)
		}

		resolvedFile, err := os.Stat(resolvedPath)
		if err != nil {
			return false, fmt.Errorf("cannot get fileinfo of the symlink %q target %q, skipping it: %w",
				linkPath, resolvedPath, err)
		}

		return resolvedFile.IsDir(), nil
	}

	return false, nil
}
