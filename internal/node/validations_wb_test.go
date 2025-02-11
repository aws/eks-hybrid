package node

import (
	"testing"

	"github.com/aws/smithy-go/ptr"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_isDrained(t *testing.T) {
	testCases := []struct {
		name string
		pods []corev1.Pod
		want bool
	}{
		{
			name: "drained: no pods",
			want: true,
		},
		{
			name: "drained: all pods are daemonsets",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod1",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind:       "DaemonSet",
								Controller: ptr.Bool(true),
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "not drained",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod1",
					},
				},
			},
			want: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(isDrained(tc.pods)).To(Equal(tc.want))
		})
	}
}
