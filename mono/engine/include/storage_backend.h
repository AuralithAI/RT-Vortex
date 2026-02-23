/**
 * AI PR Reviewer - Storage Backend Interface
 * 
 * Abstract interface for storage operations.
 * Implementations: LocalStorageBackend, CloudStorageBackend (S3/GCS/Azure/OCI)
 */

#pragma once

#include <string>
#include <vector>
#include <memory>
#include <functional>
#include <optional>
#include <chrono>
#include <map>

namespace aipr {

/**
 * Storage object metadata
 */
struct StorageObjectInfo {
    std::string key;
    size_t size = 0;
    std::string content_type;
    std::string etag;
    std::chrono::system_clock::time_point last_modified;
    std::map<std::string, std::string> metadata;
};

/**
 * Storage listing result
 */
struct StorageListResult {
    std::vector<StorageObjectInfo> objects;
    std::vector<std::string> common_prefixes;  // For hierarchical listing
    std::string continuation_token;
    bool is_truncated = false;
};

/**
 * Storage operation result
 */
struct StorageResult {
    bool success = false;
    std::string error_message;
    std::string etag;
    std::optional<StorageObjectInfo> info;
};

/**
 * Cloud provider type for storage backend
 */
enum class CloudProvider {
    Local,      // Local filesystem
    AWS,        // Amazon S3
    GCP,        // Google Cloud Storage
    Azure,      // Azure Blob Storage
    OCI,        // Oracle Cloud Infrastructure Object Storage
    MinIO,      // MinIO (S3-compatible)
    Custom      // Custom endpoint (S3-compatible)
};

/**
 * Storage backend configuration
 */
struct StorageConfig {
    CloudProvider provider = CloudProvider::Local;
    
    // Local storage
    std::string base_path;
    
    // Cloud storage common
    std::string bucket;
    std::string region;
    std::string endpoint_url;           // Custom endpoint for S3-compatible stores
    
    // Authentication
    std::string access_key;
    std::string secret_key;
    std::string session_token;          // For STS/assumed roles
    
    // IRSA/Workload Identity (K8s)
    bool use_irsa = false;              // AWS IRSA
    bool use_workload_identity = false; // GCP Workload Identity
    std::string role_arn;               // AWS role ARN for IRSA
    std::string service_account;        // K8s service account name
    
    // Azure specific
    std::string azure_account_name;
    std::string azure_account_key;
    std::string azure_sas_token;
    bool use_azure_ad = false;          // Azure AD authentication
    
    // OCI specific
    std::string oci_tenancy;
    std::string oci_user;
    std::string oci_fingerprint;
    std::string oci_key_file;
    std::string oci_compartment;
    std::string oci_namespace;
    
    // Connection settings
    int timeout_ms = 30000;
    int max_retries = 3;
    bool use_ssl = true;
    bool verify_ssl = true;
    std::string ca_bundle_path;
    
    // Presigned URL settings
    int presigned_url_expiry_seconds = 3600;
};

/**
 * Progress callback for upload/download operations
 */
using ProgressCallback = std::function<void(size_t bytes_transferred, size_t total_bytes)>;

/**
 * Abstract storage backend interface
 */
class StorageBackend {
public:
    virtual ~StorageBackend() = default;
    
    /**
     * Get the provider type
     */
    virtual CloudProvider provider() const = 0;
    
    /**
     * Check if the backend is available
     */
    virtual bool isAvailable() = 0;
    
    // =========================================================================
    // Basic Operations
    // =========================================================================
    
    /**
     * Read an object from storage
     * 
     * @param key Object key/path
     * @return Object content, or empty optional if not found
     */
    virtual std::optional<std::string> read(const std::string& key) = 0;
    
    /**
     * Read an object into a buffer
     * 
     * @param key Object key/path
     * @param buffer Pre-allocated buffer
     * @param buffer_size Buffer size
     * @return Bytes read, or -1 on error
     */
    virtual ssize_t readInto(const std::string& key, char* buffer, size_t buffer_size) = 0;
    
    /**
     * Write an object to storage
     * 
     * @param key Object key/path
     * @param content Object content
     * @param content_type MIME type (default: application/octet-stream)
     * @param metadata Optional metadata
     * @return Operation result
     */
    virtual StorageResult write(
        const std::string& key,
        const std::string& content,
        const std::string& content_type = "application/octet-stream",
        const std::map<std::string, std::string>& metadata = {}
    ) = 0;
    
    /**
     * Write from buffer to storage
     */
    virtual StorageResult writeBuffer(
        const std::string& key,
        const char* data,
        size_t size,
        const std::string& content_type = "application/octet-stream",
        const std::map<std::string, std::string>& metadata = {}
    ) = 0;
    
    /**
     * Check if an object exists
     */
    virtual bool exists(const std::string& key) = 0;
    
    /**
     * Get object metadata without downloading content
     */
    virtual std::optional<StorageObjectInfo> head(const std::string& key) = 0;
    
    /**
     * Delete an object
     */
    virtual StorageResult remove(const std::string& key) = 0;
    
    /**
     * Delete multiple objects
     */
    virtual StorageResult removeMultiple(const std::vector<std::string>& keys) = 0;
    
    // =========================================================================
    // Listing Operations
    // =========================================================================
    
    /**
     * List objects with optional prefix
     * 
     * @param prefix Key prefix to filter
     * @param delimiter Delimiter for hierarchical listing (e.g., "/")
     * @param max_keys Maximum number of keys to return
     * @param continuation_token Token for pagination
     * @return List result
     */
    virtual StorageListResult list(
        const std::string& prefix = "",
        const std::string& delimiter = "",
        int max_keys = 1000,
        const std::string& continuation_token = ""
    ) = 0;
    
    /**
     * List all objects with prefix (handles pagination internally)
     */
    virtual std::vector<StorageObjectInfo> listAll(const std::string& prefix = "") = 0;
    
    // =========================================================================
    // Advanced Operations
    // =========================================================================
    
    /**
     * Copy an object within the same backend
     */
    virtual StorageResult copy(const std::string& source_key, const std::string& dest_key) = 0;
    
    /**
     * Move/rename an object
     */
    virtual StorageResult move(const std::string& source_key, const std::string& dest_key) = 0;
    
    /**
     * Generate a presigned URL for direct access (cloud backends only)
     * 
     * @param key Object key
     * @param expiry_seconds URL expiration time
     * @param for_upload true for PUT URL, false for GET URL
     * @return Presigned URL, or empty if not supported
     */
    virtual std::optional<std::string> getPresignedUrl(
        const std::string& key,
        int expiry_seconds = 3600,
        bool for_upload = false
    ) = 0;
    
    /**
     * Upload with progress callback
     */
    virtual StorageResult writeWithProgress(
        const std::string& key,
        const std::string& content,
        ProgressCallback progress,
        const std::string& content_type = "application/octet-stream"
    ) = 0;
    
    /**
     * Download with progress callback
     */
    virtual std::optional<std::string> readWithProgress(
        const std::string& key,
        ProgressCallback progress
    ) = 0;
    
    // =========================================================================
    // Factory
    // =========================================================================
    
    /**
     * Create a storage backend from configuration
     */
    static std::unique_ptr<StorageBackend> create(const StorageConfig& config);
    
    /**
     * Create a storage backend from environment variables
     * 
     * Checks for:
     *   - AIPR_STORAGE_PROVIDER (local, s3, gcs, azure, oci, minio)
     *   - AIPR_STORAGE_BUCKET
     *   - AIPR_STORAGE_REGION
     *   - AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
     *   - GOOGLE_APPLICATION_CREDENTIALS
     *   - AZURE_STORAGE_ACCOUNT / AZURE_STORAGE_KEY
     *   - OCI_* variables
     */
    static std::unique_ptr<StorageBackend> createFromEnv();
};

/**
 * Local filesystem storage backend
 */
class LocalStorageBackend : public StorageBackend {
public:
    explicit LocalStorageBackend(const std::string& base_path);
    ~LocalStorageBackend() override = default;
    
    CloudProvider provider() const override { return CloudProvider::Local; }
    bool isAvailable() override;
    
    std::optional<std::string> read(const std::string& key) override;
    ssize_t readInto(const std::string& key, char* buffer, size_t buffer_size) override;
    
    StorageResult write(
        const std::string& key,
        const std::string& content,
        const std::string& content_type,
        const std::map<std::string, std::string>& metadata
    ) override;
    
    StorageResult writeBuffer(
        const std::string& key,
        const char* data,
        size_t size,
        const std::string& content_type,
        const std::map<std::string, std::string>& metadata
    ) override;
    
    bool exists(const std::string& key) override;
    std::optional<StorageObjectInfo> head(const std::string& key) override;
    StorageResult remove(const std::string& key) override;
    StorageResult removeMultiple(const std::vector<std::string>& keys) override;
    
    StorageListResult list(
        const std::string& prefix,
        const std::string& delimiter,
        int max_keys,
        const std::string& continuation_token
    ) override;
    
    std::vector<StorageObjectInfo> listAll(const std::string& prefix) override;
    
    StorageResult copy(const std::string& source_key, const std::string& dest_key) override;
    StorageResult move(const std::string& source_key, const std::string& dest_key) override;
    
    std::optional<std::string> getPresignedUrl(
        const std::string& key,
        int expiry_seconds,
        bool for_upload
    ) override;
    
    StorageResult writeWithProgress(
        const std::string& key,
        const std::string& content,
        ProgressCallback progress,
        const std::string& content_type
    ) override;
    
    std::optional<std::string> readWithProgress(
        const std::string& key,
        ProgressCallback progress
    ) override;

private:
    std::string base_path_;
    
    std::string resolvePath(const std::string& key) const;
    void ensureDirectory(const std::string& path);
};

/**
 * Cloud storage backend using presigned URLs (libcurl-based)
 * 
 * Supports S3, GCS, Azure Blob, OCI Object Storage, and S3-compatible stores.
 * Uses AWS Signature V4 for S3-compatible stores.
 */
class CloudStorageBackend : public StorageBackend {
public:
    explicit CloudStorageBackend(const StorageConfig& config);
    ~CloudStorageBackend() override;
    
    CloudProvider provider() const override { return config_.provider; }
    bool isAvailable() override;
    
    std::optional<std::string> read(const std::string& key) override;
    ssize_t readInto(const std::string& key, char* buffer, size_t buffer_size) override;
    
    StorageResult write(
        const std::string& key,
        const std::string& content,
        const std::string& content_type,
        const std::map<std::string, std::string>& metadata
    ) override;
    
    StorageResult writeBuffer(
        const std::string& key,
        const char* data,
        size_t size,
        const std::string& content_type,
        const std::map<std::string, std::string>& metadata
    ) override;
    
    bool exists(const std::string& key) override;
    std::optional<StorageObjectInfo> head(const std::string& key) override;
    StorageResult remove(const std::string& key) override;
    StorageResult removeMultiple(const std::vector<std::string>& keys) override;
    
    StorageListResult list(
        const std::string& prefix,
        const std::string& delimiter,
        int max_keys,
        const std::string& continuation_token
    ) override;
    
    std::vector<StorageObjectInfo> listAll(const std::string& prefix) override;
    
    StorageResult copy(const std::string& source_key, const std::string& dest_key) override;
    StorageResult move(const std::string& source_key, const std::string& dest_key) override;
    
    std::optional<std::string> getPresignedUrl(
        const std::string& key,
        int expiry_seconds,
        bool for_upload
    ) override;
    
    StorageResult writeWithProgress(
        const std::string& key,
        const std::string& content,
        ProgressCallback progress,
        const std::string& content_type
    ) override;
    
    std::optional<std::string> readWithProgress(
        const std::string& key,
        ProgressCallback progress
    ) override;
    
    /**
     * Refresh credentials (for IRSA/Workload Identity)
     */
    void refreshCredentials();

private:
    StorageConfig config_;
    void* curl_handle_;  // CURL*
    
    // Credential cache
    std::string cached_access_key_;
    std::string cached_secret_key_;
    std::string cached_session_token_;
    std::chrono::system_clock::time_point credential_expiry_;
    
    // Internal methods
    void initCurl();
    void ensureCredentials();
    std::string buildEndpointUrl(const std::string& key) const;
    std::string signRequest(
        const std::string& method,
        const std::string& url,
        const std::string& payload_hash,
        const std::map<std::string, std::string>& headers
    ) const;
    
    // AWS Signature V4
    std::string createCanonicalRequest(
        const std::string& method,
        const std::string& uri,
        const std::string& query,
        const std::map<std::string, std::string>& headers,
        const std::string& payload_hash
    ) const;
    
    std::string createStringToSign(
        const std::string& canonical_request,
        const std::string& timestamp,
        const std::string& scope
    ) const;
    
    std::string calculateSignature(
        const std::string& string_to_sign,
        const std::string& date
    ) const;
    
    // IRSA/Workload Identity
    bool refreshIRSACredentials();
    bool refreshWorkloadIdentityCredentials();
    
    // HTTP helpers
    struct HttpResponse {
        int status_code;
        std::string body;
        std::map<std::string, std::string> headers;
    };
    
    HttpResponse httpGet(const std::string& url, const std::map<std::string, std::string>& headers);
    HttpResponse httpPut(const std::string& url, const std::string& body, const std::map<std::string, std::string>& headers);
    HttpResponse httpDelete(const std::string& url, const std::map<std::string, std::string>& headers);
    HttpResponse httpHead(const std::string& url, const std::map<std::string, std::string>& headers);
};

} // namespace aipr
