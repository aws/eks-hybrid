package logger_test

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/logger"
)

func TestFromContext(t *testing.T) {
	g := NewWithT(t)
	devLog, err := zap.NewDevelopment()
	g.Expect(err).NotTo(HaveOccurred())

	testCases := []struct {
		name   string
		logger *zap.Logger
		want   *zap.Logger
	}{
		{
			name:   "has logger",
			logger: devLog,
			want:   devLog,
		},
		{
			name:   "no logger",
			logger: nil,
			want:   zap.NewNop(),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()

			if tc.logger != nil {
				ctx = logger.NewContext(ctx, tc.logger)
			}

			g.Expect(logger.FromContext(ctx)).To(Equal(tc.want))
		})
	}
}
