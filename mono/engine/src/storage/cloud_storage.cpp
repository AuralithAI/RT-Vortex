/**
 * AI PR Reviewer - Cloud Storage Backend Implementation
 * 
 * S3-compatible cloud storage using libcurl and AWS Signature V4.
 * Supports AWS S3, GCS, Azure Blob, OCI Object Storage, MinIO, and custom endpoints.
 */

#include "storage_backend.h"
#include <curl/curl.h>
#include <openssl/hmac.h>
#include <openssl/sha.h>
#include <nlohmann/json.hpp>
#include <sstream>
#include <iomanip>
#include <ctime>
#include <algorithm>
#include <regex>
#include <fstream>

using json = nlohmann::json;

namespace aipr {

// =============================================================================
// Helper Functions
// =============================================================================

static std::string sha256Hex(const std::string& data) {
    unsigned char hash[SHA256_DIGEST_LENGTH];
    SHA256(reinterpret_cast<const unsigned char*>(data.data()), data.size(), hash);
    
    std::ostringstream ss;
    for (int i = 0; i < SHA256_DIGEST_LENGTH; ++i) {
        ss << std::hex << std::setw(2) << std::setfill('0') << (int)hash[i];
    }
    return ss.str();
}

static std::string hmacSha256(const std::string& key, const std::string& data) {
    unsigned char hash[SHA256_DIGEST_LENGTH];
    unsigned int len = SHA256_DIGEST_LENGTH;
    
    HMAC(EVP_sha256(),
         key.data(), key.size(),
         reinterpret_cast<const unsigned char*>(data.data()), data.size(),
         hash, &len);
    
    return std::string(reinterpret_cast<char*>(hash), len);
}

static std::string hmacSha256Hex(const std::string& key, const std::string& data) {
    std::string hash = hmacSha256(key, data);
    
    std::ostringstream ss;
    for (unsigned char c : hash) {
        ss << std::hex << std::setw(2) << std::setfill('0') << (int)c;
    }
    return ss.str();
}

static std::string urlEncode(const std::string& value, bool encode_slash = true) {
    std::ostringstream escaped;
    escaped.fill('0');
    escaped << std::hex;
    
    for (char c : value) {
        if (isalnum(c) || c == '-' || c == '_' || c == '.' || c == '~' ||
            (!encode_slash && c == '/')) {
            escaped << c;
        } else {
            escaped << '%' << std::setw(2) << (int)(unsigned char)c;
        }
    }
    
    return escaped.str();
}

static std::string getTimestamp() {
    std::time_t now = std::time(nullptr);
    std::tm* gmt = std::gmtime(&now);
    
    char buffer[20];
    std::strftime(buffer, sizeof(buffer), "%Y%m%dT%H%M%SZ", gmt);
    return buffer;
}

static std::string getDateStamp() {
    std::time_t now = std::time(nullptr);
    std::tm* gmt = std::gmtime(&now);
    
    char buffer[10];
    std::strftime(buffer, sizeof(buffer), "%Y%m%d", gmt);
    return buffer;
}

static std::string trimString(const std::string& str) {
    size_t start = str.find_first_not_of(" \t\n\r");
    if (start == std::string::npos) return "";
    size_t end = str.find_last_not_of(" \t\n\r");
    return str.substr(start, end - start + 1);
}

// =============================================================================
// CURL Callbacks
// =============================================================================

struct CurlWriteBuffer {
    std::string data;
};

static size_t curlWriteCallback(char* ptr, size_t size, size_t nmemb, void* userdata) {
    auto* buffer = static_cast<CurlWriteBuffer*>(userdata);
    size_t total = size * nmemb;
    buffer->data.append(ptr, total);
    return total;
}

struct CurlHeaderBuffer {
    std::map<std::string, std::string> headers;
};

static size_t curlHeaderCallback(char* buffer, size_t size, size_t nitems, void* userdata) {
    auto* headers = static_cast<CurlHeaderBuffer*>(userdata);
    size_t total = size * nitems;
    
    std::string header(buffer, total);
    size_t colon = header.find(':');
    
    if (colon != std::string::npos) {
        std::string key = trimString(header.substr(0, colon));
        std::string value = trimString(header.substr(colon + 1));
        
        // Lowercase the key for consistent access
        std::transform(key.begin(), key.end(), key.begin(), ::tolower);
        headers->headers[key] = value;
    }
    
    return total;
}

struct CurlProgressData {
    ProgressCallback callback;
    size_t total_size;
};

static int curlProgressCallback(void* clientp, curl_off_t dltotal, curl_off_t dlnow,
                                 curl_off_t ultotal, curl_off_t ulnow) {
    auto* data = static_cast<CurlProgressData*>(clientp);
    
    if (data->callback) {
        size_t total = (dltotal > 0) ? dltotal : (ultotal > 0) ? ultotal : data->total_size;
        size_t current = (dlnow > 0) ? dlnow : ulnow;
        data->callback(current, total);
    }
    
    return 0;  // Return non-zero to abort
}

// =============================================================================
// CloudStorageBackend Implementation
// =============================================================================

CloudStorageBackend::CloudStorageBackend(const StorageConfig& config)
    : config_(config), curl_handle_(nullptr) {
    initCurl();
}

CloudStorageBackend::~CloudStorageBackend() {
    if (curl_handle_) {
        curl_easy_cleanup(static_cast<CURL*>(curl_handle_));
    }
}

void CloudStorageBackend::initCurl() {
    curl_handle_ = curl_easy_init();
    if (!curl_handle_) {
        throw std::runtime_error("Failed to initialize libcurl");
    }
    
    CURL* curl = static_cast<CURL*>(curl_handle_);
    
    // Set common options
    curl_easy_setopt(curl, CURLOPT_TIMEOUT_MS, config_.timeout_ms);
    curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 10);
    curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 1L);
    
    if (!config_.use_ssl || !config_.verify_ssl) {
        curl_easy_setopt(curl, CURLOPT_SSL_VERIFYPEER, 0L);
        curl_easy_setopt(curl, CURLOPT_SSL_VERIFYHOST, 0L);
    }
    
    if (!config_.ca_bundle_path.empty()) {
        curl_easy_setopt(curl, CURLOPT_CAINFO, config_.ca_bundle_path.c_str());
    }
}

void CloudStorageBackend::ensureCredentials() {
    auto now = std::chrono::system_clock::now();
    
    // If credentials are still valid, return
    if (!cached_access_key_.empty() && now < credential_expiry_) {
        return;
    }
    
    // Check for IRSA
    if (config_.use_irsa) {
        if (refreshIRSACredentials()) {
            return;
        }
    }
    
    // Check for Workload Identity
    if (config_.use_workload_identity) {
        if (refreshWorkloadIdentityCredentials()) {
            return;
        }
    }
    
    // Use static credentials from config
    cached_access_key_ = config_.access_key;
    cached_secret_key_ = config_.secret_key;
    cached_session_token_ = config_.session_token;
    credential_expiry_ = now + std::chrono::hours(24);  // Assume static creds don't expire
}

bool CloudStorageBackend::refreshIRSACredentials() {
    // AWS IRSA: Read credentials from the web identity token file
    const char* token_file = std::getenv("AWS_WEB_IDENTITY_TOKEN_FILE");
    const char* role_arn = std::getenv("AWS_ROLE_ARN");
    
    if (!token_file || !role_arn) {
        return false;
    }
    
    // Read the web identity token
    std::ifstream file(token_file);
    if (!file) {
        return false;
    }
    
    std::ostringstream ss;
    ss << file.rdbuf();
    std::string token = ss.str();
    
    // Call STS AssumeRoleWithWebIdentity
    std::string sts_url = "https://sts." + config_.region + ".amazonaws.com/";
    std::string params = "Action=AssumeRoleWithWebIdentity"
                         "&Version=2011-06-15"
                         "&RoleArn=" + urlEncode(role_arn) +
                         "&RoleSessionName=aipr-engine"
                         "&WebIdentityToken=" + urlEncode(token);
    
    CURL* curl = static_cast<CURL*>(curl_handle_);
    CurlWriteBuffer write_buffer;
    
    curl_easy_setopt(curl, CURLOPT_URL, (sts_url + "?" + params).c_str());
    curl_easy_setopt(curl, CURLOPT_HTTPGET, 1L);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curlWriteCallback);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &write_buffer);
    
    CURLcode res = curl_easy_perform(curl);
    
    if (res != CURLE_OK) {
        return false;
    }
    
    // Parse XML response (simplified - in production use proper XML parser)
    std::regex access_key_regex("<AccessKeyId>([^<]+)</AccessKeyId>");
    std::regex secret_key_regex("<SecretAccessKey>([^<]+)</SecretAccessKey>");
    std::regex session_token_regex("<SessionToken>([^<]+)</SessionToken>");
    std::regex expiration_regex("<Expiration>([^<]+)</Expiration>");
    
    std::smatch match;
    if (std::regex_search(write_buffer.data, match, access_key_regex)) {
        cached_access_key_ = match[1].str();
    }
    if (std::regex_search(write_buffer.data, match, secret_key_regex)) {
        cached_secret_key_ = match[1].str();
    }
    if (std::regex_search(write_buffer.data, match, session_token_regex)) {
        cached_session_token_ = match[1].str();
    }
    
    // Set expiry to 5 minutes before actual expiration
    credential_expiry_ = std::chrono::system_clock::now() + std::chrono::minutes(55);
    
    return !cached_access_key_.empty();
}

bool CloudStorageBackend::refreshWorkloadIdentityCredentials() {
    // GCP Workload Identity: Use metadata server
    const char* identity_token_url = "http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token";
    
    CURL* curl = static_cast<CURL*>(curl_handle_);
    CurlWriteBuffer write_buffer;
    
    struct curl_slist* headers = nullptr;
    headers = curl_slist_append(headers, "Metadata-Flavor: Google");
    
    curl_easy_setopt(curl, CURLOPT_URL, identity_token_url);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_HTTPGET, 1L);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curlWriteCallback);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &write_buffer);
    
    CURLcode res = curl_easy_perform(curl);
    curl_slist_free_all(headers);
    
    if (res != CURLE_OK) {
        return false;
    }
    
    try {
        json response = json::parse(write_buffer.data);
        cached_access_key_ = response["access_token"].get<std::string>();
        credential_expiry_ = std::chrono::system_clock::now() + 
                             std::chrono::seconds(response["expires_in"].get<int>() - 300);
        return true;
    } catch (...) {
        return false;
    }
}

std::string CloudStorageBackend::buildEndpointUrl(const std::string& key) const {
    std::string endpoint;
    
    switch (config_.provider) {
        case CloudProvider::AWS:
            if (!config_.endpoint_url.empty()) {
                endpoint = config_.endpoint_url;
            } else {
                endpoint = "https://" + config_.bucket + ".s3." + config_.region + ".amazonaws.com";
            }
            break;
            
        case CloudProvider::GCP:
            endpoint = "https://storage.googleapis.com/" + config_.bucket;
            break;
            
        case CloudProvider::Azure:
            endpoint = "https://" + config_.azure_account_name + ".blob.core.windows.net/" + config_.bucket;
            break;
            
        case CloudProvider::OCI:
            endpoint = "https://" + config_.oci_namespace + ".objectstorage." + config_.region + 
                       ".oraclecloud.com/n/" + config_.oci_namespace + "/b/" + config_.bucket + "/o";
            break;
            
        case CloudProvider::MinIO:
        case CloudProvider::Custom:
            endpoint = config_.endpoint_url + "/" + config_.bucket;
            break;
            
        default:
            throw std::runtime_error("Unsupported cloud provider");
    }
    
    // Append key
    if (!key.empty()) {
        endpoint += "/" + urlEncode(key, false);
    }
    
    return endpoint;
}

std::string CloudStorageBackend::createCanonicalRequest(
    const std::string& method,
    const std::string& uri,
    const std::string& query,
    const std::map<std::string, std::string>& headers,
    const std::string& payload_hash
) const {
    std::ostringstream canonical;
    
    // HTTP method
    canonical << method << "\n";
    
    // Canonical URI (URL-encoded path)
    canonical << uri << "\n";
    
    // Canonical query string
    canonical << query << "\n";
    
    // Canonical headers (lowercase, sorted)
    std::vector<std::pair<std::string, std::string>> sorted_headers(headers.begin(), headers.end());
    std::sort(sorted_headers.begin(), sorted_headers.end());
    
    std::string signed_headers;
    for (const auto& [key, value] : sorted_headers) {
        std::string lkey = key;
        std::transform(lkey.begin(), lkey.end(), lkey.begin(), ::tolower);
        canonical << lkey << ":" << trimString(value) << "\n";
        
        if (!signed_headers.empty()) signed_headers += ";";
        signed_headers += lkey;
    }
    canonical << "\n";
    
    // Signed headers
    canonical << signed_headers << "\n";
    
    // Hashed payload
    canonical << payload_hash;
    
    return canonical.str();
}

std::string CloudStorageBackend::createStringToSign(
    const std::string& canonical_request,
    const std::string& timestamp,
    const std::string& scope
) const {
    std::ostringstream sts;
    
    sts << "AWS4-HMAC-SHA256\n";
    sts << timestamp << "\n";
    sts << scope << "\n";
    sts << sha256Hex(canonical_request);
    
    return sts.str();
}

std::string CloudStorageBackend::calculateSignature(
    const std::string& string_to_sign,
    const std::string& date
) const {
    // Derive signing key
    std::string k_date = hmacSha256("AWS4" + cached_secret_key_, date);
    std::string k_region = hmacSha256(k_date, config_.region);
    std::string k_service = hmacSha256(k_region, "s3");
    std::string k_signing = hmacSha256(k_service, "aws4_request");
    
    // Calculate signature
    return hmacSha256Hex(k_signing, string_to_sign);
}

CloudStorageBackend::HttpResponse CloudStorageBackend::httpGet(
    const std::string& url,
    const std::map<std::string, std::string>& headers
) {
    CURL* curl = static_cast<CURL*>(curl_handle_);
    
    CurlWriteBuffer write_buffer;
    CurlHeaderBuffer header_buffer;
    
    curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
    curl_easy_setopt(curl, CURLOPT_HTTPGET, 1L);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curlWriteCallback);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &write_buffer);
    curl_easy_setopt(curl, CURLOPT_HEADERFUNCTION, curlHeaderCallback);
    curl_easy_setopt(curl, CURLOPT_HEADERDATA, &header_buffer);
    
    struct curl_slist* header_list = nullptr;
    for (const auto& [key, value] : headers) {
        header_list = curl_slist_append(header_list, (key + ": " + value).c_str());
    }
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, header_list);
    
    CURLcode res = curl_easy_perform(curl);
    
    long status_code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status_code);
    
    curl_slist_free_all(header_list);
    
    HttpResponse response;
    response.status_code = static_cast<int>(status_code);
    response.body = write_buffer.data;
    response.headers = header_buffer.headers;
    
    return response;
}

CloudStorageBackend::HttpResponse CloudStorageBackend::httpPut(
    const std::string& url,
    const std::string& body,
    const std::map<std::string, std::string>& headers
) {
    CURL* curl = static_cast<CURL*>(curl_handle_);
    
    CurlWriteBuffer write_buffer;
    CurlHeaderBuffer header_buffer;
    
    curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
    curl_easy_setopt(curl, CURLOPT_CUSTOMREQUEST, "PUT");
    curl_easy_setopt(curl, CURLOPT_POSTFIELDS, body.c_str());
    curl_easy_setopt(curl, CURLOPT_POSTFIELDSIZE, body.size());
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curlWriteCallback);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &write_buffer);
    curl_easy_setopt(curl, CURLOPT_HEADERFUNCTION, curlHeaderCallback);
    curl_easy_setopt(curl, CURLOPT_HEADERDATA, &header_buffer);
    
    struct curl_slist* header_list = nullptr;
    for (const auto& [key, value] : headers) {
        header_list = curl_slist_append(header_list, (key + ": " + value).c_str());
    }
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, header_list);
    
    CURLcode res = curl_easy_perform(curl);
    
    long status_code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status_code);
    
    curl_slist_free_all(header_list);
    curl_easy_setopt(curl, CURLOPT_CUSTOMREQUEST, nullptr);
    
    HttpResponse response;
    response.status_code = static_cast<int>(status_code);
    response.body = write_buffer.data;
    response.headers = header_buffer.headers;
    
    return response;
}

CloudStorageBackend::HttpResponse CloudStorageBackend::httpDelete(
    const std::string& url,
    const std::map<std::string, std::string>& headers
) {
    CURL* curl = static_cast<CURL*>(curl_handle_);
    
    CurlWriteBuffer write_buffer;
    
    curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
    curl_easy_setopt(curl, CURLOPT_CUSTOMREQUEST, "DELETE");
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curlWriteCallback);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &write_buffer);
    
    struct curl_slist* header_list = nullptr;
    for (const auto& [key, value] : headers) {
        header_list = curl_slist_append(header_list, (key + ": " + value).c_str());
    }
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, header_list);
    
    CURLcode res = curl_easy_perform(curl);
    
    long status_code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status_code);
    
    curl_slist_free_all(header_list);
    curl_easy_setopt(curl, CURLOPT_CUSTOMREQUEST, nullptr);
    
    HttpResponse response;
    response.status_code = static_cast<int>(status_code);
    response.body = write_buffer.data;
    
    return response;
}

CloudStorageBackend::HttpResponse CloudStorageBackend::httpHead(
    const std::string& url,
    const std::map<std::string, std::string>& headers
) {
    CURL* curl = static_cast<CURL*>(curl_handle_);
    
    CurlHeaderBuffer header_buffer;
    
    curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
    curl_easy_setopt(curl, CURLOPT_NOBODY, 1L);
    curl_easy_setopt(curl, CURLOPT_HEADERFUNCTION, curlHeaderCallback);
    curl_easy_setopt(curl, CURLOPT_HEADERDATA, &header_buffer);
    
    struct curl_slist* header_list = nullptr;
    for (const auto& [key, value] : headers) {
        header_list = curl_slist_append(header_list, (key + ": " + value).c_str());
    }
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, header_list);
    
    CURLcode res = curl_easy_perform(curl);
    
    long status_code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status_code);
    
    curl_slist_free_all(header_list);
    curl_easy_setopt(curl, CURLOPT_NOBODY, 0L);
    
    HttpResponse response;
    response.status_code = static_cast<int>(status_code);
    response.headers = header_buffer.headers;
    
    return response;
}

bool CloudStorageBackend::isAvailable() {
    try {
        ensureCredentials();
        
        // Try to list bucket (limited to 1 object)
        auto result = list("", "", 1, "");
        return true;
    } catch (...) {
        return false;
    }
}

std::optional<std::string> CloudStorageBackend::read(const std::string& key) {
    ensureCredentials();
    
    std::string url = buildEndpointUrl(key);
    std::string timestamp = getTimestamp();
    std::string date = getDateStamp();
    std::string payload_hash = sha256Hex("");
    
    // Build headers
    std::map<std::string, std::string> headers;
    headers["Host"] = url.substr(url.find("://") + 3, url.find("/", url.find("://") + 3) - url.find("://") - 3);
    headers["x-amz-date"] = timestamp;
    headers["x-amz-content-sha256"] = payload_hash;
    
    if (!cached_session_token_.empty()) {
        headers["x-amz-security-token"] = cached_session_token_;
    }
    
    // Sign request
    std::string scope = date + "/" + config_.region + "/s3/aws4_request";
    std::string canonical_uri = "/" + key;
    std::string canonical_request = createCanonicalRequest("GET", canonical_uri, "", headers, payload_hash);
    std::string string_to_sign = createStringToSign(canonical_request, timestamp, scope);
    std::string signature = calculateSignature(string_to_sign, date);
    
    // Build Authorization header
    std::string signed_headers;
    for (const auto& [k, v] : headers) {
        if (!signed_headers.empty()) signed_headers += ";";
        std::string lk = k;
        std::transform(lk.begin(), lk.end(), lk.begin(), ::tolower);
        signed_headers += lk;
    }
    
    headers["Authorization"] = "AWS4-HMAC-SHA256 Credential=" + cached_access_key_ + "/" + scope +
                               ", SignedHeaders=" + signed_headers +
                               ", Signature=" + signature;
    
    auto response = httpGet(url, headers);
    
    if (response.status_code == 200) {
        return response.body;
    } else if (response.status_code == 404) {
        return std::nullopt;
    }
    
    throw std::runtime_error("Failed to read object: HTTP " + std::to_string(response.status_code));
}

ssize_t CloudStorageBackend::readInto(const std::string& key, char* buffer, size_t buffer_size) {
    auto content = read(key);
    if (!content) {
        return -1;
    }
    
    size_t to_copy = std::min(content->size(), buffer_size);
    std::memcpy(buffer, content->data(), to_copy);
    return to_copy;
}

StorageResult CloudStorageBackend::write(
    const std::string& key,
    const std::string& content,
    const std::string& content_type,
    const std::map<std::string, std::string>& metadata
) {
    ensureCredentials();
    
    std::string url = buildEndpointUrl(key);
    std::string timestamp = getTimestamp();
    std::string date = getDateStamp();
    std::string payload_hash = sha256Hex(content);
    
    // Build headers
    std::map<std::string, std::string> headers;
    headers["Host"] = url.substr(url.find("://") + 3, url.find("/", url.find("://") + 3) - url.find("://") - 3);
    headers["x-amz-date"] = timestamp;
    headers["x-amz-content-sha256"] = payload_hash;
    headers["Content-Type"] = content_type;
    headers["Content-Length"] = std::to_string(content.size());
    
    if (!cached_session_token_.empty()) {
        headers["x-amz-security-token"] = cached_session_token_;
    }
    
    // Add custom metadata
    for (const auto& [k, v] : metadata) {
        headers["x-amz-meta-" + k] = v;
    }
    
    // Sign request
    std::string scope = date + "/" + config_.region + "/s3/aws4_request";
    std::string canonical_uri = "/" + key;
    std::string canonical_request = createCanonicalRequest("PUT", canonical_uri, "", headers, payload_hash);
    std::string string_to_sign = createStringToSign(canonical_request, timestamp, scope);
    std::string signature = calculateSignature(string_to_sign, date);
    
    // Build Authorization header
    std::string signed_headers;
    for (const auto& [k, v] : headers) {
        if (!signed_headers.empty()) signed_headers += ";";
        std::string lk = k;
        std::transform(lk.begin(), lk.end(), lk.begin(), ::tolower);
        signed_headers += lk;
    }
    
    headers["Authorization"] = "AWS4-HMAC-SHA256 Credential=" + cached_access_key_ + "/" + scope +
                               ", SignedHeaders=" + signed_headers +
                               ", Signature=" + signature;
    
    auto response = httpPut(url, content, headers);
    
    if (response.status_code >= 200 && response.status_code < 300) {
        StorageResult result;
        result.success = true;
        result.etag = response.headers["etag"];
        return result;
    }
    
    return StorageResult{false, "PUT failed: HTTP " + std::to_string(response.status_code) + " - " + response.body};
}

StorageResult CloudStorageBackend::writeBuffer(
    const std::string& key,
    const char* data,
    size_t size,
    const std::string& content_type,
    const std::map<std::string, std::string>& metadata
) {
    return write(key, std::string(data, size), content_type, metadata);
}

bool CloudStorageBackend::exists(const std::string& key) {
    auto info = head(key);
    return info.has_value();
}

std::optional<StorageObjectInfo> CloudStorageBackend::head(const std::string& key) {
    ensureCredentials();
    
    std::string url = buildEndpointUrl(key);
    std::string timestamp = getTimestamp();
    std::string date = getDateStamp();
    std::string payload_hash = sha256Hex("");
    
    std::map<std::string, std::string> headers;
    headers["Host"] = url.substr(url.find("://") + 3, url.find("/", url.find("://") + 3) - url.find("://") - 3);
    headers["x-amz-date"] = timestamp;
    headers["x-amz-content-sha256"] = payload_hash;
    
    if (!cached_session_token_.empty()) {
        headers["x-amz-security-token"] = cached_session_token_;
    }
    
    // Sign request
    std::string scope = date + "/" + config_.region + "/s3/aws4_request";
    std::string canonical_uri = "/" + key;
    std::string canonical_request = createCanonicalRequest("HEAD", canonical_uri, "", headers, payload_hash);
    std::string string_to_sign = createStringToSign(canonical_request, timestamp, scope);
    std::string signature = calculateSignature(string_to_sign, date);
    
    std::string signed_headers;
    for (const auto& [k, v] : headers) {
        if (!signed_headers.empty()) signed_headers += ";";
        std::string lk = k;
        std::transform(lk.begin(), lk.end(), lk.begin(), ::tolower);
        signed_headers += lk;
    }
    
    headers["Authorization"] = "AWS4-HMAC-SHA256 Credential=" + cached_access_key_ + "/" + scope +
                               ", SignedHeaders=" + signed_headers +
                               ", Signature=" + signature;
    
    auto response = httpHead(url, headers);
    
    if (response.status_code == 200) {
        StorageObjectInfo info;
        info.key = key;
        
        if (response.headers.count("content-length")) {
            info.size = std::stoull(response.headers["content-length"]);
        }
        if (response.headers.count("content-type")) {
            info.content_type = response.headers["content-type"];
        }
        if (response.headers.count("etag")) {
            info.etag = response.headers["etag"];
        }
        
        return info;
    }
    
    return std::nullopt;
}

StorageResult CloudStorageBackend::remove(const std::string& key) {
    ensureCredentials();
    
    std::string url = buildEndpointUrl(key);
    std::string timestamp = getTimestamp();
    std::string date = getDateStamp();
    std::string payload_hash = sha256Hex("");
    
    std::map<std::string, std::string> headers;
    headers["Host"] = url.substr(url.find("://") + 3, url.find("/", url.find("://") + 3) - url.find("://") - 3);
    headers["x-amz-date"] = timestamp;
    headers["x-amz-content-sha256"] = payload_hash;
    
    if (!cached_session_token_.empty()) {
        headers["x-amz-security-token"] = cached_session_token_;
    }
    
    std::string scope = date + "/" + config_.region + "/s3/aws4_request";
    std::string canonical_uri = "/" + key;
    std::string canonical_request = createCanonicalRequest("DELETE", canonical_uri, "", headers, payload_hash);
    std::string string_to_sign = createStringToSign(canonical_request, timestamp, scope);
    std::string signature = calculateSignature(string_to_sign, date);
    
    std::string signed_headers;
    for (const auto& [k, v] : headers) {
        if (!signed_headers.empty()) signed_headers += ";";
        std::string lk = k;
        std::transform(lk.begin(), lk.end(), lk.begin(), ::tolower);
        signed_headers += lk;
    }
    
    headers["Authorization"] = "AWS4-HMAC-SHA256 Credential=" + cached_access_key_ + "/" + scope +
                               ", SignedHeaders=" + signed_headers +
                               ", Signature=" + signature;
    
    auto response = httpDelete(url, headers);
    
    if (response.status_code >= 200 && response.status_code < 300) {
        return StorageResult{true, ""};
    }
    
    return StorageResult{false, "DELETE failed: HTTP " + std::to_string(response.status_code)};
}

StorageResult CloudStorageBackend::removeMultiple(const std::vector<std::string>& keys) {
    // S3 supports batch delete, but for simplicity we do individual deletes
    std::vector<std::string> failed;
    
    for (const auto& key : keys) {
        auto result = remove(key);
        if (!result.success) {
            failed.push_back(key);
        }
    }
    
    if (failed.empty()) {
        return StorageResult{true, ""};
    }
    
    return StorageResult{false, "Failed to delete " + std::to_string(failed.size()) + " objects"};
}

StorageListResult CloudStorageBackend::list(
    const std::string& prefix,
    const std::string& delimiter,
    int max_keys,
    const std::string& continuation_token
) {
    ensureCredentials();
    
    std::string base_url = buildEndpointUrl("");
    std::string timestamp = getTimestamp();
    std::string date = getDateStamp();
    std::string payload_hash = sha256Hex("");
    
    // Build query string
    std::ostringstream query;
    query << "list-type=2";
    if (!prefix.empty()) {
        query << "&prefix=" << urlEncode(prefix);
    }
    if (!delimiter.empty()) {
        query << "&delimiter=" << urlEncode(delimiter);
    }
    query << "&max-keys=" << max_keys;
    if (!continuation_token.empty()) {
        query << "&continuation-token=" << urlEncode(continuation_token);
    }
    
    std::string url = base_url + "?" + query.str();
    
    std::map<std::string, std::string> headers;
    headers["Host"] = base_url.substr(base_url.find("://") + 3, base_url.find("/", base_url.find("://") + 3) - base_url.find("://") - 3);
    headers["x-amz-date"] = timestamp;
    headers["x-amz-content-sha256"] = payload_hash;
    
    if (!cached_session_token_.empty()) {
        headers["x-amz-security-token"] = cached_session_token_;
    }
    
    std::string scope = date + "/" + config_.region + "/s3/aws4_request";
    std::string canonical_uri = "/";
    std::string canonical_request = createCanonicalRequest("GET", canonical_uri, query.str(), headers, payload_hash);
    std::string string_to_sign = createStringToSign(canonical_request, timestamp, scope);
    std::string signature = calculateSignature(string_to_sign, date);
    
    std::string signed_headers;
    for (const auto& [k, v] : headers) {
        if (!signed_headers.empty()) signed_headers += ";";
        std::string lk = k;
        std::transform(lk.begin(), lk.end(), lk.begin(), ::tolower);
        signed_headers += lk;
    }
    
    headers["Authorization"] = "AWS4-HMAC-SHA256 Credential=" + cached_access_key_ + "/" + scope +
                               ", SignedHeaders=" + signed_headers +
                               ", Signature=" + signature;
    
    auto response = httpGet(url, headers);
    
    StorageListResult result;
    
    if (response.status_code != 200) {
        return result;
    }
    
    // Parse XML response (simplified)
    std::regex key_regex("<Key>([^<]+)</Key>");
    std::regex size_regex("<Size>([^<]+)</Size>");
    std::regex truncated_regex("<IsTruncated>([^<]+)</IsTruncated>");
    std::regex token_regex("<NextContinuationToken>([^<]+)</NextContinuationToken>");
    std::regex prefix_regex("<CommonPrefixes><Prefix>([^<]+)</Prefix></CommonPrefixes>");
    
    std::smatch match;
    std::string body = response.body;
    
    // Check truncation
    if (std::regex_search(body, match, truncated_regex)) {
        result.is_truncated = (match[1].str() == "true");
    }
    
    // Get continuation token
    if (std::regex_search(body, match, token_regex)) {
        result.continuation_token = match[1].str();
    }
    
    // Extract objects
    std::regex contents_regex("<Contents>(.*?)</Contents>");
    auto contents_begin = std::sregex_iterator(body.begin(), body.end(), contents_regex);
    auto contents_end = std::sregex_iterator();
    
    for (auto i = contents_begin; i != contents_end; ++i) {
        std::string content_xml = (*i)[1].str();
        StorageObjectInfo info;
        
        if (std::regex_search(content_xml, match, key_regex)) {
            info.key = match[1].str();
        }
        if (std::regex_search(content_xml, match, size_regex)) {
            info.size = std::stoull(match[1].str());
        }
        
        result.objects.push_back(info);
    }
    
    // Extract common prefixes
    auto prefix_begin = std::sregex_iterator(body.begin(), body.end(), prefix_regex);
    for (auto i = prefix_begin; i != std::sregex_iterator(); ++i) {
        result.common_prefixes.push_back((*i)[1].str());
    }
    
    return result;
}

std::vector<StorageObjectInfo> CloudStorageBackend::listAll(const std::string& prefix) {
    std::vector<StorageObjectInfo> all_objects;
    std::string token;
    
    do {
        auto result = list(prefix, "", 1000, token);
        all_objects.insert(all_objects.end(), result.objects.begin(), result.objects.end());
        token = result.continuation_token;
    } while (!token.empty());
    
    return all_objects;
}

StorageResult CloudStorageBackend::copy(const std::string& source_key, const std::string& dest_key) {
    // For cloud storage, we need to do a server-side copy
    // This requires the x-amz-copy-source header
    ensureCredentials();
    
    std::string url = buildEndpointUrl(dest_key);
    std::string timestamp = getTimestamp();
    std::string date = getDateStamp();
    std::string payload_hash = sha256Hex("");
    
    std::map<std::string, std::string> headers;
    headers["Host"] = url.substr(url.find("://") + 3, url.find("/", url.find("://") + 3) - url.find("://") - 3);
    headers["x-amz-date"] = timestamp;
    headers["x-amz-content-sha256"] = payload_hash;
    headers["x-amz-copy-source"] = "/" + config_.bucket + "/" + urlEncode(source_key, false);
    
    if (!cached_session_token_.empty()) {
        headers["x-amz-security-token"] = cached_session_token_;
    }
    
    std::string scope = date + "/" + config_.region + "/s3/aws4_request";
    std::string canonical_uri = "/" + dest_key;
    std::string canonical_request = createCanonicalRequest("PUT", canonical_uri, "", headers, payload_hash);
    std::string string_to_sign = createStringToSign(canonical_request, timestamp, scope);
    std::string signature = calculateSignature(string_to_sign, date);
    
    std::string signed_headers;
    for (const auto& [k, v] : headers) {
        if (!signed_headers.empty()) signed_headers += ";";
        std::string lk = k;
        std::transform(lk.begin(), lk.end(), lk.begin(), ::tolower);
        signed_headers += lk;
    }
    
    headers["Authorization"] = "AWS4-HMAC-SHA256 Credential=" + cached_access_key_ + "/" + scope +
                               ", SignedHeaders=" + signed_headers +
                               ", Signature=" + signature;
    
    auto response = httpPut(url, "", headers);
    
    if (response.status_code >= 200 && response.status_code < 300) {
        return StorageResult{true, ""};
    }
    
    return StorageResult{false, "COPY failed: HTTP " + std::to_string(response.status_code)};
}

StorageResult CloudStorageBackend::move(const std::string& source_key, const std::string& dest_key) {
    auto copy_result = copy(source_key, dest_key);
    if (!copy_result.success) {
        return copy_result;
    }
    
    return remove(source_key);
}

std::optional<std::string> CloudStorageBackend::getPresignedUrl(
    const std::string& key,
    int expiry_seconds,
    bool for_upload
) {
    ensureCredentials();
    
    std::string method = for_upload ? "PUT" : "GET";
    std::string timestamp = getTimestamp();
    std::string date = getDateStamp();
    std::string scope = date + "/" + config_.region + "/s3/aws4_request";
    
    // Build canonical query string for presigned URL
    std::ostringstream query;
    query << "X-Amz-Algorithm=AWS4-HMAC-SHA256";
    query << "&X-Amz-Credential=" << urlEncode(cached_access_key_ + "/" + scope);
    query << "&X-Amz-Date=" << timestamp;
    query << "&X-Amz-Expires=" << expiry_seconds;
    query << "&X-Amz-SignedHeaders=host";
    
    if (!cached_session_token_.empty()) {
        query << "&X-Amz-Security-Token=" << urlEncode(cached_session_token_);
    }
    
    std::string canonical_uri = "/" + key;
    std::string payload_hash = "UNSIGNED-PAYLOAD";
    
    std::map<std::string, std::string> headers;
    std::string base_url = buildEndpointUrl("");
    headers["host"] = base_url.substr(base_url.find("://") + 3, base_url.find("/", base_url.find("://") + 3) - base_url.find("://") - 3);
    
    std::string canonical_request = createCanonicalRequest(method, canonical_uri, query.str(), headers, payload_hash);
    std::string string_to_sign = createStringToSign(canonical_request, timestamp, scope);
    std::string signature = calculateSignature(string_to_sign, date);
    
    std::string presigned_url = buildEndpointUrl(key) + "?" + query.str() + "&X-Amz-Signature=" + signature;
    
    return presigned_url;
}

StorageResult CloudStorageBackend::writeWithProgress(
    const std::string& key,
    const std::string& content,
    ProgressCallback progress,
    const std::string& content_type
) {
    // For now, just call write - progress tracking would need multipart upload
    if (progress) {
        progress(0, content.size());
    }
    
    auto result = write(key, content, content_type, {});
    
    if (progress && result.success) {
        progress(content.size(), content.size());
    }
    
    return result;
}

std::optional<std::string> CloudStorageBackend::readWithProgress(
    const std::string& key,
    ProgressCallback progress
) {
    // For now, just call read - progress tracking would need range requests
    auto info = head(key);
    
    if (progress && info) {
        progress(0, info->size);
    }
    
    auto result = read(key);
    
    if (progress && result) {
        progress(result->size(), result->size());
    }
    
    return result;
}

void CloudStorageBackend::refreshCredentials() {
    credential_expiry_ = std::chrono::system_clock::time_point{};
    ensureCredentials();
}

// =============================================================================
// Factory Methods
// =============================================================================

std::unique_ptr<StorageBackend> StorageBackend::create(const StorageConfig& config) {
    if (config.provider == CloudProvider::Local) {
        return std::make_unique<LocalStorageBackend>(config.base_path);
    }
    
    return std::make_unique<CloudStorageBackend>(config);
}

std::unique_ptr<StorageBackend> StorageBackend::createFromEnv() {
    StorageConfig config;
    
    const char* provider_str = std::getenv("AIPR_STORAGE_PROVIDER");
    std::string provider = provider_str ? provider_str : "local";
    
    if (provider == "local") {
        config.provider = CloudProvider::Local;
        const char* path = std::getenv("AIPR_STORAGE_PATH");
        config.base_path = path ? path : "/var/lib/aipr/storage";
    } else if (provider == "s3" || provider == "aws") {
        config.provider = CloudProvider::AWS;
        
        const char* bucket = std::getenv("AIPR_STORAGE_BUCKET");
        const char* region = std::getenv("AIPR_STORAGE_REGION");
        const char* endpoint = std::getenv("AIPR_STORAGE_ENDPOINT");
        const char* access_key = std::getenv("AWS_ACCESS_KEY_ID");
        const char* secret_key = std::getenv("AWS_SECRET_ACCESS_KEY");
        const char* session_token = std::getenv("AWS_SESSION_TOKEN");
        
        config.bucket = bucket ? bucket : "";
        config.region = region ? region : "us-east-1";
        config.endpoint_url = endpoint ? endpoint : "";
        config.access_key = access_key ? access_key : "";
        config.secret_key = secret_key ? secret_key : "";
        config.session_token = session_token ? session_token : "";
        
        // Check for IRSA
        if (std::getenv("AWS_WEB_IDENTITY_TOKEN_FILE")) {
            config.use_irsa = true;
        }
    } else if (provider == "gcs" || provider == "gcp") {
        config.provider = CloudProvider::GCP;
        
        const char* bucket = std::getenv("AIPR_STORAGE_BUCKET");
        config.bucket = bucket ? bucket : "";
        config.use_workload_identity = true;
    } else if (provider == "azure") {
        config.provider = CloudProvider::Azure;
        
        const char* bucket = std::getenv("AIPR_STORAGE_BUCKET");  // container
        const char* account = std::getenv("AZURE_STORAGE_ACCOUNT");
        const char* key = std::getenv("AZURE_STORAGE_KEY");
        const char* sas = std::getenv("AZURE_STORAGE_SAS_TOKEN");
        
        config.bucket = bucket ? bucket : "";
        config.azure_account_name = account ? account : "";
        config.azure_account_key = key ? key : "";
        config.azure_sas_token = sas ? sas : "";
        config.use_azure_ad = (std::getenv("AZURE_CLIENT_ID") != nullptr);
    } else if (provider == "oci") {
        config.provider = CloudProvider::OCI;
        
        const char* bucket = std::getenv("AIPR_STORAGE_BUCKET");
        const char* region = std::getenv("OCI_REGION");
        const char* namespace_str = std::getenv("OCI_NAMESPACE");
        const char* tenancy = std::getenv("OCI_TENANCY");
        const char* user = std::getenv("OCI_USER");
        const char* fingerprint = std::getenv("OCI_FINGERPRINT");
        const char* key_file = std::getenv("OCI_KEY_FILE");
        
        config.bucket = bucket ? bucket : "";
        config.region = region ? region : "";
        config.oci_namespace = namespace_str ? namespace_str : "";
        config.oci_tenancy = tenancy ? tenancy : "";
        config.oci_user = user ? user : "";
        config.oci_fingerprint = fingerprint ? fingerprint : "";
        config.oci_key_file = key_file ? key_file : "";
    } else if (provider == "minio") {
        config.provider = CloudProvider::MinIO;
        
        const char* bucket = std::getenv("AIPR_STORAGE_BUCKET");
        const char* endpoint = std::getenv("AIPR_STORAGE_ENDPOINT");
        const char* access_key = std::getenv("MINIO_ACCESS_KEY");
        const char* secret_key = std::getenv("MINIO_SECRET_KEY");
        
        config.bucket = bucket ? bucket : "";
        config.endpoint_url = endpoint ? endpoint : "http://localhost:9000";
        config.access_key = access_key ? access_key : "";
        config.secret_key = secret_key ? secret_key : "";
        config.region = "us-east-1";  // MinIO default
    }
    
    return create(config);
}

} // namespace aipr
