/**
 * AI PR Reviewer - Short-Term Memory Implementation
 * 
 * Active working memory for current session context.
 */

#include "memory_system.h"
#include <algorithm>
#include <cmath>

namespace aipr {

ShortTermMemory::ShortTermMemory(const MemoryConfig& config)
    : config_(config) {
}

ShortTermMemory::~ShortTermMemory() = default;

void ShortTermMemory::store(const SessionMemory& memory) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto& sessions = session_memories_[memory.session_id];
    
    // Enforce capacity limit
    while (sessions.size() >= config_.stm_capacity) {
        sessions.erase(sessions.begin());
    }
    
    sessions.push_back(memory);
}

void ShortTermMemory::addToHistory(
    const std::string& session_id, 
    const std::string& role, 
    const std::string& content
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto& history = histories_[session_id];
    history.emplace_back(role, content);
    
    // Limit history size
    while (history.size() > 50) {
        history.erase(history.begin());
    }
}

std::vector<std::pair<std::string, std::string>> ShortTermMemory::getHistory(
    const std::string& session_id
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = histories_.find(session_id);
    if (it != histories_.end()) {
        return it->second;
    }
    return {};
}

void ShortTermMemory::cacheRetrieval(
    const std::string& query_hash, 
    const std::vector<std::string>& result_ids
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    retrieval_cache_[query_hash] = result_ids;
    cache_timestamps_[query_hash] = std::chrono::system_clock::now();
}

std::optional<std::vector<std::string>> ShortTermMemory::getCachedRetrieval(
    const std::string& query_hash
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = retrieval_cache_.find(query_hash);
    if (it == retrieval_cache_.end()) {
        return std::nullopt;
    }
    
    // Check TTL
    auto timestamp_it = cache_timestamps_.find(query_hash);
    if (timestamp_it != cache_timestamps_.end()) {
        auto age = std::chrono::system_clock::now() - timestamp_it->second;
        if (age > config_.stm_ttl) {
            retrieval_cache_.erase(it);
            cache_timestamps_.erase(timestamp_it);
            return std::nullopt;
        }
    }
    
    return it->second;
}

std::vector<SessionMemory> ShortTermMemory::getRecent(
    const std::string& session_id, 
    int limit
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = session_memories_.find(session_id);
    if (it == session_memories_.end()) {
        return {};
    }
    
    const auto& sessions = it->second;
    int count = std::min(limit, static_cast<int>(sessions.size()));
    
    // Return most recent
    return std::vector<SessionMemory>(
        sessions.end() - count,
        sessions.end()
    );
}

std::vector<SessionMemory> ShortTermMemory::retrieve(
    const std::vector<float>& query_embedding,
    const std::string& session_id,
    int top_k
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    if (top_k < 0) top_k = config_.stm_top_k;
    
    auto it = session_memories_.find(session_id);
    if (it == session_memories_.end()) {
        return {};
    }
    
    // Score by similarity + recency
    std::vector<std::pair<size_t, double>> scored;
    const auto& sessions = it->second;
    auto now = std::chrono::system_clock::now();
    
    for (size_t i = 0; i < sessions.size(); ++i) {
        const auto& memory = sessions[i];
        
        // Cosine similarity
        double dot = 0, mag_a = 0, mag_b = 0;
        for (size_t j = 0; j < query_embedding.size() && j < memory.embedding.size(); ++j) {
            dot += query_embedding[j] * memory.embedding[j];
            mag_a += query_embedding[j] * query_embedding[j];
            mag_b += memory.embedding[j] * memory.embedding[j];
        }
        
        double similarity = 0;
        if (mag_a > 0 && mag_b > 0) {
            similarity = dot / (std::sqrt(mag_a) * std::sqrt(mag_b));
        }
        
        // Recency boost (exponential decay)
        auto age = std::chrono::duration_cast<std::chrono::minutes>(
            now - memory.created_at
        ).count();
        double recency = std::exp(-age / 30.0);  // 30-minute half-life
        
        // Combined score
        double score = 0.7 * similarity + 0.3 * recency;
        scored.emplace_back(i, score);
    }
    
    // Sort by score
    std::sort(scored.begin(), scored.end(),
              [](const auto& a, const auto& b) { return a.second > b.second; });
    
    // Build result
    std::vector<SessionMemory> results;
    for (int i = 0; i < top_k && i < static_cast<int>(scored.size()); ++i) {
        results.push_back(sessions[scored[i].first]);
    }
    
    return results;
}

void ShortTermMemory::clearSession(const std::string& session_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    session_memories_.erase(session_id);
    histories_.erase(session_id);
}

size_t ShortTermMemory::cleanup() {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto now = std::chrono::system_clock::now();
    size_t cleaned = 0;
    
    // Clean up expired sessions
    std::vector<std::string> expired_sessions;
    
    for (auto& [session_id, memories] : session_memories_) {
        // Remove old memories from each session
        auto before_size = memories.size();
        
        memories.erase(
            std::remove_if(memories.begin(), memories.end(),
                [&](const SessionMemory& m) {
                    auto age = now - m.created_at;
                    return age > config_.stm_ttl;
                }
            ),
            memories.end()
        );
        
        cleaned += before_size - memories.size();
        
        // Mark empty sessions for removal
        if (memories.empty()) {
            expired_sessions.push_back(session_id);
        }
    }
    
    // Remove empty sessions
    for (const auto& session_id : expired_sessions) {
        session_memories_.erase(session_id);
        histories_.erase(session_id);
    }
    
    // Clean up retrieval cache
    std::vector<std::string> expired_cache;
    for (const auto& [hash, timestamp] : cache_timestamps_) {
        if (now - timestamp > config_.stm_ttl) {
            expired_cache.push_back(hash);
        }
    }
    
    for (const auto& hash : expired_cache) {
        retrieval_cache_.erase(hash);
        cache_timestamps_.erase(hash);
        cleaned++;
    }
    
    return cleaned;
}

std::vector<std::string> ShortTermMemory::getActiveSessions() const {
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::vector<std::string> sessions;
    for (const auto& [session_id, _] : session_memories_) {
        sessions.push_back(session_id);
    }
    return sessions;
}

} // namespace aipr
