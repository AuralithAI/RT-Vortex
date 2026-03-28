package vcs

// ── Platform Factory Functions ──────────────────────────────────────────────
// These are set by the platform sub-packages via init() or explicit
// registration so that the resolver can create clients without importing
// platform packages (which would create a circular dependency).

// PlatformFactory creates a Platform client from resolved credentials.
type PlatformFactory func(creds *ResolvedCreds) Platform

var platformFactories = make(map[PlatformType]PlatformFactory)

// RegisterFactory registers a platform factory.  Called by each platform
// sub-package's init() function.
func RegisterFactory(pt PlatformType, f PlatformFactory) {
	platformFactories[pt] = f
}

func newGitHubFromCreds(creds *ResolvedCreds) Platform {
	if f, ok := platformFactories[PlatformGitHub]; ok {
		return f(creds)
	}
	return nil
}

func newGitLabFromCreds(creds *ResolvedCreds) Platform {
	if f, ok := platformFactories[PlatformGitLab]; ok {
		return f(creds)
	}
	return nil
}

func newBitbucketFromCreds(creds *ResolvedCreds) Platform {
	if f, ok := platformFactories[PlatformBitbucket]; ok {
		return f(creds)
	}
	return nil
}

func newAzureDevOpsFromCreds(creds *ResolvedCreds) Platform {
	if f, ok := platformFactories[PlatformAzureDevOps]; ok {
		return f(creds)
	}
	return nil
}
