/**
 * Chunk Prefixer — Prepends hierarchical context to chunk content
 *
 * Before a CodeChunk's content is embedded, ChunkPrefixer prepends a short
 * structural preamble so the embedding carries repo/module/file context.
 *
 * Format (≤ 64 tokens):
 *   [repo:<repo_id>] [module:<module>] [file:<path>] [lang:<lang>]
 *   [parent:<parent_name>] [kind:<type>]
 *
 * The prefix is written into chunk.content; the original content is
 * preserved after the prefix line.
 */

#pragma once

#include "hierarchy_builder.h"
#include "tms/tms_types.h"
#include <string>
#include <vector>
#include <cstddef>
#include <atomic>

namespace aipr {

class ChunkPrefixer {
public:
    /**
     * @param max_prefix_tokens  Hard cap on prefix length (in approx tokens).
     */
    explicit ChunkPrefixer(size_t max_prefix_tokens = 64);

    /**
     * Build a prefix string for the given chunk, capped at max_prefix_tokens.
     * Does NOT mutate the chunk.
     */
    std::string buildPrefix(
        const tms::CodeChunk& chunk,
        const std::string& repo_id,
        const RepoManifest& manifest) const;

    /**
     * Mutate each chunk's content in-place: prefix + "\n" + original content.
     * Returns the number of chunks actually prefixed.
     */
    size_t applyPrefixes(
        std::vector<tms::CodeChunk>& chunks,
        const std::string& repo_id,
        const RepoManifest& manifest);

    /**
     * Average prefix length (chars) across all prefixes generated so far.
     * The value is also published as a gauge metric.
     */
    double avgPrefixLength() const;

private:
    size_t max_prefix_tokens_;
    std::atomic<uint64_t> total_prefix_chars_{0};
    std::atomic<uint64_t> total_prefix_count_{0};

    // Rough token estimate: ~4 chars per token for code-like text
    static size_t estimateTokens(const std::string& text);

    // Truncate the prefix to fit within token budget
    std::string truncateToTokenBudget(const std::string& raw, size_t budget) const;
};

} // namespace aipr
