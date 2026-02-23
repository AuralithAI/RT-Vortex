/**
 * TMS Cognitive Memory System - Demo / Usage Example
 * 
 * This example demonstrates how to use the TMS (Transformer Memory System)
 * for intelligent code understanding in large monolithic repositories.
 * 
 * Build with: cmake --build . --target tms_demo
 * Run with:   ./bin/tms_demo /path/to/your/repo
 */

#include <iostream>
#include <string>
#include <chrono>

#include "tms/tms_memory_system.h"
#include "tms/tms_types.h"
#include "tms/repo_parser.h"
#include "tms/embedding_engine.h"

using namespace aipr::tms;

void printSeparator() {
    std::cout << "\n" << std::string(80, '=') << "\n";
}

void printResults(const ForwardResult& result) {
    std::cout << "\n📊 Query Results:\n";
    std::cout << "  Strategy used: " << static_cast<int>(result.strategy_used) << "\n";
    std::cout << "  Total retrieved: " << result.retrieved.size() << " chunks\n";
    std::cout << "  Patterns detected: " << result.patterns.size() << "\n";
    std::cout << "  Strategies suggested: " << result.strategies.size() << "\n";
    std::cout << "  Confidence: " << result.confidence << "\n";
    std::cout << "  Total time: " << result.total_time.count() << " µs\n";
    
    std::cout << "\n📄 Top Retrieved Code:\n";
    int count = 0;
    for (const auto& chunk : result.retrieved) {
        if (count++ >= 3) break;
        std::cout << "\n  [" << count << "] " << chunk.chunk.file_path;
        if (!chunk.chunk.name.empty()) {
            std::cout << " :: " << chunk.chunk.name;
        }
        std::cout << " (lines " << chunk.chunk.start_line << "-" << chunk.chunk.end_line << ")";
        std::cout << " [score: " << chunk.score << "]\n";
        
        // Show first 200 chars
        std::string preview = chunk.chunk.content.substr(0, 200);
        if (chunk.chunk.content.length() > 200) preview += "...";
        std::cout << "  ┌─────────────────────────────────────────────────────────────────\n";
        std::cout << "  │ " << preview << "\n";
        std::cout << "  └─────────────────────────────────────────────────────────────────\n";
    }
    
    if (!result.patterns.empty()) {
        std::cout << "\n🎯 Detected Patterns:\n";
        for (const auto& pattern : result.patterns) {
            std::cout << "  - " << pattern.name << ": " << pattern.description << "\n";
        }
    }
    
    if (!result.fused_context.empty()) {
        std::cout << "\n📋 Fused Context Preview (first 500 chars):\n";
        std::cout << result.fused_context.substr(0, 500) << "...\n";
    }
}

int main(int argc, char* argv[]) {
    std::cout << R"(
╔══════════════════════════════════════════════════════════════════════════════╗
║       TMS Cognitive Memory System - Brain-Inspired RAG Demo                  ║
║                                                                              ║
║  "Turning massive monolithic repositories into intelligent code memory"     ║
╚══════════════════════════════════════════════════════════════════════════════╝
)" << std::endl;

    std::string repo_path;
    if (argc >= 2) {
        repo_path = argv[1];
    } else {
        std::cout << "Usage: tms_demo <repo_path>\n";
        std::cout << "\nNo repo path provided. Running with synthetic demo...\n";
        repo_path = "";
    }

    // ==========================================================================
    // Step 1: Configure the TMS System
    // ==========================================================================
    printSeparator();
    std::cout << "🔧 Step 1: Configuring TMS Cognitive Memory System\n";
    
    TMSConfig config;
    
    // LTM Configuration (Long-Term Memory with FAISS)
    config.ltm_config.index_type = "IVF";  // IVF for large repos, HNSW for smaller
    config.ltm_config.dimension = 1536;     // OpenAI ada-002 dimension
    config.ltm_config.nlist = 100;          // Number of Voronoi cells
    config.ltm_config.nprobe = 10;          // Cells to probe during search
    config.ltm_config.use_pq = true;        // Product quantization for memory efficiency
    config.ltm_config.pq_m = 64;            // PQ subquantizers
    config.ltm_config.pq_nbits = 8;         // Bits per subquantizer
    
    // STM Configuration (Short-Term Memory)
    config.stm_config.max_queries = 100;
    config.stm_config.max_retrievals = 500;
    config.stm_config.decay_rate = 0.95;
    config.stm_config.relevance_threshold = 0.3;
    
    // MTM Configuration (Meta-Task Memory)
    config.mtm_config.max_patterns = 1000;
    config.mtm_config.max_strategies = 100;
    config.mtm_config.learning_rate = 0.1;
    config.mtm_config.activation_decay = 0.9;
    
    // Compute Controller Configuration
    config.compute_config.input_dim = 64;
    config.compute_config.hidden_dim = 128;
    config.compute_config.default_strategy = ComputeStrategy::BALANCED;
    
    // Cross-Memory Attention Configuration
    config.attention_config.num_heads = 8;
    config.attention_config.embed_dim = 1536;
    config.attention_config.ffn_dim = 4096;
    config.attention_config.use_rotary_embedding = true;
    
    // Global settings
    config.embedding_dim = 1536;
    config.default_top_k = 10;
    config.reranking_enabled = true;
    config.context_window_size = 8192;
    
    std::cout << "  ✓ LTM: FAISS " << config.ltm_config.index_type 
              << " index, dim=" << config.ltm_config.dimension << "\n";
    std::cout << "  ✓ STM: Ring buffer, max_queries=" << config.stm_config.max_queries << "\n";
    std::cout << "  ✓ MTM: Pattern/Strategy graph, max_patterns=" << config.mtm_config.max_patterns << "\n";
    std::cout << "  ✓ Attention: " << config.attention_config.num_heads << " heads, RoPE enabled\n";

    // ==========================================================================
    // Step 2: Initialize the TMS System
    // ==========================================================================
    printSeparator();
    std::cout << "🚀 Step 2: Initializing TMS Memory System\n";
    
    TMSMemorySystem tms(config);
    
    if (!tms.initialize()) {
        std::cerr << "❌ Failed to initialize TMS: " << tms.getLastError() << "\n";
        return 1;
    }
    
    std::cout << "  ✓ All subsystems initialized successfully\n";

    // ==========================================================================
    // Step 3: Index a Repository (if provided)
    // ==========================================================================
    if (!repo_path.empty()) {
        printSeparator();
        std::cout << "📚 Step 3: Indexing Repository\n";
        std::cout << "  Path: " << repo_path << "\n";
        
        auto start = std::chrono::high_resolution_clock::now();
        
        IndexOptions options;
        options.incremental = false;  // Full re-index
        options.parallel = true;
        options.batch_size = 100;
        
        auto progress_callback = [](const IndexProgress& progress) {
            std::cout << "\r  Progress: " << progress.files_processed << "/" 
                      << progress.total_files << " files, "
                      << progress.chunks_indexed << " chunks indexed" << std::flush;
        };
        
        bool success = tms.ingestRepository(repo_path, options, progress_callback);
        std::cout << "\n";
        
        auto end = std::chrono::high_resolution_clock::now();
        auto duration = std::chrono::duration_cast<std::chrono::seconds>(end - start);
        
        if (success) {
            std::cout << "  ✓ Repository indexed in " << duration.count() << " seconds\n";
            
            auto stats = tms.getStats();
            std::cout << "  ✓ Total chunks: " << stats.total_chunks << "\n";
            std::cout << "  ✓ Total files: " << stats.total_files << "\n";
            std::cout << "  ✓ LTM index size: " << stats.ltm_stats.index_size << " vectors\n";
        } else {
            std::cerr << "  ⚠ Repository indexing completed with warnings\n";
        }
    }

    // ==========================================================================
    // Step 4: Interactive Query Loop
    // ==========================================================================
    printSeparator();
    std::cout << "🔍 Step 4: Interactive Query Mode\n";
    std::cout << "  Enter queries to search the codebase. Type 'quit' to exit.\n";
    std::cout << "\n  Example queries:\n";
    std::cout << "  - 'How does authentication flow through the system?'\n";
    std::cout << "  - 'Find all database connection code'\n";
    std::cout << "  - 'Show me error handling patterns'\n";
    std::cout << "  - 'Where is the main entry point?'\n";
    
    std::string query;
    while (true) {
        std::cout << "\n> ";
        std::getline(std::cin, query);
        
        if (query == "quit" || query == "exit" || query == "q") {
            break;
        }
        
        if (query.empty()) {
            continue;
        }
        
        // Create query context
        QueryContext ctx;
        ctx.query = query;
        ctx.top_k = 10;
        ctx.include_patterns = true;
        ctx.include_strategies = true;
        
        // Execute forward pass through TMS
        auto result = tms.forward(ctx);
        
        // Display results
        printResults(result);
    }

    // ==========================================================================
    // Step 5: Print Final Statistics
    // ==========================================================================
    printSeparator();
    std::cout << "📈 Final Statistics\n";
    
    auto stats = tms.getStats();
    std::cout << "  Total queries processed: " << stats.total_queries << "\n";
    std::cout << "  Average query time: " << stats.avg_query_time_us << " µs\n";
    std::cout << "  Cache hit rate: " << (stats.cache_hits * 100.0 / std::max(1ul, stats.cache_hits + stats.cache_misses)) << "%\n";
    std::cout << "  LTM searches: " << stats.ltm_stats.total_searches << "\n";
    std::cout << "  STM lookups: " << stats.stm_stats.total_lookups << "\n";
    std::cout << "  MTM pattern matches: " << stats.mtm_stats.total_pattern_matches << "\n";
    
    printSeparator();
    std::cout << "👋 TMS Demo Complete. Thank you!\n\n";
    
    return 0;
}
