package ai.aipr.server.integration.gitlab;

import ai.aipr.server.integration.VcsPlatformClient;

/**
 * GitLab-specific extension of {@link VcsPlatformClient}.
 *
 * <p>Currently no additional methods beyond the base contract.
 * Exists to allow Spring to disambiguate between platform client beans
 * and to provide a seam for future GitLab-specific API methods.</p>
 */
public interface GitLabPlatformClient extends VcsPlatformClient {
}

