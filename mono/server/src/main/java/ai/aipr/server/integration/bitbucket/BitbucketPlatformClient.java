package ai.aipr.server.integration.bitbucket;

import ai.aipr.server.integration.VcsPlatformClient;

/**
 * Bitbucket-specific extension of {@link VcsPlatformClient}.
 *
 * <p>Currently no additional methods beyond the base contract.
 * Exists to allow Spring to disambiguate between platform client beans
 * and to provide a seam for future Bitbucket-specific API methods.</p>
 */
public interface BitbucketPlatformClient extends VcsPlatformClient {
}

