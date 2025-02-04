package artifact_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/eks-hybrid/internal/artifact"
)

func TestInstallFile(t *testing.T) {
	srcData := []byte("hello, world!")
	tmp := t.TempDir()
	src := bytes.NewBuffer(srcData)
	dst := filepath.Join(tmp, "file")
	perms := fs.FileMode(0o644)

	err := artifact.InstallFile(dst, src, perms)
	if err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}

	if fi.Mode() != perms {
		t.Fatalf("expected file to have perms %v; found %v", perms, fi.Mode())
	}

	dstData, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}

	if string(srcData) != string(dstData) {
		t.Fatalf("data read doesn't match: %s", dstData)
	}
}

func TestInstallFile_FileExists(t *testing.T) {
	tmp := t.TempDir()
	src := bytes.NewBufferString("hello, world!")
	dst := filepath.Join(tmp, "file")
	perms := fs.FileMode(0o644)

	if err := os.WriteFile(dst, []byte("hello, world!"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := artifact.InstallFile(dst, src, perms)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallFile_DirNotExists(t *testing.T) {
	tmp := t.TempDir()
	src := bytes.NewBufferString("hello, world!")
	dir := filepath.Join(tmp, "nonexistent")
	dst := filepath.Join(dir, "file")
	perms := fs.FileMode(0o644)

	err := artifact.InstallFile(dst, src, perms)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !info.IsDir() {
		t.Fatalf("%v is not a direcftory", dir)
	}

	if info.Mode() != artifact.DefaultDirPerms {
		t.Fatalf("Expected dir with %v permissions; received %v", artifact.DefaultDirPerms, info.Mode())
	}
}
