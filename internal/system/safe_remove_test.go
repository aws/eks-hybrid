package system

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	mountutils "k8s.io/mount-utils"
)

// MockMounter is a mock implementation of mount.Interface for testing
type MockMounter struct {
	mock.Mock
}

func (m *MockMounter) Mount(source string, target string, fstype string, options []string) error {
	args := m.Called(source, target, fstype, options)
	return args.Error(0)
}

func (m *MockMounter) MountSensitive(source string, target string, fstype string, options []string, sensitiveOptions []string) error {
	args := m.Called(source, target, fstype, options, sensitiveOptions)
	return args.Error(0)
}

func (m *MockMounter) MountSensitiveWithoutSystemd(source string, target string, fstype string, options []string, sensitiveOptions []string) error {
	args := m.Called(source, target, fstype, options, sensitiveOptions)
	return args.Error(0)
}

func (m *MockMounter) MountSensitiveWithoutSystemdWithMountFlags(source string, target string, fstype string, options []string, sensitiveOptions []string, mountFlags []string) error {
	args := m.Called(source, target, fstype, options, sensitiveOptions, mountFlags)
	return args.Error(0)
}

func (m *MockMounter) Unmount(target string) error {
	args := m.Called(target)
	return args.Error(0)
}

func (m *MockMounter) List() ([]mountutils.MountPoint, error) {
	args := m.Called()
	return args.Get(0).([]mountutils.MountPoint), args.Error(1)
}

func (m *MockMounter) IsMountPoint(file string) (bool, error) {
	args := m.Called(file)
	return args.Bool(0), args.Error(1)
}

func (m *MockMounter) IsLikelyNotMountPoint(file string) (bool, error) {
	args := m.Called(file)
	return args.Bool(0), args.Error(1)
}

func (m *MockMounter) GetMountRefs(pathname string) ([]string, error) {
	args := m.Called(pathname)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockMounter) CanSafelySkipMountPointCheck() bool {
	args := m.Called()
	return args.Bool(0)
}

func TestNewSafeRemover(t *testing.T) {
	remover := NewSafeRemover()
	assert.NotNil(t, remover)
	assert.NotNil(t, remover.mounter)
}

func TestSafeRemoveAll_Function(t *testing.T) {
	// Test the package-level function
	tempDir, err := os.MkdirTemp("", "safe-remove-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Should remove successfully when no mount points
	err = SafeRemoveAll(tempDir, false, false)
	assert.NoError(t, err)
	assert.NoFileExists(t, tempDir)
}

func TestSafeRemover_SafeRemoveAll_NoMountPoints(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	tempDir, err := os.MkdirTemp("", "safe-remove-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test structure
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	subDir := filepath.Join(tempDir, "subdir")
	err = os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	// Mock: no mount points found
	mockMounter.On("IsMountPoint", mock.AnythingOfType("string")).Return(false, nil)

	err = remover.SafeRemoveAll(tempDir, false, false)
	assert.NoError(t, err)
	assert.NoFileExists(t, tempDir)

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_SafeRemoveAll_WithMountPoints_DisallowUnmount(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	tempDir, err := os.MkdirTemp("", "safe-remove-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	mountDir := filepath.Join(tempDir, "mounted")
	err = os.MkdirAll(mountDir, 0755)
	require.NoError(t, err)

	// Mock: mount point found, unmount not allowed
	mockMounter.On("IsMountPoint", tempDir).Return(false, nil)
	mockMounter.On("IsMountPoint", mountDir).Return(true, nil)

	err = remover.SafeRemoveAll(tempDir, false, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "contains 1 mount points")
	assert.Contains(t, err.Error(), "mount points detected")

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_SafeRemoveAll_WithMountPoints_AllowUnmount_Success(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	tempDir, err := os.MkdirTemp("", "safe-remove-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	mountDir := filepath.Join(tempDir, "mounted")
	err = os.MkdirAll(mountDir, 0755)
	require.NoError(t, err)

	// Mock: mount point found during discovery
	mockMounter.On("IsMountPoint", tempDir).Return(false, nil).Once()
	mockMounter.On("IsMountPoint", mountDir).Return(true, nil).Once()
	// Mock: unmount succeeds
	mockMounter.On("Unmount", mountDir).Return(nil).Once()
	// Mock: verification shows mount point is gone
	mockMounter.On("IsMountPoint", mountDir).Return(false, nil).Once()

	err = remover.SafeRemoveAll(tempDir, true, false)
	assert.NoError(t, err)
	assert.NoFileExists(t, tempDir)

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_SafeRemoveAll_WithMountPoints_UnmountFails(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	tempDir, err := os.MkdirTemp("", "safe-remove-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	mountDir := filepath.Join(tempDir, "mounted")
	err = os.MkdirAll(mountDir, 0755)
	require.NoError(t, err)

	// Mock: mount point found, unmount fails
	mockMounter.On("IsMountPoint", tempDir).Return(false, nil)
	mockMounter.On("IsMountPoint", mountDir).Return(true, nil)
	mockMounter.On("Unmount", mountDir).Return(fmt.Errorf("unmount failed")).Times(3) // 3 retries

	err = remover.SafeRemoveAll(tempDir, true, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmount")

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_SafeRemoveAll_PlatformNotSupported(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	tempDir, err := os.MkdirTemp("", "safe-remove-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Mock: platform not supported error
	mockMounter.On("IsMountPoint", mock.AnythingOfType("string")).Return(false, fmt.Errorf("util/mount on this platform is not supported"))

	err = remover.SafeRemoveAll(tempDir, false, false)
	assert.NoError(t, err) // Should fallback to os.RemoveAll
	assert.NoFileExists(t, tempDir)

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_findMountPointsInPath_NoMountPoints(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	tempDir, err := os.MkdirTemp("", "safe-remove-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test structure
	subDir := filepath.Join(tempDir, "subdir")
	err = os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Mock: no mount points
	mockMounter.On("IsMountPoint", mock.AnythingOfType("string")).Return(false, nil)

	mountPoints, err := remover.findMountPointsInPath(tempDir)
	assert.NoError(t, err)
	assert.Empty(t, mountPoints)

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_findMountPointsInPath_WithDirectoryMountPoint(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	tempDir, err := os.MkdirTemp("", "safe-remove-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	mountDir := filepath.Join(tempDir, "mounted")
	err = os.MkdirAll(mountDir, 0755)
	require.NoError(t, err)

	subMountDir := filepath.Join(mountDir, "submount")
	err = os.MkdirAll(subMountDir, 0755)
	require.NoError(t, err)

	// Mock: mountDir is a mount point, should skip checking submount
	mockMounter.On("IsMountPoint", tempDir).Return(false, nil)
	mockMounter.On("IsMountPoint", mountDir).Return(true, nil)
	// subMountDir should not be checked due to SkipDir optimization

	mountPoints, err := remover.findMountPointsInPath(tempDir)
	assert.NoError(t, err)
	assert.Len(t, mountPoints, 1)
	assert.Contains(t, mountPoints, mountDir)

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_findMountPointsInPath_WithFileMountPoint(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	tempDir, err := os.MkdirTemp("", "safe-remove-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a regular file
	mountedFile := filepath.Join(tempDir, "mounted-file.txt")
	err = os.WriteFile(mountedFile, []byte("mounted"), 0644)
	require.NoError(t, err)

	// Create a regular directory
	regularDir := filepath.Join(tempDir, "regular")
	err = os.MkdirAll(regularDir, 0755)
	require.NoError(t, err)

	// Mock: file is a mount point, directory is not
	mockMounter.On("IsMountPoint", tempDir).Return(false, nil)
	mockMounter.On("IsMountPoint", mountedFile).Return(true, nil)
	mockMounter.On("IsMountPoint", regularDir).Return(false, nil)

	mountPoints, err := remover.findMountPointsInPath(tempDir)
	assert.NoError(t, err)
	assert.Len(t, mountPoints, 1)
	assert.Contains(t, mountPoints, mountedFile)

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_findMountPointsInPath_ErrorHandling(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	tempDir, err := os.MkdirTemp("", "safe-remove-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Mock: IsMountPoint returns error, should be skipped
	mockMounter.On("IsMountPoint", mock.AnythingOfType("string")).Return(false, fmt.Errorf("permission denied"))

	mountPoints, err := remover.findMountPointsInPath(tempDir)
	assert.NoError(t, err) // Errors are skipped, not propagated
	assert.Empty(t, mountPoints)

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_sortMountPointsByDepth(t *testing.T) {
	remover := &SafeRemover{}

	mountPoints := []string{
		"/a",
		"/a/b/c/d",
		"/a/b",
		"/a/b/c",
	}

	sorted := remover.sortMountPointsByDepth(mountPoints)

	expected := []string{
		"/a/b/c/d", // depth 4
		"/a/b/c",   // depth 3
		"/a/b",     // depth 2
		"/a",       // depth 1
	}

	assert.Equal(t, expected, sorted)
}

func TestSafeRemover_unmountWithRetry_Success(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	mountPoint := "/test/mount"

	// Mock: unmount succeeds on first try
	mockMounter.On("Unmount", mountPoint).Return(nil).Once()

	err := remover.unmountWithRetry(mountPoint, false)
	assert.NoError(t, err)

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_unmountWithRetry_FailsWithoutForce(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	mountPoint := "/test/mount"

	// Mock: unmount fails 3 times
	mockMounter.On("Unmount", mountPoint).Return(fmt.Errorf("device busy")).Times(3)

	err := remover.unmountWithRetry(mountPoint, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmount after 3 attempts")

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_verifyUnmounted_Success(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	mountPoints := []string{"/test/mount1", "/test/mount2"}

	// Mock: all mount points are unmounted
	for _, mp := range mountPoints {
		mockMounter.On("IsMountPoint", mp).Return(false, nil)
	}

	err := remover.verifyUnmounted(mountPoints)
	assert.NoError(t, err)

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_verifyUnmounted_StillMounted(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	mountPoints := []string{"/test/mount1", "/test/mount2"}

	// Mock: first mount point is still mounted
	mockMounter.On("IsMountPoint", "/test/mount1").Return(true, nil)

	err := remover.verifyUnmounted(mountPoints)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is still mounted after unmount attempt")

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_verifyUnmounted_ErrorChecking(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	mountPoints := []string{"/test/mount1"}

	// Mock: error checking mount status (should be ignored)
	mockMounter.On("IsMountPoint", "/test/mount1").Return(false, fmt.Errorf("permission denied"))

	err := remover.verifyUnmounted(mountPoints)
	assert.NoError(t, err) // Errors are ignored in verification

	mockMounter.AssertExpectations(t)
}

func TestSafeRemover_InvalidPath(t *testing.T) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	// Test with invalid path characters that might cause filepath.Abs to fail
	invalidPath := string([]byte{0, 1, 2}) // null bytes and control characters

	err := remover.SafeRemoveAll(invalidPath, false, false)
	// On most systems, this should either work or fail gracefully
	// The exact behavior depends on the OS, so we just ensure it doesn't panic
	_ = err // We don't assert specific error since behavior varies by OS
}

// Benchmark tests
func BenchmarkSafeRemover_findMountPointsInPath(b *testing.B) {
	mockMounter := &MockMounter{}
	remover := &SafeRemover{mounter: mockMounter}

	tempDir, err := os.MkdirTemp("", "benchmark-test-*")
	require.NoError(b, err)
	defer os.RemoveAll(tempDir)

	// Create a moderately complex directory structure
	for i := 0; i < 10; i++ {
		dir := filepath.Join(tempDir, fmt.Sprintf("dir%d", i))
		err = os.MkdirAll(dir, 0755)
		require.NoError(b, err)
		
		for j := 0; j < 5; j++ {
			subdir := filepath.Join(dir, fmt.Sprintf("subdir%d", j))
			err = os.MkdirAll(subdir, 0755)
			require.NoError(b, err)
		}
	}

	// Mock: no mount points
	mockMounter.On("IsMountPoint", mock.AnythingOfType("string")).Return(false, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := remover.findMountPointsInPath(tempDir)
		require.NoError(b, err)
	}
}