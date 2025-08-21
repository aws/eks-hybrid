package artifact

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// DefaultDirPerms are the permissions assigned to a directory when an Install* func is called
// and it has to create the parent directories for the destination.
const DefaultDirPerms = fs.ModeDir | 0o755

// InstallFile installs src to dst with perms permissions. It ensures any base paths exist
// before installing.
func InstallFile(dst string, src io.Reader, perms fs.FileMode) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(path.Dir(dst), DefaultDirPerms); err != nil {
		return err
	}

	fh, err := os.OpenFile(dst, os.O_CREATE|os.O_RDWR|os.O_TRUNC, perms)
	if err != nil {
		return err
	}
	defer fh.Close()

	_, err = io.Copy(fh, src)
	return err
}

// InstallFileWithLogging installs src to dst with perms permissions and provides detailed logging.
// It ensures any base paths exist before installing.
func InstallFileWithLogging(dst string, src io.Reader, perms fs.FileMode, logger *zap.Logger) error {

	// Check if /etc/passwd file is present
	passwdFile := "/etc/passwd"
	if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
		logger.Warn("Before /etc/passwd file does not exist", zap.String("path", passwdFile))
	} else if err != nil {
		logger.Error("Before Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
	} else {
		logger.Info("Before /etc/passwd file is present", zap.String("path", passwdFile))
	}

	logger.Info("Installing file", zap.String("destination", dst))

	logger.Debug("Removing existing file if present", zap.String("path", dst))
	if err := os.RemoveAll(dst); err != nil {
		logger.Error("Failed to remove existing file", zap.String("path", dst), zap.Error(err))
		return err
	}

	if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
		logger.Warn("After /etc/passwd file does not exist", zap.String("path", passwdFile))
	} else if err != nil {
		logger.Error("After Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
	} else {
		logger.Info("After /etc/passwd file is present", zap.String("path", passwdFile))
	}

	parentDir := path.Dir(dst)
	logger.Debug("Creating parent directories", zap.String("path", parentDir))
	if err := os.MkdirAll(parentDir, DefaultDirPerms); err != nil {
		logger.Error("Failed to create parent directories", zap.String("path", parentDir), zap.Error(err))
		return err
	}

	logger.Debug("Creating destination file", zap.String("path", dst))
	fh, err := os.OpenFile(dst, os.O_CREATE|os.O_RDWR|os.O_TRUNC, perms)
	if err != nil {
		logger.Error("Failed to create destination file", zap.String("path", dst), zap.Error(err))
		return err
	}
	defer fh.Close()

	logger.Debug("Copying content to destination file", zap.String("path", dst))
	_, err = io.Copy(fh, src)
	if err != nil {
		logger.Error("Failed to copy content to destination file", zap.String("path", dst), zap.Error(err))
		return err
	}

	logger.Info("Successfully installed file", zap.String("destination", dst))
	return nil
}

// InstallTarGz untars the src file into the dst directory and deletes the src tgz file
func InstallTarGz(dst, src string) error {
	if err := os.MkdirAll(dst, DefaultDirPerms); err != nil {
		return err
	}
	reader, err := os.Open(src)
	if err != nil {
		return errors.Wrap(err, "opening source file")
	}
	defer reader.Close()
	gzr, err := gzip.NewReader(reader)
	if err != nil {
		return errors.Wrap(err, "creating gzip reader")
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			// no more files
			break
		} else if err != nil {
			return errors.Wrap(err, "reading tar file")
		} else if header == nil {
			continue
		}

		if !validRelPath(header.Name) {
			return fmt.Errorf("tar contained invalid name error %q", header.Name)
		}

		target := filepath.Join(dst, header.Name)
		info := header.FileInfo()
		if info.IsDir() {
			if err := os.MkdirAll(target, info.Mode()); err != nil {
				return errors.Wrap(err, "creating directory")
			}
			continue
		}

		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return errors.Wrap(err, "creating file")
		}
		defer f.Close()

		if _, err := io.Copy(f, tr); err != nil {
			return errors.Wrap(err, "copying file contents")
		}
	}

	// Remove the tgz file
	if err := os.Remove(src); err != nil {
		return errors.Wrap(err, "removing source file")
	}
	return nil
}

func validRelPath(p string) bool {
	if p == "" || strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.Contains(p, "../") {
		return false
	}
	return true
}
