/**
 * AI PR Reviewer - Memory System Implementation
 *
 * Coordinates LTM, STM, MTM and cross-memory attention.
 */

#include "memory_system.h"
#include <thread>
#include <chrono>
#include <sstream>

namespace aipr {

// =============================================================================
// MemorySystem Implementation
// =============================================================================

MemorySystem::MemorySystem(const MemoryConfig& config)
    : config_(config) {
}

MemorySystem::~MemorySystem() {
    stop_consolidation_ = true;
    if (consolidation_thread_.joinable()) {
        consolidation_thread_.join();
    }
}

void MemorySystem::initialize() {
    if (initialized_) return;

    std::lock_guard<std::mutex> lock(mutex_);

    // Create memory subsystems
    ltm_ = std::make_unique<LongTermMemory>(config_);
    stm_ = std::make_unique<ShortTermMemory>(config_);
    mtm_ = std::make_unique<MetaTaskMemory>(config_);

    // Create attention module
    CrossMemoryAttention::Config attn_config;
    attn_config.embed_dim = static_cast<int>(config_.ltm_dimension);
    attention_ = std::make_unique<CrossMemoryAttention>(attn_config);

    // Load persisted data
    load();

    // Start background consolidation
    stop_consolidation_ = false;
    consolidation_thread_ = std::thread([this]() {
        consolidationLoop();
    });

    initialized_ = true;
}

void MemorySystem::indexRepository(
    const std::string& repo_id,
    const std::vector<Chunk>& chunks,
    const std::vector<std::vector<float>>& embeddings
) {
    std::lock_guard<std::mutex> lock(mutex_);

    if (chunks.size() != embeddings.size()) {
        throw std::invalid_argument("Chunk and embedding count mismatch");
    }

    std::vector<CodeMemory> memories;
    memories.reserve(chunks.size());

    for (size_t i = 0; i < chunks.size(); ++i) {
        CodeMemory mem;
        mem.id = repo_id + ":" + chunks[i].id;
        mem.repo_id = repo_id;
        mem.file_path = chunks[i].file_path;
        mem.start_line = chunks[i].start_line;
        mem.end_line = chunks[i].end_line;
        mem.content = chunks[i].content;
        mem.language = chunks[i].language;
        mem.symbols = chunks[i].symbols;
        mem.embedding = embeddings[i];
        mem.importance_score = 1.0;
        mem.created_at = std::chrono::system_clock::now();
        mem.last_accessed = mem.created_at;

        memories.push_back(std::move(mem));
    }

    ltm_->storeBatch(memories);
}

void MemorySystem::startSession(const std::string& session_id) {
    // STM doesn't have a startSession method, but we can clear any old session
    stm_->clearSession(session_id);
}

void MemorySystem::addContext(
    const std::string& session_id,
    const std::string& context_type,
    const std::string& content,
    const std::vector<float>& embedding
) {
    SessionMemory mem;
    mem.id = session_id + ":" + std::to_string(
        std::chrono::system_clock::now().time_since_epoch().count()
    );
    mem.session_id = session_id;
    mem.content = content;
    mem.embedding = embedding;
    mem.metadata["type"] = context_type;
    mem.created_at = std::chrono::system_clock::now();

    stm_->store(mem);
}

MemoryRetrievalResult MemorySystem::retrieve(
    const std::string& session_id,
    const std::string& query,
    const std::vector<float>& query_embedding,
    int top_k
) {
    // 1. Retrieve from LTM
    auto ltm_results = ltm_->hybridRetrieve(query, query_embedding, top_k);

    // 2. Get relevant STM items
    auto stm_results = stm_->retrieve(query_embedding, session_id, top_k / 2);

    // 3. Match MTM patterns (uses embedding-based matching)
    auto patterns = mtm_->matchPatterns(query_embedding, "", top_k / 4);

    // 4. Get applicable strategies
    auto strategies = mtm_->getTopStrategies(5);

    // 5. Apply cross-memory attention
    return attention_->attend(
        query_embedding,
        ltm_results,
        stm_results,
        patterns,
        strategies
    );
}

void MemorySystem::learnFromFeedback(
    const std::string& /*session_id*/,
    double review_quality,
    const std::vector<std::string>& helpful_items,
    const std::vector<std::string>& unhelpful_items
) {
    // Update LTM importance scores
    for (const auto& id : helpful_items) {
        ltm_->updateImportance(id, review_quality * 0.1);
    }

    for (const auto& id : unhelpful_items) {
        ltm_->updateImportance(id, -0.1);
    }

    // Learn from outcome in MTM
    mtm_->learnFromOutcome("", helpful_items, unhelpful_items, review_quality);
}

void MemorySystem::endSession(const std::string& session_id) {
    stm_->clearSession(session_id);
}

MemorySystem::SystemStats MemorySystem::getStats() const {
    SystemStats stats;

    stats.ltm_stats = ltm_->getStats();
    stats.stm_sessions = stm_->getActiveSessions().size();
    // Approximate pattern/strategy counts from top-level retrieval
    stats.mtm_patterns = 0;
    stats.mtm_strategies = 0;

    // Rough memory estimate
    stats.total_memory_mb =
        (stats.ltm_stats.memory_bytes +
         stats.stm_sessions * 1024 * 100) / (1024 * 1024);

    return stats;
}

void MemorySystem::runMaintenance() {
    std::lock_guard<std::mutex> lock(mutex_);

    // Consolidate LTM
    ltm_->consolidate();

    // Clean up expired STM sessions
    stm_->cleanup();

    // Persist changes
    persist();
}

void MemorySystem::persist() {
    ltm_->persist();
    mtm_->persist();
}

void MemorySystem::load() {
    ltm_->load();
    mtm_->load();
}

void MemorySystem::consolidationLoop() {
    while (!stop_consolidation_) {
        std::this_thread::sleep_for(std::chrono::minutes(5));

        if (stop_consolidation_) break;

        try {
            runMaintenance();
        } catch (const std::exception& /*e*/) {
            // Log error but continue
        }
    }
}

// =============================================================================
// MemoryAwareContextBuilder Implementation
// =============================================================================

MemoryAwareContextBuilder::MemoryAwareContextBuilder(MemorySystem& memory)
    : memory_(memory) {
}

MemoryAwareContextBuilder::ContextPack MemoryAwareContextBuilder::buildReviewContext(
    const std::string& session_id,
    const std::string& diff,
    const std::vector<float>& diff_embedding,
    const std::vector<std::string>& changed_files,
    int max_tokens
) {
    ContextPack pack;

    // Start session if not already
    memory_.startSession(session_id);

    // Add diff to STM
    memory_.addContext(session_id, "diff", diff, diff_embedding);

    // Retrieve relevant context
    std::string query = "Review context for: " +
        (changed_files.empty() ? "code changes" : changed_files[0]);

    pack.retrieval_result = memory_.retrieve(
        session_id, query, diff_embedding, 20
    );

    // Extract components from retrieval result
    // Convert ltm_items (MemoryItem) to CodeMemory for the pack
    for (const auto& item : pack.retrieval_result.ltm_items) {
        CodeMemory cm;
        cm.id = item.id;
        cm.content = item.content;
        cm.embedding = item.embedding;
        cm.importance_score = item.importance_score;
        pack.code_chunks.push_back(std::move(cm));
    }

    pack.applicable_patterns = pack.retrieval_result.mtm_patterns;
    pack.suggested_strategies = pack.retrieval_result.mtm_strategies;

    // Build attention scores map from LTM attention weights
    for (size_t i = 0; i < pack.retrieval_result.ltm_items.size() &&
                        i < pack.retrieval_result.ltm_attention.size(); ++i) {
        pack.attention_scores[pack.retrieval_result.ltm_items[i].id] =
            static_cast<double>(pack.retrieval_result.ltm_attention[i]);
    }

    // Format combined context
    pack.combined_context = formatContext(pack.retrieval_result, max_tokens);

    // Add reasoning trace
    pack.reasoning_trace.push_back("Retrieved " +
        std::to_string(pack.code_chunks.size()) + " code chunks from LTM");
    pack.reasoning_trace.push_back("Matched " +
        std::to_string(pack.applicable_patterns.size()) + " patterns from MTM");
    if (!pack.suggested_strategies.empty()) {
        pack.reasoning_trace.push_back("Suggested strategy: " +
            pack.suggested_strategies[0].strategy_type);
    }

    return pack;
}

std::string MemoryAwareContextBuilder::formatContext(
    const MemoryRetrievalResult& result,
    int max_tokens
) {
    std::ostringstream context;
    int estimated_tokens = 0;

    // Add patterns first (usually short)
    if (!result.mtm_patterns.empty()) {
        context << "## Relevant Patterns\n\n";
        for (const auto& pattern : result.mtm_patterns) {
            std::string entry = "- " + pattern.rule_name + " (" + pattern.pattern_type + ")\n";
            estimated_tokens += static_cast<int>(entry.size()) / 4;
            if (estimated_tokens > max_tokens) break;
            context << entry;
        }
        context << "\n";
    }

    // Add strategy if available
    if (!result.mtm_strategies.empty()) {
        const auto& strategy = result.mtm_strategies[0];
        context << "## Review Strategy: " << strategy.strategy_type << "\n\n";
        context << "Context: " << strategy.context_type << "\n\n";
    }

    // Add code context
    context << "## Relevant Code Context\n\n";
    for (const auto& item : result.ltm_items) {
        std::string entry = "### " + item.id + "\n\n";
        entry += item.content + "\n\n";

        estimated_tokens += static_cast<int>(entry.size()) / 4;
        if (estimated_tokens > max_tokens) break;

        context << entry;
    }

    return context.str();
}

} // namespace aipr
