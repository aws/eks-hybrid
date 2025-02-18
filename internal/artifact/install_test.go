package artifact_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"

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

func TestInstallPackageWithRetries(t *testing.T) {
	testCases := []struct {
		name    string
		source  artifact.Package
		wantErr string
	}{
		{
			name: "happy path",
			source: artifact.NewPackageSource(
				artifact.NewCmd("echo", "hello"),
				artifact.Cmd{},
				artifact.Cmd{},
			),
		},
		{
			name: "error",
			source: artifact.NewPackageSource(
				artifact.NewCmd("fake-command", "1"),
				artifact.Cmd{},
				artifact.Cmd{},
			),
			wantErr: `running command [fake-command 1]:  [Err exec: "fake-command": executable file not found in $PATH]`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
			defer cancel()

			err := artifact.InstallPackageWithRetries(ctx, tc.source, 1*time.Millisecond)
			if tc.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tc.wantErr)))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestInstallPackageWithRetriesSuccessAfterFailure(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	source := &dynamicSource{}
	source.SetInstallCmd(artifact.NewCmd("fake-command"))

	go func() {
		time.Sleep(60 * time.Millisecond)
		source.SetInstallCmd(artifact.NewCmd("echo", "hello"))
	}()

	g.Expect(artifact.InstallPackageWithRetries(ctx, source, 1*time.Millisecond)).To(Succeed())
}

func TestUpgradePackageWithRetriesSuccessAfterFailure(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	source := &dynamicSource{}
	source.SetUpgradeCmd(artifact.NewCmd("fake-command"))

	go func() {
		time.Sleep(60 * time.Millisecond)
		source.SetUpgradeCmd(artifact.NewCmd("echo", "hello"))
	}()

	g.Expect(artifact.UpgradePackageWithRetries(ctx, source, 1*time.Millisecond)).To(Succeed())
}

type dynamicSource struct {
	sync.RWMutex
	installCmd   artifact.Cmd
	uninstallCmd artifact.Cmd
	upgradeCmd   artifact.Cmd
}

var _ artifact.Package = &dynamicSource{}

func (f *dynamicSource) InstallCmd(ctx context.Context) *exec.Cmd {
	f.RLock()
	defer f.RUnlock()
	return f.installCmd.Command(ctx)
}

func (f *dynamicSource) UninstallCmd(ctx context.Context) *exec.Cmd {
	f.RLock()
	defer f.RUnlock()
	return f.uninstallCmd.Command(ctx)
}

func (f *dynamicSource) UpgradeCmd(ctx context.Context) *exec.Cmd {
	f.RLock()
	defer f.RUnlock()
	return f.upgradeCmd.Command(ctx)
}

func (f *dynamicSource) SetInstallCmd(cmd artifact.Cmd) {
	f.Lock()
	defer f.Unlock()
	f.installCmd = cmd
}

func (f *dynamicSource) SetUninstallCmd(cmd artifact.Cmd) {
	f.Lock()
	defer f.Unlock()
	f.uninstallCmd = cmd
}

func (f *dynamicSource) SetUpgradeCmd(cmd artifact.Cmd) {
	f.Lock()
	defer f.Unlock()
	f.upgradeCmd = cmd
}
