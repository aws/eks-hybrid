package mocks

import (
	internalapi "github.com/containerd/containerd/integration/cri-api/pkg/apis"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
	v1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// MockRuntimeService is a testify mock for internalapi.RuntimeService
// Only the methods used by RemovePods are implemented
type MockRuntimeService struct {
	mock.Mock
	internalapi.RuntimeService
}

func (m *MockRuntimeService) ListPodSandbox(filter *v1.PodSandboxFilter, opts ...grpc.CallOption) ([]*v1.PodSandbox, error) {
	args := m.Called(filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*v1.PodSandbox), args.Error(1)
}

func (m *MockRuntimeService) StopPodSandbox(id string, opts ...grpc.CallOption) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockRuntimeService) RemovePodSandbox(id string, opts ...grpc.CallOption) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockRuntimeService) ListContainers(filter *v1.ContainerFilter, opts ...grpc.CallOption) ([]*v1.Container, error) {
	args := m.Called(filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*v1.Container), args.Error(1)
}

func (m *MockRuntimeService) ContainerStatus(id string, opts ...grpc.CallOption) (*v1.ContainerStatus, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*v1.ContainerStatus), args.Error(1)
}

func (m *MockRuntimeService) StopContainer(id string, timeout int64, opts ...grpc.CallOption) error {
	args := m.Called(id, timeout)
	return args.Error(0)
}

func (m *MockRuntimeService) RemoveContainer(id string, opts ...grpc.CallOption) error {
	args := m.Called(id)
	return args.Error(0)
}
