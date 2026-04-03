/**
 * AI PR Reviewer - Engine Implementation
 *
 * Provides EngineConfig::load() and Engine::create() factory.
 * Wires the public Engine API to the TMS cognitive memory system,
 * indexer, retriever, and heuristic review signals.
 */

#include "engine_api.h"
#include "review_signals.h"
#include "logging.h"
#include "metrics.h"
#include "version.h"
#include "config_validator.h"
#include "storage_migration.h"
#include "tms/tms_memory_system.h"
#include "tms/tms_types.h"
#include "tms/repo_parser.h"
#include "knowledge_graph.h"
#include "tms/embedding_engine.h"
#include "tms/multimodal_embedder.h"
#include <nlohmann/json.hpp>
#include <fstream>
#include <sstream>
#include <regex>
#include <stdexcept>
#include <filesystem>
#include <chrono>
#include <algorithm>
#include <cstdlib>
#include <iostream>
#include <unordered_set>
#include <mutex>

// Platform-specific headers for system diagnostics
#ifdef __linux__
  #include <sys/sysinfo.h>
  #include <sys/statvfs.h>
#elif defined(__APPLE__)
  #include <mach/mach.h>
  #include <sys/mount.h>
  #include <sys/param.h>
#elif defined(_WIN32)
  #define WIN32_LEAN_AND_MEAN
  #include <windows.h>
#endif

namespace fs = std::filesystem;
using json = nlohmann::json;

namespace aipr {

// =============================================================================
// EngineConfig::load  —  supports both flat key:value and JSON config files
// =============================================================================

EngineConfig EngineConfig::load(const std::string& config_path) {
    EngineConfig config;

    std::ifstream file(config_path);
    if (!file.is_open()) {
        throw std::runtime_error("Cannot open config file: " + config_path);
    }

    // Peek first non-whitespace character to decide format
    std::string content((std::istreambuf_iterator<char>(file)),
                         std::istreambuf_iterator<char>());

    size_t first = content.find_first_not_of(" \t\r\n");
    bool is_json = (first != std::string::npos && content[first] == '{');

    if (is_json) {
        // ---- JSON format ----
        try {
            auto j = json::parse(content);

            auto str = [&](const char* key, std::string& out) {
                if (j.contains(key) && j[key].is_string()) out = j[key].get<std::string>();
            };
            auto num = [&](const char* key, size_t& out) {
                if (j.contains(key) && j[key].is_number()) out = j[key].get<size_t>();
            };
            auto flt = [&](const char* key, float& out) {
                if (j.contains(key) && j[key].is_number()) out = j[key].get<float>();
            };
            auto bl = [&](const char* key, bool& out) {
                if (j.contains(key) && j[key].is_boolean()) out = j[key].get<bool>();
            };

            str("storage_path",       config.storage_path);
            str("config_profile",     config.config_profile);
            num("max_file_size_kb",   config.max_file_size_kb);
            num("chunk_size",         config.chunk_size);
            num("chunk_overlap",      config.chunk_overlap);
            bl ("enable_ast_chunking",config.enable_ast_chunking);
            num("top_k",             config.top_k);
            flt("lexical_weight",    config.lexical_weight);
            flt("vector_weight",     config.vector_weight);
            num("graph_expand_depth",config.graph_expand_depth);
            str("embed_endpoint",    config.embed_endpoint);
            str("embed_api_key_env", config.embed_api_key_env);
            str("embed_model",       config.embed_model);
            num("embed_dimensions",  config.embed_dimensions);
            num("embed_batch_size",  config.embed_batch_size);
            num("embed_timeout_seconds", config.embed_timeout_seconds);
            str("onnx_model_path",   config.onnx_model_path);
            str("onnx_tokenizer_path", config.onnx_tokenizer_path);
            str("onnx_model_name",   config.onnx_model_name);

            if (j.contains("embed_provider")) {
                auto p = j["embed_provider"].get<std::string>();
                if (p == "HTTP" || p == "http")            config.embed_provider = EmbedProvider::HTTP;
                else if (p == "LOCAL_ONNX" || p == "onnx") config.embed_provider = EmbedProvider::LOCAL_ONNX;
                else                                       config.embed_provider = EmbedProvider::CUSTOM;
            }
        } catch (const json::exception& e) {
            throw std::runtime_error("Failed to parse JSON config: " + std::string(e.what()));
        }
    } else {
        // ---- Flat key:value format (YAML-like) ----
        std::istringstream stream(content);
        std::string line;
        while (std::getline(stream, line)) {
            if (line.empty() || line[0] == '#') continue;

            auto pos = line.find(':');
            if (pos == std::string::npos) continue;

            std::string key = line.substr(0, pos);
            std::string value = line.substr(pos + 1);

            auto trim = [](std::string& s) {
                size_t start = s.find_first_not_of(" \t\r\n");
                size_t end   = s.find_last_not_of(" \t\r\n");
                s = (start == std::string::npos) ? "" : s.substr(start, end - start + 1);
            };
            trim(key);
            trim(value);

            if      (key == "storage_path")       config.storage_path = value;
            else if (key == "config_profile")     config.config_profile = value;
            else if (key == "max_file_size_kb")   config.max_file_size_kb = std::stoull(value);
            else if (key == "chunk_size")         config.chunk_size = std::stoull(value);
            else if (key == "chunk_overlap")      config.chunk_overlap = std::stoull(value);
            else if (key == "enable_ast_chunking")config.enable_ast_chunking = (value == "true");
            else if (key == "top_k")              config.top_k = std::stoull(value);
            else if (key == "lexical_weight")     config.lexical_weight = std::stof(value);
            else if (key == "vector_weight")      config.vector_weight = std::stof(value);
            else if (key == "graph_expand_depth") config.graph_expand_depth = std::stoull(value);
            else if (key == "embed_endpoint")     config.embed_endpoint = value;
            else if (key == "embed_api_key_env")  config.embed_api_key_env = value;
            else if (key == "embed_model")        config.embed_model = value;
            else if (key == "embed_dimensions")   config.embed_dimensions = std::stoull(value);
            else if (key == "embed_batch_size")   config.embed_batch_size = std::stoull(value);
            else if (key == "embed_timeout_seconds") config.embed_timeout_seconds = std::stoull(value);
            else if (key == "onnx_model_path")    config.onnx_model_path = value;
            else if (key == "onnx_tokenizer_path")config.onnx_tokenizer_path = value;
            else if (key == "onnx_model_name")    config.onnx_model_name = value;
            else if (key == "embed_provider") {
                if      (value == "HTTP" || value == "http")            config.embed_provider = EmbedProvider::HTTP;
                else if (value == "LOCAL_ONNX" || value == "onnx")      config.embed_provider = EmbedProvider::LOCAL_ONNX;
                else                                                    config.embed_provider = EmbedProvider::CUSTOM;
            }
        }
    }

    return config;
}

// =============================================================================
// Helpers
// =============================================================================

static std::string currentTimestamp() {
    auto now  = std::chrono::system_clock::now();
    auto time = std::chrono::system_clock::to_time_t(now);
    std::ostringstream ss;
    ss << std::put_time(std::gmtime(&time), "%Y-%m-%dT%H:%M:%SZ");
    return ss.str();
}

// Convert TMS RetrievedChunk -> Engine ContextChunk
static ContextChunk toContextChunk(const tms::RetrievedChunk& rc) {
    ContextChunk cc;
    cc.id             = rc.chunk.id;
    cc.file_path      = rc.chunk.file_path;
    cc.start_line     = static_cast<size_t>(rc.chunk.start_line);
    cc.end_line       = static_cast<size_t>(rc.chunk.end_line);
    cc.content        = rc.chunk.content;
    cc.language       = rc.chunk.language;
    cc.symbols        = rc.chunk.symbols;
    cc.dependencies   = rc.chunk.dependencies;
    cc.type           = rc.chunk.type;
    cc.relevance_score = rc.combined_score;
    return cc;
}

// Parse unified diff into ParsedDiff
static ParsedDiff parseDiff(const std::string& diff_text) {
    // Inline DiffParser logic (matches diff_parser.cpp)
    ParsedDiff result;
    std::istringstream stream(diff_text);
    std::string line;
    DiffHunk current_hunk;
    FileInfo current_file;
    bool in_hunk = false;
    std::string current_new_path;

    std::regex diff_header(R"(^diff --git a/(.+) b/(.+)$)");
    std::regex hunk_header(R"(^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$)");
    std::regex new_file_mode(R"(^new file mode)");
    std::regex deleted_file_mode(R"(^deleted file mode)");
    std::regex rename_from(R"(^rename from (.+)$)");

    while (std::getline(stream, line)) {
        std::smatch match;

        if (std::regex_match(line, match, diff_header)) {
            if (in_hunk && !current_hunk.content.empty())
                result.hunks.push_back(current_hunk);
            if (!current_new_path.empty()) {
                current_file.path = current_new_path;
                result.changed_files.push_back(current_file);
            }
            current_new_path = match[2].str();
            current_file = FileInfo();
            current_file.path = current_new_path;
            current_file.change_type = ChangeType::Modified;
            current_hunk = DiffHunk();
            in_hunk = false;
            continue;
        }
        if (std::regex_search(line, new_file_mode)) {
            current_file.change_type = ChangeType::Added;
            continue;
        }
        if (std::regex_search(line, deleted_file_mode)) {
            current_file.change_type = ChangeType::Deleted;
            continue;
        }
        if (std::regex_search(line, rename_from)) {
            current_file.change_type = ChangeType::Renamed;
            continue;
        }
        if (std::regex_match(line, match, hunk_header)) {
            if (in_hunk && !current_hunk.content.empty())
                result.hunks.push_back(current_hunk);
            current_hunk = DiffHunk();
            current_hunk.file_path  = current_new_path;
            current_hunk.old_start  = std::stoul(match[1].str());
            current_hunk.old_lines  = match[2].matched ? std::stoul(match[2].str()) : 1;
            current_hunk.new_start  = std::stoul(match[3].str());
            current_hunk.new_lines  = match[4].matched ? std::stoul(match[4].str()) : 1;
            current_hunk.content    = line + "\n";
            in_hunk = true;
            continue;
        }
        if (in_hunk) {
            current_hunk.content += line + "\n";
            if (!line.empty()) {
                if (line[0] == '+') {
                    current_hunk.added_lines.push_back(line.substr(1));
                    result.total_additions++;
                } else if (line[0] == '-') {
                    current_hunk.removed_lines.push_back(line.substr(1));
                    result.total_deletions++;
                }
            }
        }
    }
    if (in_hunk && !current_hunk.content.empty())
        result.hunks.push_back(current_hunk);
    if (!current_new_path.empty()) {
        current_file.path = current_new_path;
        result.changed_files.push_back(current_file);
    }
    return result;
}

// =============================================================================
// Helper: detect remote URLs and shell-escape strings
// =============================================================================

static bool isRemoteURL(const std::string& path) {
    return path.rfind("http://", 0) == 0 ||
           path.rfind("https://", 0) == 0 ||
           path.rfind("git@", 0) == 0 ||
           path.rfind("ssh://", 0) == 0;
}

static std::string shellEscape(const std::string& s) {
    std::string escaped = "'";
    for (char c : s) {
        if (c == '\'') {
            escaped += "'\\''";
        } else {
            escaped += c;
        }
    }
    escaped += "'";
    return escaped;
}

// =============================================================================
// EngineImpl  —  production implementation backed by TMS
// =============================================================================

class EngineImpl : public Engine {
public:
    explicit EngineImpl(const EngineConfig& config)
        : config_(config)
    {
        // Build TMS config from EngineConfig
        tms::TMSConfig tms_cfg;
        tms_cfg.embedding_dimension   = config.embed_dimensions;
        tms_cfg.embedding_model       = config.embed_model;
        tms_cfg.storage_path          = config.storage_path + "/tms";
        tms_cfg.ltm_capacity          = 10000000;
        tms_cfg.ltm_default_top_k     = static_cast<int>(config.top_k);
        tms_cfg.vram_budget_gb        = 4.0f;
        tms_cfg.enable_adaptive_strategy = true;

        // Embedding backend config
        switch (config.embed_provider) {
            case EmbedProvider::LOCAL_ONNX:
                tms_cfg.embedding_backend = "onnx";
                tms_cfg.onnx_model_path = config.onnx_model_path;
                tms_cfg.onnx_tokenizer_path = config.onnx_tokenizer_path;
                break;
            case EmbedProvider::HTTP:
                tms_cfg.embedding_backend = "http";
                tms_cfg.embed_api_endpoint = config.embed_endpoint;
                break;
            default:
                tms_cfg.embedding_backend = "mock";
                break;
        }

        tms_ = std::make_unique<tms::TMSMemorySystem>(tms_cfg);
        tms_->initialize();

        // Create review signals with all built-in heuristic checks
        review_signals_ = ReviewSignals::createWithDefaults();
    }

    // =========================================================================
    // Embedding Runtime Configuration
    // =========================================================================

    void configureEmbedding(
        const std::string& provider,
        const std::string& endpoint,
        const std::string& model,
        const std::string& api_key,
        size_t dimensions) override
    {
        if (provider.empty()) return; // no-op if not specified

        std::cerr << "[ENGINE] configureEmbedding: provider=" << provider
                  << " model=" << model
                  << " endpoint=" << endpoint
                  << " dims=" << dimensions << std::endl;

        tms_->reconfigureEmbedding(provider, endpoint, model, api_key, dimensions);
    }

    // =========================================================================
    // Storage Path
    // =========================================================================

    std::string getStoragePath() const override {
        return config_.storage_path;
    }

    // =========================================================================
    // Cross-Repo helpers (used by CrossRepoServiceImpl)
    // =========================================================================

    std::string getRepoPath(const std::string& repo_id) const override {
        std::string path = config_.storage_path + "/repos/" + repo_id;
        if (fs::exists(path)) return path;
        return "";
    }

    std::vector<ContextChunk> getCodeChunksForRepo(
        const std::string& repo_id) const override
    {
        // Retrieve all TMS code chunks for this repo and convert to
        // the public ContextChunk type.
        auto& ltm = const_cast<tms::LTMFaiss&>(tms_->ltm());
        auto stats = ltm.getStats();
        auto it = stats.chunks_per_repo.find(repo_id);
        if (it == stats.chunks_per_repo.end() || it->second == 0) {
            return {};
        }

        // Use TMS search with a broad query to retrieve all chunks.
        // The repo_filter ensures we only get chunks from this repo.
        // We ask for a large top_k to get as many as possible.
        tms::TMSQuery tms_q;
        tms_q.query_text  = "*";
        tms_q.repo_filter = repo_id;
        tms_q.session_id  = "cross_repo_scan_" + repo_id;

        tms::TMSResponse resp = tms_->forward(tms_q);

        std::vector<ContextChunk> results;
        results.reserve(resp.attention_output.fused_chunks.size());

        for (const auto& rc : resp.attention_output.fused_chunks) {
            results.push_back(toContextChunk(rc));
        }
        return results;
    }

    // =========================================================================
    // Clone Token (transient, per-request)
    // =========================================================================

    void setCloneToken(const std::string& token) override {
        std::lock_guard<std::mutex> lock(clone_token_mu_);
        clone_token_ = token;
    }

    // =========================================================================
    // Indexing
    // =========================================================================

    IndexStats indexRepository(
        const std::string& repo_id,
        const std::string& repo_path,
        ProgressCallback progress) override
    {
        return indexRepositoryWithAction(repo_id, repo_path, "index", "", progress);
    }

    IndexStats indexRepositoryWithAction(
        const std::string& repo_id,
        const std::string& repo_path,
        const std::string& action,       // "index" | "reindex" | "reclone"
        const std::string& target_branch, // optional branch to checkout
        ProgressCallback progress)
    {
        auto start = std::chrono::steady_clock::now();

        // Consume the transient clone token (if any) — clear after read.
        std::string token;
        {
            std::lock_guard<std::mutex> lock(clone_token_mu_);
            token = std::move(clone_token_);
            clone_token_.clear();
        }

        // Determine the local path to index.
        // If repo_path is a URL, git-clone it first — unless action says otherwise.
        std::string local_path = repo_path;
        bool cloned = false;

        if (isRemoteURL(repo_path)) {
            local_path = config_.storage_path + "/repos/" + repo_id;

            if (action == "reindex") {
                // ── Reindex mode: skip all git operations ───────────────────
                // Use existing local clone as-is. If it doesn't exist, fall
                // through to a normal clone so we don't fail on a missing dir.
                if (fs::exists(local_path)) {
                    std::cerr << "[ENGINE] reindex: using existing local clone at "
                              << local_path << std::endl;
                    if (progress) progress(10, 100, "Reindexing existing clone...");
                } else {
                    std::cerr << "[ENGINE] reindex: no local clone found, "
                              << "falling back to clone" << std::endl;
                    // Fall through to clone below
                }
            }

            if (action == "reclone") {
                // ── Reclone mode: force fresh clone ─────────────────────────
                if (fs::exists(local_path)) {
                    std::cerr << "[ENGINE] reclone: removing existing clone at "
                              << local_path << std::endl;
                    fs::remove_all(local_path);
                }
            }

            // Only perform git operations if local path doesn't exist yet
            // (reindex with existing files skips this entire block)
            if (!fs::exists(local_path)) {
                if (progress) progress(0, 100, "Cloning repository...");

                // Build the git auth argument.
                std::string git_auth_flag;
                if (!token.empty() && repo_path.rfind("https://", 0) == 0) {
                    git_auth_flag = " -c http.extraHeader=" +
                                    shellEscape("Authorization: Bearer " + token);
                }

                fs::create_directories(fs::path(local_path).parent_path());

                // Try unauthenticated first (works for public repos), then
                // fall back to token-authenticated clone if available.
                std::string clone_base = "git clone --depth 1 " +
                                         shellEscape(repo_path) + " " +
                                         shellEscape(local_path) + " 2>&1";
                int rc = std::system(clone_base.c_str());

                if (rc != 0 && !git_auth_flag.empty()) {
                    // Unauthenticated clone failed — retry with token
                    std::cerr << "[ENGINE] Unauthenticated clone failed, retrying with VCS token" << std::endl;
                    // Clean up partial clone
                    if (fs::exists(local_path)) fs::remove_all(local_path);
                    std::string auth_clone = "git" + git_auth_flag +
                                             " clone --depth 1 " + shellEscape(repo_path) +
                                             " " + shellEscape(local_path) + " 2>&1";
                    rc = std::system(auth_clone.c_str());
                }

                if (rc != 0) {
                    throw std::runtime_error("git clone failed for " + repo_path + " (exit code " + std::to_string(rc) + ")");
                }
                cloned = true;
                if (progress) progress(10, 100, "Repository cloned, scanning files...");
            } else if (action != "reindex") {
                // ── Default "index" mode with existing clone: pull updates ──
                if (progress) progress(0, 100, "Pulling latest changes...");

                std::string git_auth_flag;
                if (!token.empty() && repo_path.rfind("https://", 0) == 0) {
                    git_auth_flag = " -c http.extraHeader=" +
                                    shellEscape("Authorization: Bearer " + token);
                }

                // Try unauthenticated pull first, fall back to token
                std::string pull_cmd = "git -C " + shellEscape(local_path) +
                                       " pull --ff-only 2>&1";
                int rc = std::system(pull_cmd.c_str());

                if (rc != 0 && !git_auth_flag.empty()) {
                    std::cerr << "[ENGINE] Unauthenticated pull failed, retrying with VCS token" << std::endl;
                    pull_cmd = "git" + git_auth_flag + " -C " + shellEscape(local_path) +
                               " pull --ff-only 2>&1";
                    rc = std::system(pull_cmd.c_str());
                }

                if (rc != 0) {
                    std::cerr << "[ENGINE] pull failed (rc=" << rc
                              << "), continuing with existing files" << std::endl;
                }
                if (progress) progress(10, 100, "Repository updated, scanning files...");
            }

            // ── Branch checkout (if target_branch specified) ────────────────
            if (!target_branch.empty() && fs::exists(local_path)) {
                std::string git_auth_flag;
                if (!token.empty() && repo_path.rfind("https://", 0) == 0) {
                    git_auth_flag = " -c http.extraHeader=" +
                                    shellEscape("Authorization: Bearer " + token);
                }
                // Fetch the branch — try unauthenticated first, fall back to token
                std::string fetch_cmd = "git -C " + shellEscape(local_path) +
                                        " fetch origin " + shellEscape(target_branch) +
                                        " --depth 1 2>&1";
                int fetch_rc = std::system(fetch_cmd.c_str());

                if (fetch_rc != 0 && !git_auth_flag.empty()) {
                    fetch_cmd = "git" + git_auth_flag + " -C " + shellEscape(local_path) +
                                " fetch origin " + shellEscape(target_branch) +
                                " --depth 1 2>&1";
                    std::system(fetch_cmd.c_str());
                }

                std::string checkout_cmd = "git -C " + shellEscape(local_path) +
                                          " checkout " + shellEscape(target_branch) + " 2>&1";
                int rc = std::system(checkout_cmd.c_str());
                if (rc != 0) {
                    std::cerr << "[ENGINE] checkout of branch " << target_branch
                              << " failed (rc=" << rc << "), using current branch" << std::endl;
                }
            }
        }

        // Wrap the Engine ProgressCallback into the TMS progress callback
        auto tms_progress = [&](float pct, const std::string& status) {
            if (progress) {
                // If we cloned, offset the progress (10-100%)
                size_t current = cloned ? (10 + static_cast<size_t>(pct * 90))
                                        : static_cast<size_t>(pct * 100);
                progress(current, 100, status);
            }
        };

        std::cerr << "[ENGINE] indexRepository: local_path=" << local_path
                  << " repo_id=" << repo_id
                  << " exists=" << fs::exists(local_path) << std::endl;

        tms_->ingestRepository(local_path, repo_id, tms_progress);

        auto end = std::chrono::steady_clock::now();

        // Build stats from the TMS subsystem
        auto tms_stats = tms_->getStats();
        size_t chunk_count = tms_->ltm().getRepoChunkCount(repo_id);

        std::cerr << "[ENGINE] indexRepository complete: chunk_count=" << chunk_count
                  << " duration=" << std::chrono::duration_cast<std::chrono::seconds>(end - start).count() << "s" << std::endl;

        IndexStats stats;
        stats.repo_id         = repo_id;
        stats.index_version   = "1";
        stats.total_chunks    = chunk_count;
        stats.indexed_files   = chunk_count > 0 ? chunk_count / 5 : 0; // estimate
        stats.total_files     = stats.indexed_files;
        stats.total_symbols   = 0; // populated per-chunk
        stats.index_size_bytes = tms_stats.ltm_index_size_mb * 1024 * 1024;
        stats.last_updated    = currentTimestamp();
        stats.is_complete     = true;

        return stats;
    }

    IndexStats updateIndex(
        const std::string& repo_id,
        const std::vector<std::string>& changed_files,
        const std::string& /*base_sha*/,
        const std::string& /*head_sha*/) override
    {
        // Incremental update: remove old chunks for changed files then re-parse
        std::vector<std::string> deleted_files; // none — we keep all, just re-ingest changes
        tms_->updateRepository(repo_id, changed_files, deleted_files);

        return getIndexStats(repo_id);
    }

    IndexStats getIndexStats(const std::string& repo_id) override {
        auto tms_stats  = tms_->getStats();
        size_t chunk_count = tms_->ltm().getRepoChunkCount(repo_id);

        IndexStats stats;
        stats.repo_id         = repo_id;
        stats.index_version   = "1";
        stats.total_chunks    = chunk_count;
        stats.indexed_files   = chunk_count > 0 ? chunk_count / 5 : 0;
        stats.total_files     = stats.indexed_files;
        stats.index_size_bytes = tms_stats.ltm_index_size_mb * 1024 * 1024;
        stats.last_updated    = currentTimestamp();
        stats.is_complete     = (chunk_count > 0);
        return stats;
    }

    bool deleteIndex(const std::string& repo_id) override {
        tms_->removeRepository(repo_id);
        return true;
    }

    // =========================================================================
    // Retrieval
    // =========================================================================

    std::vector<ContextChunk> search(
        const std::string& repo_id,
        const std::string& query,
        size_t top_k) override
    {
        auto start = std::chrono::steady_clock::now();

        LOG_INFO("[ChatRAG] search: repo=" + repo_id +
                 " top_k=" + std::to_string(top_k) +
                 " query_len=" + std::to_string(query.size()));

        // Build TMS query
        tms::TMSQuery tms_q;
        tms_q.query_text  = query;
        tms_q.repo_filter = repo_id;
        tms_q.session_id  = "search_" + repo_id;

        tms::TMSResponse resp = tms_->forward(tms_q);

        auto search_end = std::chrono::steady_clock::now();
        auto search_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
            search_end - start).count();

        // Log TMS retrieval metrics
        LOG_INFO("[ChatRAG] TMS forward complete: " +
                 std::string("strategy=") + resp.compute_decision.reasoning +
                 " ltm_scanned=" + std::to_string(resp.ltm_items_scanned) +
                 " stm_scanned=" + std::to_string(resp.stm_items_scanned) +
                 " mtm_scanned=" + std::to_string(resp.mtm_items_scanned) +
                 " fused_chunks=" + std::to_string(resp.attention_output.fused_chunks.size()) +
                 " patterns_matched=" + std::to_string(resp.matched_patterns.size()) +
                 " tms_ms=" + std::to_string(search_ms));

        // Convert fused chunks to ContextChunks, respecting top_k
        std::vector<ContextChunk> results;
        results.reserve(std::min(top_k, resp.attention_output.fused_chunks.size()));

        size_t total_content_chars = 0;
        for (size_t i = 0; i < resp.attention_output.fused_chunks.size() && i < top_k; ++i) {
            auto cc = toContextChunk(resp.attention_output.fused_chunks[i]);
            total_content_chars += cc.content.size();
            results.push_back(std::move(cc));
        }

        LOG_INFO("[ChatRAG] search result: repo=" + repo_id +
                 " chunks_returned=" + std::to_string(results.size()) +
                 " total_content_chars=" + std::to_string(total_content_chars) +
                 " est_tokens=" + std::to_string(total_content_chars / 4) +
                 " total_ms=" + std::to_string(search_ms));

        // Log per-chunk details at DEBUG
        for (size_t i = 0; i < results.size(); ++i) {
            const auto& rc = resp.attention_output.fused_chunks[i];
            LOG_DEBUG("[ChatRAG]   chunk[" + std::to_string(i) + "]: " +
                      results[i].file_path + ":" +
                      std::to_string(results[i].start_line) + "-" +
                      std::to_string(results[i].end_line) +
                      " score=" + std::to_string(results[i].relevance_score) +
                      " (vec=" + std::to_string(rc.similarity_score) +
                      " lex=" + std::to_string(rc.lexical_score) +
                      " attn=" + std::to_string(rc.attention_weight) +
                      ") src=" + rc.memory_source +
                      " chars=" + std::to_string(results[i].content.size()));
        }

        return results;
    }

    SearchResult searchWithMeta(
        const std::string& repo_id,
        const std::string& query,
        size_t top_k) override
    {
        tms::TMSQuery tms_q;
        tms_q.query_text  = query;
        tms_q.repo_filter = repo_id;
        tms_q.session_id  = "search_" + repo_id;

        tms::TMSResponse resp = tms_->forward(tms_q);

        SearchResult sr;
        sr.graph_confidence = resp.graph_confidence;
        sr.graph_expanded_chunks = resp.graph_expanded_chunks;
        sr.requires_llm = resp.requires_llm;
        sr.max_retrieval_score = resp.max_retrieval_score;

        sr.chunks.reserve(std::min(top_k, resp.attention_output.fused_chunks.size()));
        for (size_t i = 0; i < resp.attention_output.fused_chunks.size() && i < top_k; ++i) {
            sr.chunks.push_back(toContextChunk(resp.attention_output.fused_chunks[i]));
        }
        return sr;
    }

    // =========================================================================
    // Review
    // =========================================================================

    ContextPack buildReviewContext(
        const std::string& repo_id,
        const std::string& diff,
        const std::string& pr_title,
        const std::string& pr_description) override
    {
        ContextPack pack;
        pack.repo_id        = repo_id;
        pack.pr_title       = pr_title;
        pack.pr_description = pr_description;
        pack.diff           = diff;

        // 1. Parse the diff to extract changed files and symbols
        ParsedDiff parsed = parseDiff(diff);

        // Build a search query from diff context
        std::ostringstream query_builder;
        query_builder << "Code review context for PR: " << pr_title << "\n";
        if (!pr_description.empty()) {
            query_builder << pr_description << "\n";
        }
        query_builder << "Changed files: ";
        for (const auto& f : parsed.changed_files) {
            query_builder << f.path << " ";
        }

        // 2. Search TMS for relevant context chunks
        tms::TMSQuery tms_q;
        tms_q.query_text  = query_builder.str();
        tms_q.repo_filter = repo_id;
        tms_q.session_id  = "review_" + repo_id;

        // Add changed files as hints
        for (const auto& f : parsed.changed_files) {
            tms_q.hint_files.push_back(f.path);
        }

        tms::TMSResponse resp = tms_->forward(tms_q);

        // Convert fused chunks
        for (const auto& rc : resp.attention_output.fused_chunks) {
            pack.context_chunks.push_back(toContextChunk(rc));
        }

        // 3. Build touched symbols list from diff hunks
        for (const auto& hunk : parsed.hunks) {
            // Create a basic touched-symbol entry per hunk
            TouchedSymbol ts;
            ts.symbol.file_path = hunk.file_path;
            ts.symbol.name      = hunk.file_path; // best-effort without AST
            ts.symbol.kind      = "file";
            ts.symbol.line      = hunk.new_start;
            ts.change_type      = ChangeType::Modified;
            ts.additions        = hunk.added_lines.size();
            ts.deletions        = hunk.removed_lines.size();
            pack.touched_symbols.push_back(ts);
        }

        // 4. Run heuristic checks and add warnings
        auto findings = review_signals_->runAllChecks(parsed);
        for (const auto& f : findings) {
            std::ostringstream warning;
            warning << "[" << severityToString(f.severity) << "] "
                    << f.message;
            if (!f.file_path.empty()) {
                warning << " (" << f.file_path;
                if (f.line > 0) warning << ":" << f.line;
                warning << ")";
            }
            pack.heuristic_warnings.push_back(warning.str());
        }

        return pack;
    }

    std::vector<HeuristicFinding> runHeuristics(
        const std::string& diff) override
    {
        ParsedDiff parsed = parseDiff(diff);
        return review_signals_->runAllChecks(parsed);
    }

    // =========================================================================
    // Utility
    // =========================================================================

    std::string getVersion() const override {
        return AIPR_VERSION_FULL;
    }

    DiagnosticResult runDiagnostics() override {
        DiagnosticResult result;
        result.engine_version = AIPR_VERSION_FULL;

        // Check TMS subsystem
        try {
            auto stats = tms_->getStats();
            result.checks_passed.push_back(
                "TMS initialized: " + std::to_string(stats.ltm_total_chunks) + " chunks in LTM");
            result.checks_passed.push_back(
                "MTM: " + std::to_string(stats.mtm_patterns) + " patterns, "
                + std::to_string(stats.mtm_strategies) + " strategies");
            result.checks_passed.push_back(
                "STM: " + std::to_string(stats.stm_active_sessions) + " active sessions");
        } catch (const std::exception& e) {
            result.checks_failed.push_back("TMS subsystem error: " + std::string(e.what()));
        }

        // Check storage
        if (!config_.storage_path.empty()) {
            if (fs::exists(config_.storage_path)) {
                result.checks_passed.push_back("Storage path accessible: " + config_.storage_path);
            } else {
                result.warnings.push_back("Storage path does not exist: " + config_.storage_path);
            }
        }

        // System resources — cross-platform
#ifdef __linux__
        struct sysinfo si;
        if (sysinfo(&si) == 0) {
            result.available_memory_mb = (si.freeram * si.mem_unit) / (1024 * 1024);
        }
        if (!config_.storage_path.empty() && fs::exists(config_.storage_path)) {
            struct statvfs sv;
            if (statvfs(config_.storage_path.c_str(), &sv) == 0) {
                result.available_disk_mb = (sv.f_bavail * sv.f_frsize) / (1024 * 1024);
            }
        }
  #if defined(__aarch64__) || defined(__ARM_ARCH)
        result.platform = "linux-arm64";
  #else
        result.platform = "linux-x86_64";
  #endif

#elif defined(__APPLE__)
        // macOS: use Mach APIs for memory, statfs for disk
        mach_port_t host = mach_host_self();
        vm_statistics64_data_t vm_stats;
        mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
        if (host_statistics64(host, HOST_VM_INFO64,
                              reinterpret_cast<host_info64_t>(&vm_stats), &count) == KERN_SUCCESS) {
            uint64_t free_bytes = (static_cast<uint64_t>(vm_stats.free_count)
                                 + static_cast<uint64_t>(vm_stats.inactive_count))
                                 * vm_page_size;
            result.available_memory_mb = free_bytes / (1024 * 1024);
        }
        if (!config_.storage_path.empty() && fs::exists(config_.storage_path)) {
            struct statfs sf;
            if (statfs(config_.storage_path.c_str(), &sf) == 0) {
                result.available_disk_mb =
                    (static_cast<uint64_t>(sf.f_bavail) * sf.f_bsize) / (1024 * 1024);
            }
        }
  #if defined(__aarch64__) || defined(__arm64__)
        result.platform = "darwin-arm64";
  #else
        result.platform = "darwin-x64";
  #endif

#elif defined(_WIN32)
        // Windows: GlobalMemoryStatusEx for memory
        MEMORYSTATUSEX memstat;
        memstat.dwLength = sizeof(memstat);
        if (GlobalMemoryStatusEx(&memstat)) {
            result.available_memory_mb =
                static_cast<size_t>(memstat.ullAvailPhys / (1024 * 1024));
        }
        if (!config_.storage_path.empty() && fs::exists(config_.storage_path)) {
            ULARGE_INTEGER free_bytes;
            // GetDiskFreeSpaceExA works on paths
            if (GetDiskFreeSpaceExA(config_.storage_path.c_str(),
                                    &free_bytes, nullptr, nullptr)) {
                result.available_disk_mb =
                    static_cast<size_t>(free_bytes.QuadPart / (1024 * 1024));
            }
        }
        result.platform = "windows-x64";
#endif

        // Review signals
        auto check_ids = review_signals_->getRegisteredChecks();
        result.checks_passed.push_back(
            "Review signals: " + std::to_string(check_ids.size()) + " heuristic checks registered");

        result.healthy = result.checks_failed.empty();
        return result;
    }

    // =========================================================================
    // Embedding Statistics
    // =========================================================================

    EmbedStats getEmbedStats(const std::string& repo_id) override {
        EmbedStats stats;

        // Model information
        auto& emb = tms_->embeddingEngine();
        stats.active_model = emb.activeModelName();
        stats.embedding_dimension = emb.activeDimension();
        auto emb_config = emb.getConfig();
        switch (emb_config.backend) {
            case tms::EmbeddingBackend::ONNX_RUNTIME: stats.backend_type = "onnx"; break;
            case tms::EmbeddingBackend::HTTP_API:      stats.backend_type = "http"; break;
            default:                                   stats.backend_type = "mock"; break;
        }

        // Index statistics
        auto tms_stats = tms_->getStats();
        auto ltm_stats = tms_->ltm().getStats();
        stats.total_chunks = repo_id.empty() ? ltm_stats.total_chunks
                                             : tms_->ltm().getRepoChunkCount(repo_id);
        stats.total_vectors = ltm_stats.index_vectors;
        stats.index_size_bytes = ltm_stats.memory_bytes;

        // KG statistics
        stats.kg_enabled = true; // config_.knowledge_graph_enabled would require TMS config access

        // Performance from metrics registry
        auto& reg = metrics::Registry::instance();
        auto embed_hist = reg.getHistogram(metrics::EMBED_LATENCY_S);
        stats.avg_embed_latency_ms = embed_hist.avg * 1000.0;

        auto search_hist = reg.getHistogram(metrics::SEARCH_LATENCY_S);
        stats.avg_search_latency_ms = search_hist.avg * 1000.0;

        stats.total_queries = tms_stats.total_queries;

        // Cache statistics
        auto cache_stats = emb.getCacheStats();
        stats.embed_cache_size = cache_stats.size;
        stats.embed_cache_hit_rate = cache_stats.hit_rate;

        // Confidence gate from metrics
        stats.llm_avoided_rate = reg.getGauge(metrics::LLM_AVOIDED_RATE);
        stats.avg_confidence_score = reg.getGauge(metrics::CONFIDENCE_GATE_COMBINED);
        stats.llm_avoided_count = static_cast<size_t>(reg.getCounter(metrics::LLM_AVOIDED_TOTAL));
        stats.llm_used_count = static_cast<size_t>(reg.getCounter(metrics::LLM_USED_TOTAL));

        // GraphRAG from metrics
        auto graph_hist = reg.getHistogram(metrics::GRAPH_TRAVERSAL_LATENCY_S);
        stats.avg_graph_expansion_ms = graph_hist.avg * 1000.0;
        stats.avg_graph_expanded_chunks = reg.getGauge(metrics::GRAPH_EXPANDED_CHUNKS);

        // Hot-swap
        stats.model_swaps_total = static_cast<size_t>(reg.getCounter(metrics::MODEL_SWAPS_TOTAL));

        // Merkle cache
        stats.merkle_cache_hit_rate = reg.getGauge(metrics::MERKLE_CACHE_HIT_RATE);
        stats.merkle_cached_files = static_cast<size_t>(reg.getGauge(metrics::MERKLE_FILES_SKIPPED));

        // Multi-vector dual-resolution index
        if (auto* mv = tms_->multiVector()) {
            auto mv_stats = mv->getStats();
            stats.multi_vector_enabled = mv_stats.dual_index_active;
            stats.coarse_dimension = mv_stats.coarse_dimension;
            stats.fine_dimension = mv_stats.fine_dimension;
            stats.coarse_index_vectors = mv_stats.coarse_index_vectors;
            stats.fine_index_vectors = mv_stats.fine_index_vectors;
        }

        return stats;
    }

    // =========================================================================
    // Knowledge Graph — Intra-Repo File Map
    // =========================================================================

    RepoFileMap getRepoFileMap(
        const std::string& repo_id,
        const std::vector<std::string>& node_types,
        const std::vector<std::string>& edge_types,
        size_t max_nodes) override
    {
        RepoFileMap result;

        auto* kg = tms_->knowledgeGraph();
        if (!kg || !kg->isOpen()) return result;

        // Build filter sets (empty = accept all)
        std::unordered_set<std::string> nt_filter(node_types.begin(), node_types.end());
        std::unordered_set<std::string> et_filter(edge_types.begin(), edge_types.end());

        // Fetch all nodes for the repo
        auto raw_nodes = kg->getNodes(repo_id);

        // Filter by node type first
        std::vector<decltype(raw_nodes)::value_type> filtered_raw;
        for (auto& n : raw_nodes) {
            if (!nt_filter.empty() && nt_filter.find(n.node_type) == nt_filter.end())
                continue;
            filtered_raw.push_back(std::move(n));
        }

        // Track total before capping
        const size_t full_node_count = filtered_raw.size();

        // If we need to cap, rank nodes by connectivity (degree) so the
        // most-connected nodes survive.  This keeps the graph useful.
        if (max_nodes > 0 && filtered_raw.size() > max_nodes) {
            // Quick degree count from neighbors()
            std::unordered_map<std::string, size_t> degree;
            for (const auto& n : filtered_raw) {
                auto edges = kg->neighbors(n.id);
                degree[n.id] = edges.size();
            }
            // Partial sort: keep top max_nodes by degree
            std::partial_sort(
                filtered_raw.begin(),
                filtered_raw.begin() + static_cast<ptrdiff_t>(max_nodes),
                filtered_raw.end(),
                [&](const auto& a, const auto& b) {
                    return degree[a.id] > degree[b.id];
                });
            filtered_raw.resize(max_nodes);
            result.truncated = true;
        }

        // Build final node list + id set for edge filtering
        std::unordered_set<std::string> node_id_set;
        for (const auto& n : filtered_raw) {
            result.nodes.push_back({
                n.id, n.node_type, n.name, n.file_path, n.language, n.metadata
            });
            node_id_set.insert(n.id);
        }

        // Fetch edges by iterating over nodes and getting neighbors
        // Use a set to deduplicate edges (since neighbors() returns edges from both sides)
        std::unordered_set<int64_t> seen_edge_ids;
        for (const auto& n : result.nodes) {
            auto edges = kg->neighbors(n.id);
            for (const auto& e : edges) {
                if (e.repo_id != repo_id) continue;
                if (!et_filter.empty() && et_filter.find(e.edge_type) == et_filter.end())
                    continue;
                // Only include edges where both endpoints survived the cap
                if (node_id_set.find(e.src_id) == node_id_set.end() ||
                    node_id_set.find(e.dst_id) == node_id_set.end())
                    continue;
                if (seen_edge_ids.insert(e.id).second) {
                    FileMapEdge fe;
                    fe.id        = e.id;
                    fe.src_id    = e.src_id;
                    fe.dst_id    = e.dst_id;
                    fe.edge_type = e.edge_type;
                    fe.weight    = e.weight;
                    result.edges.push_back(std::move(fe));
                }
            }
        }

        result.total_nodes = full_node_count;
        result.total_edges = result.edges.size();
        return result;
    }

    // =========================================================================
    // Multimodal Embedding
    // =========================================================================

    bool embedBinaryAsset(
        const std::string& repo_id,
        const std::vector<uint8_t>& data,
        const std::string& mime_type,
        const std::string& file_name,
        const std::string& asset_id) override
    {
        ensureMultimodalEmbedder();
        if (!mm_embedder_) return false;

        auto at = tms::assetTypeFromString(mime_type);
        auto modality = tms::modalityForAsset(at);
        if (modality == tms::EmbeddingModality::TEXT) {
            // Text assets go through the standard text pipeline
            return false;
        }

        auto result = mm_embedder_->embed(at, "", data, mime_type);
        if (!result.success || result.embedding.empty()) return false;

        // Store in FAISS via TMS
        tms::AssetChunk chunk;
        chunk.repo_id = repo_id;
        chunk.asset_id = asset_id;
        chunk.file_name = file_name;
        chunk.mime_type = mime_type;
        chunk.modality = modality;
        chunk.embedding = std::move(result.embedding);
        chunk.chunk_index = 0;
        chunk.total_chunks = 1;

        return tms_->storeAssetChunk(chunk);
    }

    std::vector<ModalityConfig> getMultimodalConfig() override {
        ensureMultimodalEmbedder();
        std::vector<ModalityConfig> configs;

        // Text modality (always enabled via the main embedding engine)
        ModalityConfig text;
        text.modality = "text";
        text.model_name = tms_->embeddingEngine().activeModelName();
        text.description = "Semantic search across code, documentation, and text files.";
        text.status = "ready";
        text.enabled = true;
        text.size_mb = 0;
        configs.push_back(text);

        if (mm_embedder_) {
            auto statuses = mm_embedder_->getStatus();

            // Image modality
            ModalityConfig img;
            img.modality = "image";
            img.model_name = "SigLIP Base";
            img.description = "Search screenshots, diagrams, and visual assets alongside your code.";
            img.size_mb = 350;
            if (auto it = statuses.find(tms::EmbeddingModality::VISION); it != statuses.end()) {
                img.enabled = it->second.enabled;
                img.download_progress = it->second.download_progress;
                if (it->second.ready)             img.status = "ready";
                else if (it->second.downloading)  img.status = "downloading";
                else if (!it->second.error.empty()) img.status = "error";
                else                               img.status = "pending";
            }
            configs.push_back(img);

            // Audio modality
            ModalityConfig aud;
            aud.modality = "audio";
            aud.model_name = "CLAP General";
            aud.description = "Find voice recordings, meeting notes, and audio assets.";
            aud.size_mb = 650;
            if (auto it = statuses.find(tms::EmbeddingModality::AUDIO); it != statuses.end()) {
                aud.enabled = it->second.enabled;
                aud.download_progress = it->second.download_progress;
                if (it->second.ready)             aud.status = "ready";
                else if (it->second.downloading)  aud.status = "downloading";
                else if (!it->second.error.empty()) aud.status = "error";
                else                               aud.status = "pending";
            }
            configs.push_back(aud);
        }

        return configs;
    }

    ModalityConfig setModalityEnabled(
        const std::string& modality,
        bool enabled) override
    {
        ensureMultimodalEmbedder();
        if (!mm_embedder_) return {};

        tms::EmbeddingModality mod;
        if (modality == "image") mod = tms::EmbeddingModality::VISION;
        else if (modality == "audio") mod = tms::EmbeddingModality::AUDIO;
        else return {};

        mm_embedder_->setModalityEnabled(mod, enabled);

        auto statuses = mm_embedder_->getStatus();

        ModalityConfig cfg;
        cfg.modality = modality;
        cfg.enabled = enabled;
        if (auto it = statuses.find(mod); it != statuses.end()) {
            cfg.download_progress = it->second.download_progress;
            if (it->second.ready)             cfg.status = "ready";
            else if (it->second.downloading)  cfg.status = "downloading";
            else if (!it->second.error.empty()) cfg.status = "error";
            else                               cfg.status = "pending";
        }

        if (modality == "image") {
            cfg.model_name = "SigLIP Base";
            cfg.description = "Search screenshots, diagrams, and visual assets alongside your code.";
            cfg.size_mb = 350;
        } else {
            cfg.model_name = "CLAP General";
            cfg.description = "Find voice recordings, meeting notes, and audio assets.";
            cfg.size_mb = 650;
        }

        return cfg;
    }

private:
    EngineConfig config_;
    std::unique_ptr<tms::TMSMemorySystem> tms_;
    std::unique_ptr<ReviewSignals> review_signals_;

    // Multimodal embedder (lazy-initialized)
    std::unique_ptr<tms::MultiModalEmbedder> mm_embedder_;
    std::once_flag mm_init_flag_;

    void ensureMultimodalEmbedder() {
        std::call_once(mm_init_flag_, [this]() {
            try {
                auto& emb = tms_->embeddingEngine();
                tms::TMSConfig tms_cfg;
                tms_cfg.image_embedding_enabled = config_.multimodal_image_enabled;
                tms_cfg.audio_embedding_enabled = config_.multimodal_audio_enabled;

                // Use models_dir from EngineConfig (set by main.cpp from RTVORTEX_HOME/models).
                // Fall back to storage_path/../models if models_dir wasn't set.
                namespace fs = std::filesystem;
                std::string models_dir = config_.models_dir;
                if (models_dir.empty()) {
                    models_dir = (fs::path(config_.storage_path).parent_path() / "models").string();
                }

                mm_embedder_ = std::make_unique<tms::MultiModalEmbedder>(emb, tms_cfg);
                mm_embedder_->initialize(models_dir);
            } catch (const std::exception& e) {
                std::cerr << "[ENGINE] Failed to initialize multimodal embedder: "
                          << e.what() << std::endl;
            }
        });
    }

    // Transient clone token — set per-request, consumed once, then cleared.
    std::mutex clone_token_mu_;
    std::string clone_token_;
};

// =============================================================================
// Engine::create factory
// =============================================================================

std::unique_ptr<Engine> Engine::create(const EngineConfig& config) {
    auto errors = ConfigValidator::validate(config);
    if (!errors.empty()) {
        std::string msg = "EngineConfig validation failed:";
        for (const auto& e : errors) {
            msg += "\n  " + e.field + ": " + e.message;
        }
        throw std::runtime_error(msg);
    }
    return std::make_unique<EngineImpl>(config);
}

} // namespace aipr
