package containerd_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/cri-api/pkg/apis/runtime/v1"

	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/containerd/mocks"
)

func TestClient_RemovePods(t *testing.T) {
	t.Run("no pods or containers", func(t *testing.T) {
		m := new(mocks.MockRuntimeService)
		m.On("ListPodSandbox", mock.Anything).Return([]*v1.PodSandbox{}, nil)
		m.On("ListContainers", (*v1.ContainerFilter)(nil)).Return([]*v1.Container{}, nil)
		c := &containerd.Client{Runtime: m}
		err := c.RemovePods()
		assert.NoError(t, err)
		m.AssertExpectations(t)
	})

	t.Run("error listing pods", func(t *testing.T) {
		m := new(mocks.MockRuntimeService)
		m.On("ListPodSandbox", mock.Anything).Return(nil, errors.New("fail list pods"))
		c := &containerd.Client{Runtime: m}
		err := c.RemovePods()
		assert.ErrorContains(t, err, "fail list pods")
		m.AssertExpectations(t)
	})

	t.Run("error stopping/removing pod", func(t *testing.T) {
		m := new(mocks.MockRuntimeService)
		pod := &v1.PodSandbox{Metadata: &v1.PodSandboxMetadata{Name: "pod1"}, Id: "pod1id"}
		m.On("ListPodSandbox", mock.Anything).Return([]*v1.PodSandbox{pod}, nil)
		m.On("StopPodSandbox", "pod1id").Return(errors.New("fail stop pod")).Times(3)
		m.On("ListContainers", (*v1.ContainerFilter)(nil)).Return([]*v1.Container{}, nil)
		c := &containerd.Client{Runtime: m}
		err := c.RemovePods()
		assert.NoError(t, err, "RemovePods should ignore pod stop errors")
		m.AssertExpectations(t)
	})

	t.Run("error listing containers", func(t *testing.T) {
		m := new(mocks.MockRuntimeService)
		m.On("ListPodSandbox", mock.Anything).Return([]*v1.PodSandbox{}, nil)
		m.On("ListContainers", (*v1.ContainerFilter)(nil)).Return(nil, errors.New("fail list containers"))
		c := &containerd.Client{Runtime: m}
		err := c.RemovePods()
		assert.ErrorContains(t, err, "fail list containers")
		m.AssertExpectations(t)
	})

	t.Run("error stopping/removing container", func(t *testing.T) {
		m := new(mocks.MockRuntimeService)
		container := &v1.Container{Metadata: &v1.ContainerMetadata{Name: "c1"}, Id: "cid1"}
		status := &v1.ContainerStatus{State: v1.ContainerState_CONTAINER_RUNNING}
		m.On("ListPodSandbox", mock.Anything).Return([]*v1.PodSandbox{}, nil)
		m.On("ListContainers", (*v1.ContainerFilter)(nil)).Return([]*v1.Container{container}, nil)
		m.On("ContainerStatus", "cid1").Return(status, nil)
		m.On("StopContainer", "cid1", int64(0)).Return(errors.New("fail stop container")).Times(3)
		c := &containerd.Client{Runtime: m}
		err := c.RemovePods()
		assert.NoError(t, err, "RemovePods should ignore container stop errors")
		m.AssertExpectations(t)
	})

	t.Run("all success", func(t *testing.T) {
		m := new(mocks.MockRuntimeService)
		pod := &v1.PodSandbox{Metadata: &v1.PodSandboxMetadata{Name: "pod1"}, Id: "pod1id"}
		container := &v1.Container{Metadata: &v1.ContainerMetadata{Name: "c1"}, Id: "cid1"}
		status := &v1.ContainerStatus{State: v1.ContainerState_CONTAINER_EXITED}
		m.On("ListPodSandbox", mock.Anything).Return([]*v1.PodSandbox{pod}, nil)
		m.On("StopPodSandbox", "pod1id").Return(nil)
		m.On("RemovePodSandbox", "pod1id").Return(nil)
		m.On("ListContainers", (*v1.ContainerFilter)(nil)).Return([]*v1.Container{container}, nil)
		m.On("ContainerStatus", "cid1").Return(status, nil)
		m.On("RemoveContainer", "cid1").Return(nil)
		c := &containerd.Client{Runtime: m}
		err := c.RemovePods()
		assert.NoError(t, err)
		m.AssertExpectations(t)
	})
}
