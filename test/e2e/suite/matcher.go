package suite

import "github.com/aws/eks-hybrid/internal/creds"

type nodeadmConfigMatcher struct {
	matchOS            func(name string) bool
	matchCredsProvider func(name creds.CredentialProvider) bool
}

func (m nodeadmConfigMatcher) matches(osName string, creds creds.CredentialProvider) bool {
	return m.matchOS(osName) && m.matchCredsProvider(creds)
}

type nodeadmConfigMatchers []nodeadmConfigMatcher

func (m nodeadmConfigMatchers) matches(osName string, creds creds.CredentialProvider) bool {
	for _, matcher := range m {
		if matcher.matches(osName, creds) {
			return true
		}
	}
	return false
}
