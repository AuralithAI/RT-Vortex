/**
 * Chunk Prefixer implementation
 *
 * Prepends a short hierarchical context line to each chunk's content so that
 * the resulting embedding encodes repo → module → file → parent structure.
 *
 * The prefix is capped at max_prefix_tokens (default 64, ~256 chars).
 */

#include "chunk_prefixer.h"
#include "metrics.h"
#include <sstream>
#include <algorithm>

namespace aipr {

ChunkPrefixer::ChunkPrefixer(size_t max_prefix_tokens)
    : max_prefix_tokens_(max_prefix_tokens) {}

// ── Token estimation ──────────────────────────────────────────────────────

size_t ChunkPrefixer::estimateTokens(const std::string& text) {
    // Rough heuristic: ~4 characters per token for code-like text
    return text.size() / 4 + 1;
}

// ── Truncation ─────────────────────────────────────────────────────────────

std::string ChunkPrefixer::truncateToTokenBudget(
    const std::string& raw,
    size_t budget) const
{
    size_t char_budget = budget * 4;  // inverse of estimateTokens
    if (raw.size() <= char_budget) return raw;
    return raw.substr(0, char_budget);
}

// ── Build prefix ──────────────────────────────────────────────────────────

std::string ChunkPrefixer::buildPrefix(
    const tms::CodeChunk& chunk,
    const std::string& repo_id,
    const RepoManifest& manifest) const
{
    std::ostringstream prefix;

    // [repo:<id>]
    if (!repo_id.empty()) {
        prefix << "[repo:" << repo_id << "] ";
    }

    // [module:<module>]
    auto mod_it = manifest.file_to_module.find(chunk.file_path);
    if (mod_it != manifest.file_to_module.end() && !mod_it->second.empty()) {
        prefix << "[module:" << mod_it->second << "] ";
    }

    // [file:<path>]
    if (!chunk.file_path.empty()) {
        prefix << "[file:" << chunk.file_path << "] ";
    }

    // [lang:<language>]
    if (!chunk.language.empty()) {
        prefix << "[lang:" << chunk.language << "] ";
    }

    // [parent:<parent_name>]
    if (!chunk.parent_name.empty()) {
        prefix << "[parent:" << chunk.parent_name << "] ";
    }

    // [kind:<type>]
    if (!chunk.type.empty()) {
        prefix << "[kind:" << chunk.type << "]";
    }

    std::string result = prefix.str();
    // Trim trailing space
    while (!result.empty() && result.back() == ' ') {
        result.pop_back();
    }

    return truncateToTokenBudget(result, max_prefix_tokens_);
}

// ── Apply prefixes in bulk ─────────────────────────────────────────────────

size_t ChunkPrefixer::applyPrefixes(
    std::vector<tms::CodeChunk>& chunks,
    const std::string& repo_id,
    const RepoManifest& manifest)
{
    size_t count = 0;

    for (auto& chunk : chunks) {
        std::string prefix = buildPrefix(chunk, repo_id, manifest);
        if (prefix.empty()) continue;

        chunk.content = prefix + "\n" + chunk.content;
        ++count;

        total_prefix_chars_.fetch_add(prefix.size(), std::memory_order_relaxed);
        total_prefix_count_.fetch_add(1, std::memory_order_relaxed);
    }

    // Publish gauge
    double avg = avgPrefixLength();
    if (avg > 0.0) {
        metrics::Registry::instance().setGauge(metrics::AVG_PREFIX_LENGTH_CHARS, avg);
    }

    return count;
}

// ── Stats ──────────────────────────────────────────────────────────────────

double ChunkPrefixer::avgPrefixLength() const {
    uint64_t n = total_prefix_count_.load(std::memory_order_relaxed);
    if (n == 0) return 0.0;
    return static_cast<double>(total_prefix_chars_.load(std::memory_order_relaxed))
           / static_cast<double>(n);
}

} // namespace aipr
