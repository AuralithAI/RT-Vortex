/**
 * TMS Cognitive Memory System - Core Types
 * 
 * Brain-inspired memory architecture for massive monorepo code understanding.
 * Optimized for 500k–5M+ LOC repositories with human-like reasoning.
 * 
 * System Overview:
 * ┌─────────────────────────────────────────────────────────────────────────┐
 * │                    TMS Cognitive Memory System                          │
 * │                                                                         │
 * │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────────────────┐ │
 * │  │    LTM    │  │    STM    │  │    MTM    │  │  Compute Controller   │ │
 * │  │  (FAISS)  │◄─┤  (Ring)   │◄─┤  (Graph)  │◄─┤  (Strategy Selector)  │ │
 * │  │           │  │           │  │           │  │                       │ │
 * │  │ Permanent │  │ Session   │  │ Patterns  │  │ FAST/BALANCED/THOROUGH│ │
 * │  │ Knowledge │  │ Context   │  │ Strategies│  │                       │ │
 * │  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘  └───────────┬───────────┘ │
 * │        │              │              │                    │             │
 * │        └──────────────┴──────────────┴────────────────────┘             │
 * │                              │                                          │
 * │                    ┌─────────▼─────────┐                                │
 * │                    │ Cross-Memory      │                                │
 * │                    │ Attention Module  │                                │
 * │                    │ (Multi-Head Attn) │                                │
 * │                    └─────────┬─────────┘                                │
 * │                              │                                          │
 * │                    ┌─────────▼─────────┐                                │
 * │                    │  Fused Context    │                                │
 * │                    │  (For LLM Input)  │                                │
 * │                    └───────────────────┘                                │
 * └─────────────────────────────────────────────────────────────────────────┘
 */

#pragma once

#include <string>
#include <vector>
#include <map>
#include <unordered_map>
#include <chrono>
#include <cstdint>
#include <optional>

namespace aipr::tms {

// =============================================================================
// Code Chunk (Fundamental Unit of Indexing)
// =============================================================================

/**
 * CodeChunk - The atomic unit of code understanding
 * 
 * Each chunk represents a semantic unit of code with rich metadata
 * for context reconstruction during retrieval.
 */
struct CodeChunk {
    std::string id;                         // Unique identifier
    std::string file_path;                  // Relative path within repo
    std::string language;                   // Language identifier (cpp, java, python, etc.)
    std::string type;                       // "function", "class", "file_summary", "module", "dependency"
    std::string name;                       // Symbol name (function/class name)
    std::string qualified_name;             // Fully qualified name (e.g., namespace::class::method)
    std::string content;                    // Code content (including docstring)
    std::string signature;                  // Function/method signature
    std::string docstring;                  // Extracted documentation
    
    // Structural metadata
    std::vector<std::string> dependencies;  // Import/include dependencies
    std::vector<std::string> callees;       // Functions this chunk calls
    std::vector<std::string> callers;       // Functions that call this chunk
    std::vector<std::string> symbols;       // All symbols defined in this chunk
    std::vector<std::string> references;    // External symbol references
    
    // Position metadata
    int start_line = 0;
    int end_line = 0;
    int start_byte = 0;
    int end_byte = 0;
    int indent_level = 0;
    
    // Parent context
    std::string parent_chunk_id;            // ID of parent scope (class for method, etc.)
    std::string parent_name;                // Name of parent scope
    
    // Semantic metadata
    std::vector<std::string> tags;          // Semantic tags (e.g., "authentication", "database")
    double complexity_score = 0.0;          // Cyclomatic complexity estimate
    double importance_score = 1.0;          // Relevance weighting
    
    // Timestamps
    std::chrono::system_clock::time_point created_at;
    std::chrono::system_clock::time_point last_modified;
    std::string content_hash;               // For change detection
};

// =============================================================================
// Chunking Strategy Configuration
// =============================================================================

/**
 * ChunkStrategy - Defines how code is divided into chunks
 */
enum class ChunkType {
    FILE_SUMMARY,       // High-level file/module summary
    CLASS_MODULE,       // Class or module level
    FUNCTION_METHOD,    // Function/method with signature + body + docstring
    DEPENDENCY_GRAPH,   // Import/dependency relationships
    CROSS_FILE_CONTEXT, // Cross-file context window (N functions around call site)
    INLINE_COMMENT,     // Important inline comments
    TEST_CASE           // Test cases with assertions
};

struct ChunkingConfig {
    // Size constraints (approximate token counts)
    size_t target_chunk_tokens = 512;
    size_t max_chunk_tokens = 1024;
    size_t min_chunk_tokens = 50;
    
    // Overlap for context continuity
    size_t overlap_lines = 3;
    size_t cross_file_context_radius = 3;  // Functions around call site
    
    // Strategy flags
    bool preserve_functions = true;         // Keep functions as atomic units
    bool include_parent_context = true;     // Include enclosing scope info
    bool extract_dependencies = true;       // Parse import/include statements
    bool extract_call_graph = true;         // Build function call relationships
    bool generate_file_summaries = true;    // Create file-level summary chunks
    bool hierarchy_enabled = false;         // Enable hierarchical context prefixing
    
    // Language-specific settings
    std::map<std::string, size_t> language_chunk_sizes;  // Override per language
};

// =============================================================================
// Memory Metadata (Shared across memory types)
// =============================================================================

struct MemoryMetadata {
    std::string id;
    std::chrono::system_clock::time_point created_at;
    std::chrono::system_clock::time_point last_accessed;
    int access_count = 0;
    double importance_score = 1.0;
    double decay_factor = 1.0;              // For time-based decay
    std::map<std::string, std::string> extra;
};

// =============================================================================
// Retrieved Chunk (Result from memory search)
// =============================================================================

struct RetrievedChunk {
    CodeChunk chunk;
    float similarity_score = 0.0f;          // Vector similarity (0-1)
    float lexical_score = 0.0f;             // BM25/keyword match score
    float combined_score = 0.0f;            // Final score (after RRF fusion)
    float attention_weight = 0.0f;          // Cross-memory attention weight
    std::string memory_source;              // "LTM", "STM", "MTM"
    MemoryMetadata metadata;
};

// =============================================================================
// Pattern Memory (For MTM)
// =============================================================================

/**
 * PatternEntry - A learned code pattern
 * 
 * Examples:
 * - "Authentication flow pattern"
 * - "Error handling anti-pattern"
 * - "Resource cleanup pattern"
 */
struct PatternEntry {
    std::string id;
    std::string name;
    std::string description;
    std::string pattern_type;               // "bug", "security", "performance", "architecture"
    std::vector<float> embedding;           // Pattern embedding for matching
    
    // Pattern examples
    std::vector<std::string> example_chunk_ids;
    std::vector<std::string> example_snippets;
    
    // Effectiveness tracking
    double confidence = 0.5;
    int occurrence_count = 0;
    int true_positive_count = 0;
    int false_positive_count = 0;
    
    // Applicability
    std::vector<std::string> applicable_languages;
    std::vector<std::string> applicable_contexts;
    
    MemoryMetadata metadata;
};

// =============================================================================
// Strategy Memory (For MTM)
// =============================================================================

/**
 * StrategyEntry - A review/analysis strategy
 * 
 * Examples:
 * - "Security-focused review for auth code"
 * - "Performance analysis for database queries"
 * - "Refactoring suggestions for legacy code"
 */
struct StrategyEntry {
    std::string id;
    std::string name;
    std::string description;
    std::string strategy_type;              // "review", "analysis", "refactor", "explain"
    std::string context_type;               // "security", "performance", "architecture"
    
    // Strategy content
    std::string prompt_template;            // Template for LLM prompt
    std::vector<std::string> focus_areas;   // What to focus on
    std::vector<std::string> applicable_pattern_ids;
    
    // Effectiveness tracking
    double effectiveness_score = 0.5;
    int use_count = 0;
    int success_count = 0;
    
    MemoryMetadata metadata;
};

// =============================================================================
// Compute Strategy (From Controller)
// =============================================================================

/**
 * ComputeStrategy - How much compute to spend on retrieval/attention
 */
enum class ComputeStrategy {
    FAST,               // Quick retrieval, minimal attention (< 100ms)
    BALANCED,           // Standard retrieval, moderate attention (< 500ms)
    THOROUGH            // Deep retrieval, full attention (< 2000ms)
};

struct ComputeDecision {
    ComputeStrategy strategy;
    int ltm_top_k;                          // How many items to retrieve from LTM
    int stm_top_k;                          // How many items from STM
    int mtm_top_k;                          // How many patterns/strategies from MTM
    bool enable_cross_attention;            // Whether to run cross-memory attention
    int attention_heads;                    // Number of attention heads to use
    float memory_budget_mb;                 // VRAM budget
    std::string reasoning;                  // Why this decision was made
};

// =============================================================================
// Cross-Memory Attention Output
// =============================================================================

struct CrossMemoryOutput {
    // Fused results
    std::vector<RetrievedChunk> fused_chunks;
    std::string fused_context;              // Concatenated context for LLM
    std::vector<float> fused_embedding;     // Combined embedding
    
    // Attention weights (for explainability)
    std::vector<float> ltm_attention_weights;
    std::vector<float> stm_attention_weights;
    std::vector<float> mtm_attention_weights;
    
    // Per-head weights (for debugging)
    std::vector<std::vector<float>> per_head_ltm_weights;
    std::vector<std::vector<float>> per_head_stm_weights;
    std::vector<std::vector<float>> per_head_mtm_weights;
    
    // Metrics
    double confidence_score = 0.0;
    std::chrono::milliseconds computation_time{};
    size_t total_tokens_in_context = 0;
};

// =============================================================================
// TMS Configuration
// =============================================================================

struct TMSConfig {
    // Embedding configuration
    size_t embedding_dimension = 384;       // Embedding vector size (384 for MiniLM)
    std::string embedding_model = "all-MiniLM-L6-v2";
    std::string embedding_backend = "onnx"; // "onnx", "http", "mock"
    std::string onnx_model_path;            // Path to ONNX model file
    std::string onnx_tokenizer_path;        // Path to tokenizer.json
    std::string embed_api_endpoint;         // HTTP API endpoint (for http backend)
    std::string embed_api_key;              // HTTP API key (for http backend)
    
    // LTM Configuration (FAISS)
    size_t ltm_capacity = 10000000;         // 10M vectors for large repos
    int ltm_nlist = 4096;                   // IVF cluster count (for IndexIVFPQ)
    int ltm_nprobe = 64;                    // Clusters to probe during search
    int ltm_m = 32;                         // PQ subquantizers
    int ltm_bits = 8;                       // Bits per subquantizer
    bool ltm_use_gpu = false;               // GPU acceleration
    int ltm_default_top_k = 12;
    
    // STM Configuration (Ring Buffer)
    size_t stm_capacity = 100;              // Recent queries/retrievals
    std::chrono::minutes stm_ttl{30};       // Time-to-live
    int stm_default_top_k = 10;
    
    // MTM Configuration (Graph)
    size_t mtm_pattern_capacity = 10000;
    size_t mtm_strategy_capacity = 1000;
    double mtm_confidence_threshold = 0.7;
    
    // Cross-Memory Attention
    int attention_num_heads = 8;
    int attention_head_dim = 64;
    double attention_dropout = 0.1;
    bool use_rotary_embedding = true;
    int max_sequence_length = 8192;
    
    // Compute Controller
    float vram_budget_gb = 4.0;             // Available VRAM
    bool enable_adaptive_strategy = true;
    
    // Memory Consolidation
    bool enable_consolidation = true;
    std::chrono::hours consolidation_interval{24};
    double consolidation_threshold = 0.3;   // Min importance to keep
    
    // Persistence
    std::string storage_path = "./tms_data";
    bool persist_stm = false;               // STM is usually ephemeral
    bool persist_mtm = true;

    // Hierarchical chunking
    bool hierarchy_enabled = false;         // Enable hierarchical context prefixing

    // Knowledge Graph (persistent architectural understanding)
    bool knowledge_graph_enabled = true;    // SQLite-backed KG for structural edges

    // Memory Accounts (domain-aware query routing)
    bool memory_accounts_enabled = true;    // Enable DEV/OPS/SECURITY/HISTORY routing

    // Confidence Gate (Zero-LLM Engine)
    bool confidence_gate_enabled = false;   // When true, skip LLM if retrieval is high-confidence
    float confidence_gate_threshold = 0.85f; // Min max-retrieval score to skip LLM
    int query_timeout_seconds = 5;          // Hard timeout for forward() queries

    // Config versioning
    uint32_t config_version = 1;            // Schema version for storage migration
    bool auto_migrate = false;              // Auto-migrate storage on version bump
};

// =============================================================================
// Query Types
// =============================================================================

/**
 * TMSQuery - Input to the TMS system
 */
struct TMSQuery {
    std::string query_text;                 // Natural language query
    std::vector<float> query_embedding;     // Pre-computed embedding (optional)
    std::string session_id;                 // Session for STM lookup
    
    // Filters
    std::string repo_filter;                // Limit to specific repo
    std::vector<std::string> language_filter;
    std::vector<std::string> file_path_patterns;
    
    // Hints
    std::vector<std::string> hint_files;    // Files likely relevant
    std::vector<std::string> hint_symbols;  // Symbols likely relevant
    
    // Strategy override
    std::optional<ComputeStrategy> force_strategy;
};

/**
 * TMSResponse - Output from the TMS system
 */
struct TMSResponse {
    // Main output
    CrossMemoryOutput attention_output;
    ComputeDecision compute_decision;
    
    // Applicable patterns and strategies
    std::vector<PatternEntry> matched_patterns;
    std::vector<StrategyEntry> suggested_strategies;
    
    // Metrics
    std::chrono::milliseconds total_time;
    size_t ltm_items_scanned;
    size_t stm_items_scanned;
    size_t mtm_items_scanned;
    
    // Confidence gate (Zero-LLM Engine)
    bool requires_llm = true;               // False when gate fires → caller may skip LLM
    float max_retrieval_score = 0.0f;        // Highest similarity in retrieval results
    std::string llm_skip_reason;             // Human-readable reason when requires_llm == false
    
    // Explainability
    std::vector<std::string> reasoning_trace;
};

} // namespace aipr::tms
