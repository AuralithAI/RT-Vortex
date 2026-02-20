package ai.aipr.server.service;

import ai.aipr.server.model.RepositoryInfo;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

import java.io.File;
import java.io.IOException;
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
 */
@Service
public class RepositoryService {
    
    private static final Logger log = LoggerFactory.getLogger(RepositoryService.class);
    
    // In-memory storage for repositories (replace with database in production)
    private final Map<String, RepositoryInfo> repositories = new ConcurrentHashMap<>();
    private final Map<String, List<String>> userRepositories = new ConcurrentHashMap<>();
    
    @Value("${aipr.repositories.base-path:./repos}")
    private String basePath;
    
    /**
     * Get the local path for a repository.
     */
    public Optional<Path> getRepositoryPath(String repoId) {
        Path repoPath = Path.of(basePath, repoId.replace("/", "_"));
        if (Files.exists(repoPath)) {
            return Optional.of(repoPath);
        }
        return Optional.empty();
    }
    
    /**
     * Clone or fetch a repository.
     */
    public Path ensureRepository(String repoUrl, String repoId, String branch) throws IOException {
        Path repoPath = Path.of(basePath, repoId.replace("/", "_"));
        
        if (Files.exists(repoPath)) {
            log.info("Fetching existing repository: {}", repoId);
            fetch(repoPath, branch);
        } else {
            log.info("Cloning repository: {} from {}", repoId, repoUrl);
            clone(repoUrl, repoPath, branch);
        }
        
        return repoPath;
    }
    
    /**
     * Clone a repository.
     */
    private void clone(String repoUrl, Path targetPath, String branch) throws IOException {
        Files.createDirectories(targetPath.getParent());
        
        ProcessBuilder pb = new ProcessBuilder(
                "git", "clone", 
                "--branch", branch,
                "--depth", "1",
                repoUrl, 
                targetPath.toString()
        );
        pb.inheritIO();
        
        try {
            Process process = pb.start();
            int exitCode = process.waitFor();
            if (exitCode != 0) {
                throw new IOException("Git clone failed with exit code: " + exitCode);
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IOException("Git clone interrupted", e);
        }
    }
    
    /**
     * Fetch latest changes.
     */
    private void fetch(Path repoPath, String branch) throws IOException {
        ProcessBuilder pb = new ProcessBuilder(
                "git", "-C", repoPath.toString(),
                "fetch", "origin", branch
        );
        pb.inheritIO();
        
        try {
            Process process = pb.start();
            int exitCode = process.waitFor();
            if (exitCode != 0) {
                throw new IOException("Git fetch failed with exit code: " + exitCode);
            }
            
            // Reset to origin/branch
            pb = new ProcessBuilder(
                    "git", "-C", repoPath.toString(),
                    "reset", "--hard", "origin/" + branch
            );
            pb.inheritIO();
            process = pb.start();
            process.waitFor();
            
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IOException("Git fetch interrupted", e);
        }
    }
    
    /**
     * Delete a repository.
     */
    public void deleteRepository(String repoId) throws IOException {
        Path repoPath = Path.of(basePath, repoId.replace("/", "_"));
        if (Files.exists(repoPath)) {
            deleteRecursively(repoPath.toFile());
        }
    }
    
    private void deleteRecursively(File file) throws IOException {
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
    
    /**
     * Add a repository for a user.
     */
    public RepositoryInfo addRepository(String url, String name) {
        String id = UUID.randomUUID().toString();
        String platform = detectPlatform(url);
        String owner = extractOwner(url);
        String repoName = name != null ? name : extractRepoName(url);
        
        var repoInfo = new RepositoryInfo(id, platform, owner, repoName, "main", "owner", false);
        repositories.put(id, repoInfo);
        
        log.info("Added repository: id={}, name={}", id, repoName);
        return repoInfo;
    }
    
    /**
     * List repositories for a user.
     */
    public List<RepositoryInfo> listRepositories(String userId) {
        List<String> repoIds = userRepositories.getOrDefault(userId, List.of());
        List<RepositoryInfo> result = new ArrayList<>();
        for (String repoId : repoIds) {
            RepositoryInfo repo = repositories.get(repoId);
            if (repo != null) {
                result.add(repo);
            }
        }
        return result;
    }
    
    private String detectPlatform(String url) {
        if (url.contains("github.com")) return "github";
        if (url.contains("gitlab.com")) return "gitlab";
        if (url.contains("bitbucket.org")) return "bitbucket";
        return "unknown";
    }
    
    private String extractOwner(String url) {
        // Extract owner from URL like https://github.com/owner/repo
        String[] parts = url.split("/");
        if (parts.length >= 4) {
            return parts[parts.length - 2];
        }
        return "unknown";
    }
    
    private String extractRepoName(String url) {
        // Extract repo name from URL
        String[] parts = url.split("/");
        if (parts.length > 0) {
            String name = parts[parts.length - 1];
            return name.replace(".git", "");
        }
        return "unknown";
    }
}
