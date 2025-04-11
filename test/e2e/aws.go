package e2e

import (
	"context"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/ratelimit"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
)

// SanitizeForAWSName removes everything except alphanumeric characters and hyphens from a string.
func SanitizeForAWSName(input string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9-]+`)
	return re.ReplaceAllString(input, "")
}

// Truncate drops characters from the end of a string if it exceeds the limit.
func Truncate(name string, limit int) string {
	if len(name) > limit {
		name = name[:limit]
	}
	return name
}

func NewAWSConfig(ctx context.Context, optFns ...func(*config.LoadOptions) error) (aws.Config, error) {
	optFns = append(optFns, config.WithRetryer(func() aws.Retryer {
		return retry.NewAdaptiveMode(func(o *retry.AdaptiveModeOptions) {
			// the adaptive retryer wraps the standard retryer but implements a custom rate limiterfor getting the AttemptToken
			// which the sdk calls internally before making a request (including retried requests)
			// when getting this GetAttemptToken it will sleep if neccessary based on its internal rate limiter
			// However, when a request fails, the sdk calls GetRetryToken, which adapative sends its wrapped standard retryer
			// the standard retryer uses the TokenRateLimit to make a determination of whether to retry or not and its pretty tight
			// this disables the TokenRateLimit on the standard retryer by setting it to the None implementation
			// see for more:
			//	https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-retries-timeouts.html
			//	https://github.com/aws/aws-sdk-go-v2/blob/main/aws/retry/adaptive.go
			//	https://github.com/aws/aws-sdk-go-v2/blob/main/aws/retry/standard.go
			o.StandardOptions = []func(*retry.StandardOptions){
				func(o *retry.StandardOptions) {
					o.MaxAttempts = 40
					o.RateLimiter = ratelimit.None
				},
			}
		})
	}))
	return config.LoadDefaultConfig(ctx, optFns...)
}

func NewEKSClient(aws aws.Config, endpoint string) *eks.Client {
	return eks.NewFromConfig(aws, func(o *eks.Options) {
		o.EndpointResolverV2 = &EksResolverV2{Endpoint: endpoint}
	})
}
