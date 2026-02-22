package ai.aipr.server.service;

import ai.aipr.server.model.RepositoryInfo;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

import java.io.File;
import java.io.IOException;
import java.net.URI;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Service for managing repository operations (cloning, fetching, etc.).
 *
 * <h3>Credential handling</h3>
 * <p>For HTTPS clones of private repositories, pass a token via
 * {@link #ensureRepository(String, String, String, String)}.
 * The token is injected into the URL as {@code https://x-token:<TOKEN>@host/...}
 * which is accepted by GitHub, GitLab, and Bitbucket.</p>
 *
 * <p><b>Note:</b> the credential-injected URL is never logged — only the
 * sanitized form (without token) appears in log messages.</p>
 */
@Service
public class RepositoryService {

    private static final Logger log = LoggerFactory.getLogger(RepositoryService.class);
    private static final int GIT_TIMEOUT_SECONDS = 120;

    // In-memory storage — will be replaced by PostgreSQL when storage layer is wired
    private final Map<String, RepositoryInfo> repositories = new ConcurrentHashMap<>();
    private final Map<String, List<String>> userRepositories = new ConcurrentHashMap<>();

    @Value("${aipr.repositories.base-path:./repos}")
    private String basePath;

    // =========================================================================
    // Public API
    // =========================================================================

    /**
     * Get the local path for a repository if it has been cloned.
     */
    public Optional<Path> getRepositoryPath(@NotNull String repoId) {
        Path repoPath = repoLocalPath(repoId);
        if (Files.exists(repoPath)) {
            return Optional.of(repoPath);
        }
        return Optional.empty();
    }

    /**
     * Clone or fetch a repository without credentials (public repos).
     */
    public Path ensureRepository(String repoUrl, @NotNull String repoId,
                                 String branch) throws IOException {
        return ensureRepository(repoUrl, repoId, branch, null);
    }

    /**
     * Clone or fetch a repository, optionally authenticating with {@code token}.
     *
     * <p>For private repos on GitHub, GitLab, or Bitbucket, provide the
     * platform access token. It is injected into the HTTPS URL as
     * {@code https://x-token:<TOKEN>@host/path} and is never written to logs.</p>
     *
     * @param repoUrl  the public HTTPS clone URL (no credentials embedded)
     * @param repoId   the canonical repo identifier (e.g., "owner/repo")
     * @param branch   the branch to check out
     * @param token    optional access token; {@code null} for public repos
     */
    public Path ensureRepository(String repoUrl, @NotNull String repoId,
                                 String branch, @Nullable String token) throws IOException {
        Path repoPath = repoLocalPath(repoId);
        String cloneUrl = injectToken(repoUrl, token);

        if (Files.exists(repoPath)) {
            log.info("Fetching existing repository: repoId={}, branch={}", repoId, branch);
            fetch(repoPath, cloneUrl, branch);
        } else {
            log.info("Cloning repository: repoId={}, branch={}", repoId, branch);
            clone(cloneUrl, repoPath, branch);
        }

        return repoPath;
    }

    /**
     * Delete a repository from local storage and in-memory registry.
     */
    public void deleteRepository(String repoId) throws IOException {
        repositories.remove(repoId);
        userRepositories.values().forEach(ids -> ids.remove(repoId));

        Path repoPath = repoLocalPath(repoId);
        if (Files.exists(repoPath)) {
            deleteRecursively(repoPath.toFile());
        }
        log.info("Deleted repository: repoId={}", repoId);
    }

    /**
     * Add a repository record for a user.
     */
    public RepositoryInfo addRepository(String userId, String url, String name) {
        String id = UUID.randomUUID().toString();
        String platform = detectPlatform(url);
        String owner = extractOwner(url);
        String repoName = name != null ? name : extractRepoName(url);

        var repoInfo = new RepositoryInfo(id, platform, owner, repoName, "main", "owner", false);
        repositories.put(id, repoInfo);
        userRepositories.computeIfAbsent(userId, k -> new ArrayList<>()).add(id);

        log.info("Added repository: repoId={}, name={}, userId={}", id, repoName, userId);
        return repoInfo;
    }

    /**
     * Get a repository by ID.
     */
    public Optional<RepositoryInfo> getRepository(String repositoryId) {
        return Optional.ofNullable(repositories.get(repositoryId));
    }

    /**
     * List repositories for a user.
     */
    public List<RepositoryInfo> listRepositories(String userId) {
        List<String> repoIds = userRepositories.getOrDefault(userId, List.of());
        List<RepositoryInfo> result = new ArrayList<>();
        for (String repoId : repoIds) {
            RepositoryInfo repo = repositories.get(repoId);
            if (repo != null) result.add(repo);
        }
        return result;
    }

    // =========================================================================
    // Git Operations
    // =========================================================================

    private void clone(@NotNull String cloneUrl, @NotNull Path targetPath,
                       String branch) throws IOException {
        Files.createDirectories(targetPath.getParent());

        // -c credential.helper="" disables any system credential manager,
        // preventing interactive prompts and ensuring we fail fast on auth errors.
        runGit(null,
                "clone",
                "-c", "credential.helper=",
                "--branch", branch,
                "--depth", "1",
                cloneUrl,
                targetPath.toString());
    }

    private void fetch(@NotNull Path repoPath, @NotNull String remoteUrl,
                       String branch) throws IOException {
        // Update remote URL in case the token changed
        runGit(repoPath, "remote", "set-url", "origin", remoteUrl);
        runGit(repoPath, "fetch",
                "-c", "credential.helper=",
                "--depth", "1",
                "origin", branch);
        runGit(repoPath, "reset", "--hard", "origin/" + branch);
    }

    /**
     * Run a git command, waiting up to {@value #GIT_TIMEOUT_SECONDS} seconds.
     *
     * @param workDir optional working directory; {@code null} to inherit
     * @param args    git sub-command and arguments
     */
    private void runGit(@Nullable Path workDir, String... args) throws IOException {
        String[] command = new String[args.length + 1];
        command[0] = "git";
        System.arraycopy(args, 0, command, 1, args.length);

        ProcessBuilder pb = new ProcessBuilder(command);
        pb.redirectErrorStream(true); // merge stderr into stdout

        if (workDir != null) {
            pb.directory(workDir.toFile());
        }

        // Never inherit stdin — prevents git from hanging waiting for a password prompt
        pb.redirectInput(ProcessBuilder.Redirect.from(new File(System.getProperty("os.name")
                .toLowerCase().contains("win") ? "NUL" : "/dev/null")));

        Process process = pb.start();
        // Drain stdout/stderr to prevent buffer deadlock
        String output;
        try (var reader = new java.io.BufferedReader(
                new java.io.InputStreamReader(process.getInputStream()))) {
            output = reader.lines().collect(java.util.stream.Collectors.joining("\n"));
        }

        try {
            boolean finished = process.waitFor(GIT_TIMEOUT_SECONDS, java.util.concurrent.TimeUnit.SECONDS);
            if (!finished) {
                process.destroyForcibly();
                throw new IOException("Git command timed out after " + GIT_TIMEOUT_SECONDS + "s");
            }
            int exitCode = process.exitValue();
            if (exitCode != 0) {
                // Sanitize output before logging — strip anything that looks like a token in a URL
                String sanitized = output.replaceAll("(?<=://)([^@]+)@", "***@");
                throw new IOException("Git command failed (exit " + exitCode + "): " + sanitized);
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            process.destroyForcibly();
            throw new IOException("Git command interrupted", e);
        }
    }

    // =========================================================================
    // Credential Injection
    // =========================================================================

    /**
     * Inject {@code token} into an HTTPS URL as the password component.
     * Returns the original URL unchanged if no token is provided or the URL
     * is not HTTPS (e.g., SSH git@ URLs).
     *
     * <p>Result format: {@code https://x-token:<TOKEN>@host/path}</p>
     * <p>This is accepted by GitHub, GitLab, and Bitbucket without further config.</p>
     */
    @NotNull
    static String injectToken(@NotNull String repoUrl, @Nullable String token) {
        if (token == null || token.isBlank()) {
            return repoUrl;
        }
        if (!repoUrl.startsWith("https://")) {
            // SSH URLs (git@github.com:...) use key-based auth — token not applicable
            return repoUrl;
        }
        try {
            URI uri = new URI(repoUrl);
            // Strip any existing userinfo to avoid double-embedding
            URI withCreds = new URI(
                    uri.getScheme(),
                    "x-token:" + token,
                    uri.getHost(),
                    uri.getPort(),
                    uri.getPath(),
                    uri.getQuery(),
                    uri.getFragment()
            );
            return withCreds.toASCIIString();
        } catch (Exception e) {
            log.warn("Could not inject token into URL — using original URL: {}", e.getMessage());
            return repoUrl;
        }
    }

    // =========================================================================
    // URL Parsing Helpers
    // =========================================================================

    private void deleteRecursively(@NotNull File file) throws IOException {
        if (file.isDirectory()) {
            File[] children = file.listFiles();
            if (children != null) {
                for (File child : children) {
                    deleteRecursively(child);
                }
            }
        }
        Files.delete(file.toPath());
    }

    @NotNull
    private Path repoLocalPath(@NotNull String repoId) {
        // Replace "/" with "_" to produce a flat directory name (e.g., "owner_repo")
        return Path.of(basePath, repoId.replace("/", "_"));
    }

    @NotNull
    private String detectPlatform(@NotNull String url) {
        if (url.contains("github.com"))    return "github";
        if (url.contains("gitlab.com"))    return "gitlab";
        if (url.contains("bitbucket.org")) return "bitbucket";
        return "unknown";
    }

    private String extractOwner(@NotNull String url) {
        String[] parts = url.split("/");
        return parts.length >= 4 ? parts[parts.length - 2] : "unknown";
    }

    @NotNull
    private String extractRepoName(@NotNull String url) {
        String[] parts = url.split("/");
        if (parts.length > 0) {
            return parts[parts.length - 1].replace(".git", "");
        }
        return "unknown";
    }
}
