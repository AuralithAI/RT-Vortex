/**
 * TMS Short-Term Memory (STM) - Ring Buffer Implementation
 * 
 * Active working memory for current session context.
 */

#include "tms/stm.h"
#include <algorithm>
#include <numeric>
#include <functional>
#include <sstream>
#include <iomanip>
#include <cmath>

namespace aipr::tms {

// =============================================================================
// Constructor / Destructor
// =============================================================================

STM::STM(const STMConfig& config)
    : config_(config) {
}

STM::~STM() = default;

// =============================================================================
// Session Management
// =============================================================================

void STM::startSession(const std::string& session_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    if (sessions_.count(session_id)) {
        // Session already exists, just touch it
        sessions_[session_id].last_accessed = std::chrono::system_clock::now();
        return;
    }
    
    // Check capacity
    if (sessions_.size() >= config_.max_sessions) {
        // Remove oldest session
        auto oldest = sessions_.begin();
        auto oldest_time = oldest->second.last_accessed;
        
        for (auto it = sessions_.begin(); it != sessions_.end(); ++it) {
            if (it->second.last_accessed < oldest_time) {
                oldest = it;
                oldest_time = it->second.last_accessed;
            }
        }
        
        sessions_.erase(oldest);
    }
    
    // Create new session
    SessionData session;
    session.id = session_id;
    session.created_at = std::chrono::system_clock::now();
    session.last_accessed = session.created_at;
    
    sessions_[session_id] = std::move(session);
}

void STM::endSession(const std::string& session_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    sessions_.erase(session_id);
}

bool STM::hasSession(const std::string& session_id) const {
    std::lock_guard<std::mutex> lock(mutex_);
    return sessions_.count(session_id) > 0;
}

std::vector<std::string> STM::getActiveSessions() const {
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::vector<std::string> ids;
    ids.reserve(sessions_.size());
    
    for (const auto& [id, _] : sessions_) {
        ids.push_back(id);
    }
    
    return ids;
}

void STM::touchSession(const std::string& session_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = sessions_.find(session_id);
    if (it != sessions_.end()) {
        it->second.last_accessed = std::chrono::system_clock::now();
    }
}

// =============================================================================
// Entry Management
// =============================================================================

void STM::store(const STMEntry& entry) {
    std::lock_guard<std::mutex> lock(mutex_);
    ensureSession(entry.session_id);
    
    auto& session = sessions_[entry.session_id];
    
    // Check capacity
    if (session.entries.size() >= config_.capacity) {
        evictOldestEntry(session);
    }
    
    // Add entry
    session.entries.push_back(entry);
    session.entry_index[entry.id] = session.entries.size() - 1;
    session.last_accessed = std::chrono::system_clock::now();
}

void STM::storeQuery(
    const std::string& session_id,
    const std::string& query_text,
    const std::vector<float>& query_embedding,
    const std::vector<std::string>& retrieved_chunk_ids
) {
    STMEntry entry;
    entry.id = session_id + "_q_" + std::to_string(std::chrono::system_clock::now().time_since_epoch().count());
    entry.session_id = session_id;
    entry.entry_type = "query";
    entry.query_text = query_text;
    entry.content = query_text;
    entry.embedding = query_embedding;
    entry.retrieved_chunk_ids = retrieved_chunk_ids;
    entry.created_at = std::chrono::system_clock::now();
    entry.last_accessed = entry.created_at;
    
    store(entry);
}

void STM::storeContext(
    const std::string& session_id,
    const std::string& context_type,
    const std::string& content,
    const std::vector<float>& embedding
) {
    STMEntry entry;
    entry.id = session_id + "_c_" + std::to_string(std::chrono::system_clock::now().time_since_epoch().count());
    entry.session_id = session_id;
    entry.entry_type = "context";
    entry.context_type = context_type;
    entry.content = content;
    entry.embedding = embedding;
    entry.created_at = std::chrono::system_clock::now();
    entry.last_accessed = entry.created_at;
    
    store(entry);
}

std::optional<STMEntry> STM::get(const std::string& entry_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    for (auto& [session_id, session] : sessions_) {
        auto it = session.entry_index.find(entry_id);
        if (it != session.entry_index.end() && it->second < session.entries.size()) {
            auto& entry = session.entries[it->second];
            entry.last_accessed = std::chrono::system_clock::now();
            entry.access_count++;
            return entry;
        }
    }
    
    return std::nullopt;
}

std::vector<STMEntry> STM::getRecent(
    const std::string& session_id,
    int limit,
    const std::string& entry_type
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = sessions_.find(session_id);
    if (it == sessions_.end()) {
        return {};
    }
    
    if (limit <= 0) limit = config_.default_top_k;
    
    std::vector<STMEntry> results;
    const auto& entries = it->second.entries;
    
    // Iterate from most recent
    for (auto rit = entries.rbegin(); rit != entries.rend() && results.size() < static_cast<size_t>(limit); ++rit) {
        if (entry_type.empty() || rit->entry_type == entry_type) {
            results.push_back(*rit);
        }
    }
    
    return results;
}

std::vector<RetrievedChunk> STM::search(
    const std::string& session_id,
    const std::vector<float>& query_embedding,
    int top_k
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = sessions_.find(session_id);
    if (it == sessions_.end()) {
        return {};
    }
    
    if (top_k <= 0) top_k = config_.default_top_k;
    
    // Compute similarity for all entries with embeddings
    std::vector<std::pair<size_t, float>> scores;
    
    const auto& entries = it->second.entries;
    for (size_t i = 0; i < entries.size(); ++i) {
        if (entries[i].embedding.empty()) continue;
        
        float sim = cosineSimilarity(query_embedding, entries[i].embedding);
        scores.emplace_back(i, sim);
    }
    
    // Sort by similarity
    std::partial_sort(
        scores.begin(),
        scores.begin() + std::min(static_cast<size_t>(top_k), scores.size()),
        scores.end(),
        [](const auto& a, const auto& b) { return a.second > b.second; }
    );
    
    // Build results
    std::vector<RetrievedChunk> results;
    for (size_t i = 0; i < std::min(static_cast<size_t>(top_k), scores.size()); ++i) {
        const auto& entry = entries[scores[i].first];
        
        RetrievedChunk chunk;
        chunk.chunk.id = entry.id;
        chunk.chunk.content = entry.content;
        chunk.similarity_score = scores[i].second;
        chunk.combined_score = scores[i].second;
        chunk.memory_source = "STM";
        
        results.push_back(chunk);
    }
    
    return results;
}

void STM::markForPromotion(const std::string& entry_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    for (auto& [session_id, session] : sessions_) {
        auto it = session.entry_index.find(entry_id);
        if (it != session.entry_index.end() && it->second < session.entries.size()) {
            session.entries[it->second].promoted_to_ltm = true;
            return;
        }
    }
}

std::vector<STMEntry> STM::getPromotionCandidates(const std::string& session_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = sessions_.find(session_id);
    if (it == sessions_.end()) {
        return {};
    }
    
    std::vector<STMEntry> candidates;
    for (const auto& entry : it->second.entries) {
        if (entry.promoted_to_ltm || entry.access_count >= 3 || entry.relevance_score > 0.8) {
            candidates.push_back(entry);
        }
    }
    
    return candidates;
}

// =============================================================================
// Conversation History
// =============================================================================

void STM::addConversationTurn(
    const std::string& session_id,
    const std::string& role,
    const std::string& content,
    const std::vector<float>& embedding
) {
    std::lock_guard<std::mutex> lock(mutex_);
    ensureSession(session_id);
    
    auto& session = sessions_[session_id];
    
    // Enforce limit
    if (session.conversation.size() >= config_.conversation_history_limit) {
        session.conversation.erase(session.conversation.begin());
    }
    
    ConversationTurn turn;
    turn.role = role;
    turn.content = content;
    turn.timestamp = std::chrono::system_clock::now();
    if (!embedding.empty()) {
        turn.embedding = embedding;
    }
    
    session.conversation.push_back(std::move(turn));
    session.last_accessed = std::chrono::system_clock::now();
}

std::vector<ConversationTurn> STM::getConversationHistory(
    const std::string& session_id,
    int limit
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = sessions_.find(session_id);
    if (it == sessions_.end()) {
        return {};
    }
    
    const auto& conversation = it->second.conversation;
    
    if (limit <= 0 || static_cast<size_t>(limit) >= conversation.size()) {
        return conversation;
    }
    
    // Return last N turns
    return std::vector<ConversationTurn>(
        conversation.end() - limit,
        conversation.end()
    );
}

void STM::clearConversationHistory(const std::string& session_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = sessions_.find(session_id);
    if (it != sessions_.end()) {
        it->second.conversation.clear();
    }
}

// =============================================================================
// Retrieval Cache
// =============================================================================

void STM::cacheRetrieval(
    const std::string& query_hash,
    const std::vector<std::string>& chunk_ids
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Enforce cache size
    if (retrieval_cache_.size() >= config_.retrieval_cache_size) {
        // Remove oldest entry
        auto oldest = retrieval_cache_.begin();
        auto oldest_time = oldest->second.cached_at;
        
        for (auto it = retrieval_cache_.begin(); it != retrieval_cache_.end(); ++it) {
            if (it->second.cached_at < oldest_time) {
                oldest = it;
                oldest_time = it->second.cached_at;
            }
        }
        
        retrieval_cache_.erase(oldest);
    }
    
    RetrievalCacheEntry entry;
    entry.query_hash = query_hash;
    entry.chunk_ids = chunk_ids;
    entry.cached_at = std::chrono::system_clock::now();
    
    retrieval_cache_[query_hash] = std::move(entry);
}

std::optional<std::vector<std::string>> STM::getCachedRetrieval(
    const std::string& query_hash
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = retrieval_cache_.find(query_hash);
    if (it == retrieval_cache_.end()) {
        cache_misses_++;
        return std::nullopt;
    }
    
    // Check TTL
    auto age = std::chrono::system_clock::now() - it->second.cached_at;
    if (age > config_.cache_ttl) {
        retrieval_cache_.erase(it);
        cache_misses_++;
        return std::nullopt;
    }
    
    it->second.hit_count++;
    cache_hits_++;
    
    return it->second.chunk_ids;
}

std::string STM::computeQueryHash(
    const std::string& query_text,
    const std::string& repo_filter
) {
    // Simple hash - in production use proper hash function
    std::hash<std::string> hasher;
    size_t h1 = hasher(query_text);
    size_t h2 = hasher(repo_filter);
    
    std::ostringstream oss;
    oss << std::hex << (h1 ^ (h2 << 1));
    return oss.str();
}

void STM::clearCache() {
    std::lock_guard<std::mutex> lock(mutex_);
    retrieval_cache_.clear();
}

// =============================================================================
// Maintenance
// =============================================================================

size_t STM::cleanup() {
    std::lock_guard<std::mutex> lock(mutex_);
    
    size_t removed = 0;
    auto now = std::chrono::system_clock::now();
    
    for (auto& [session_id, session] : sessions_) {
        // Remove expired entries
        auto new_end = std::remove_if(
            session.entries.begin(),
            session.entries.end(),
            [&](const STMEntry& entry) {
                auto age = std::chrono::duration_cast<std::chrono::minutes>(
                    now - entry.created_at
                );
                return age > config_.ttl;
            }
        );
        
        removed += std::distance(new_end, session.entries.end());
        session.entries.erase(new_end, session.entries.end());
        
        // Rebuild index
        session.entry_index.clear();
        for (size_t i = 0; i < session.entries.size(); ++i) {
            session.entry_index[session.entries[i].id] = i;
        }
    }
    
    // Clean cache
    auto cache_new_end = retrieval_cache_.begin();
    while (cache_new_end != retrieval_cache_.end()) {
        auto age = std::chrono::duration_cast<std::chrono::minutes>(
            now - cache_new_end->second.cached_at
        );
        if (age > config_.cache_ttl) {
            cache_new_end = retrieval_cache_.erase(cache_new_end);
            removed++;
        } else {
            ++cache_new_end;
        }
    }
    
    return removed;
}

size_t STM::cleanupSessions() {
    std::lock_guard<std::mutex> lock(mutex_);
    
    size_t removed = 0;
    auto now = std::chrono::system_clock::now();
    
    auto it = sessions_.begin();
    while (it != sessions_.end()) {
        auto age = std::chrono::duration_cast<std::chrono::minutes>(
            now - it->second.last_accessed
        );
        
        // Remove sessions inactive for 2x TTL
        if (age > config_.ttl * 2) {
            it = sessions_.erase(it);
            removed++;
        } else {
            ++it;
        }
    }
    
    return removed;
}

// =============================================================================
// Statistics
// =============================================================================

STM::Stats STM::getStats() const {
    std::lock_guard<std::mutex> lock(mutex_);
    
    Stats stats;
    stats.active_sessions = sessions_.size();
    stats.cache_size = retrieval_cache_.size();
    stats.cache_hits = cache_hits_;
    stats.cache_misses = cache_misses_;
    
    if (cache_hits_ + cache_misses_ > 0) {
        stats.hit_rate = static_cast<double>(cache_hits_) / (cache_hits_ + cache_misses_);
    }
    
    for (const auto& [_, session] : sessions_) {
        stats.total_entries += session.entries.size();
        stats.conversation_turns += session.conversation.size();
    }
    
    return stats;
}

STM::SessionStats STM::getSessionStats(const std::string& session_id) const {
    std::lock_guard<std::mutex> lock(mutex_);
    
    SessionStats stats;
    stats.session_id = session_id;
    
    auto it = sessions_.find(session_id);
    if (it != sessions_.end()) {
        stats.entry_count = it->second.entries.size();
        stats.conversation_turns = it->second.conversation.size();
        stats.created_at = it->second.created_at;
        stats.last_accessed = it->second.last_accessed;
    }
    
    return stats;
}

// =============================================================================
// Helpers
// =============================================================================

void STM::ensureSession(const std::string& session_id) {
    if (!sessions_.count(session_id)) {
        SessionData session;
        session.id = session_id;
        session.created_at = std::chrono::system_clock::now();
        session.last_accessed = session.created_at;
        sessions_[session_id] = std::move(session);
    }
}

void STM::evictOldestEntry(SessionData& session) {
    if (session.entries.empty()) return;
    
    // Remove from index
    const auto& entry = session.entries.front();
    session.entry_index.erase(entry.id);
    
    // Remove entry
    session.entries.pop_front();
    
    // Rebuild index (positions shifted)
    session.entry_index.clear();
    for (size_t i = 0; i < session.entries.size(); ++i) {
        session.entry_index[session.entries[i].id] = i;
    }
}

bool STM::isExpired(const std::chrono::system_clock::time_point& time) const {
    auto age = std::chrono::duration_cast<std::chrono::minutes>(
        std::chrono::system_clock::now() - time
    );
    return age > config_.ttl;
}

float STM::cosineSimilarity(const std::vector<float>& a, const std::vector<float>& b) const {
    if (a.size() != b.size() || a.empty()) return 0.0f;
    
    float dot = 0.0f, norm_a = 0.0f, norm_b = 0.0f;
    
    for (size_t i = 0; i < a.size(); ++i) {
        dot += a[i] * b[i];
        norm_a += a[i] * a[i];
        norm_b += b[i] * b[i];
    }
    
    float denom = std::sqrt(norm_a) * std::sqrt(norm_b);
    return denom > 0 ? dot / denom : 0.0f;
}

// =============================================================================
// Canonical Answers (Zero-LLM Fast Path)
// =============================================================================

void STM::precomputeCanonical(
    const std::vector<RetrievedChunk>& retrieval_results,
    const std::vector<std::vector<float>>& embeddings,
    float min_score
) {
    std::lock_guard<std::mutex> lock(mutex_);

    for (size_t idx = 0; idx < retrieval_results.size(); ++idx) {
        const auto& rc = retrieval_results[idx];
        if (rc.similarity_score < min_score) continue;

        // Avoid duplicates — deduplicate by chunk ID
        bool exists = false;
        for (const auto& ca : canonical_cache_) {
            if (ca.chunk_id == rc.chunk.id) { exists = true; break; }
        }
        if (exists) continue;

        CanonicalAnswer ca;
        ca.chunk_id = rc.chunk.id;
        ca.content = rc.chunk.content;
        ca.embedding = (idx < embeddings.size()) ? embeddings[idx] : std::vector<float>{};
        ca.score = rc.similarity_score;
        ca.computed_at = std::chrono::system_clock::now();
        canonical_cache_.push_back(std::move(ca));
    }

    // Cap at a reasonable size (LRU-style: keep most recent)
    constexpr size_t kMaxCanonical = 500;
    if (canonical_cache_.size() > kMaxCanonical) {
        canonical_cache_.erase(
            canonical_cache_.begin(),
            canonical_cache_.begin() +
                static_cast<long>(canonical_cache_.size() - kMaxCanonical));
    }
}

std::optional<CanonicalAnswer> STM::lookupCanonical(
    const std::vector<float>& query_embedding,
    float threshold
) const {
    std::lock_guard<std::mutex> lock(mutex_);

    float best_sim = 0.0f;
    const CanonicalAnswer* best = nullptr;

    for (const auto& ca : canonical_cache_) {
        if (ca.embedding.empty()) continue;
        float sim = cosineSimilarity(query_embedding, ca.embedding);
        if (sim > best_sim) {
            best_sim = sim;
            best = &ca;
        }
    }

    if (best && best_sim >= threshold) {
        CanonicalAnswer result = *best;
        result.score = best_sim;
        return result;
    }
    return std::nullopt;
}

} // namespace aipr::tms
