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
#include <algorithm>

#include "tms/tms_memory_system.h"
#include "tms/tms_types.h"
#include "tms/repo_parser.h"
#include "tms/embedding_engine.h"

using namespace aipr::tms;

void printSeparator() {
    std::cout << "\n" << std::string(80, '=') << "\n";
}

void printResults(const TMSResponse& response) {
    std::cout << "\n\xF0\x9F\x93\x8A Query Results:\n";
    std::cout << "  Strategy used: " << static_cast<int>(response.compute_decision.strategy) << "\n";
    std::cout << "  Fused chunks: " << response.attention_output.fused_chunks.size() << "\n";
    std::cout << "  Patterns matched: " << response.matched_patterns.size() << "\n";
    std::cout << "  Strategies suggested: " << response.suggested_strategies.size() << "\n";
    std::cout << "  Total time: " << response.total_time.count() << " ms\n";
    std::cout << "  LTM items scanned: " << response.ltm_items_scanned << "\n";
    
    std::cout << "\n\xF0\x9F\x93\x84 Top Retrieved Code:\n";
    int count = 0;
    for (const auto& chunk : response.attention_output.fused_chunks) {
        if (count++ >= 3) break;
        std::cout << "\n  [" << count << "] " << chunk.chunk.file_path;
        if (!chunk.chunk.name.empty()) {
            std::cout << " :: " << chunk.chunk.name;
        }
        std::cout << " (lines " << chunk.chunk.start_line << "-" << chunk.chunk.end_line << ")";
        std::cout << " [score: " << chunk.combined_score << "]\n";
        
        // Show first 200 chars
        std::string preview = chunk.chunk.content.substr(0, 200);
        if (chunk.chunk.content.length() > 200) preview += "...";
        std::cout << "  +---------------------------------------------------------------------\n";
        std::cout << "  | " << preview << "\n";
        std::cout << "  +---------------------------------------------------------------------\n";
    }
    
    if (!response.matched_patterns.empty()) {
        std::cout << "\n\xF0\x9F\x8E\xAF Detected Patterns:\n";
        for (const auto& pattern : response.matched_patterns) {
            std::cout << "  - " << pattern.name << ": " << pattern.description << "\n";
        }
    }
    
    if (!response.attention_output.fused_context.empty()) {
        std::cout << "\n\xF0\x9F\x93\x8B Fused Context Preview (first 500 chars):\n";
        std::string preview = response.attention_output.fused_context.substr(
            0, std::min(size_t(500), response.attention_output.fused_context.size()));
        std::cout << preview << "...\n";
    }
    
    if (!response.reasoning_trace.empty()) {
        std::cout << "\n\xF0\x9F\x94\x8D Reasoning Trace:\n";
        for (const auto& step : response.reasoning_trace) {
            std::cout << "  > " << step << "\n";
        }
    }
}

int main(int argc, char* argv[]) {
    std::cout << R"(
+==============================================================================+
|       TMS Cognitive Memory System - Brain-Inspired RAG Demo                  |
|                                                                              |
|  "Turning massive monolithic repositories into intelligent code memory"      |
+==============================================================================+
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
    std::cout << "\xF0\x9F\x94\xA7 Step 1: Configuring TMS Cognitive Memory System\n";
    
    TMSConfig config;
    
    // LTM Configuration (Long-Term Memory with FAISS)
    config.embedding_dimension = 1536;
    config.ltm_capacity = 10000000;
    config.ltm_nlist = 4096;
    config.ltm_nprobe = 64;
    config.ltm_m = 32;
    config.ltm_bits = 8;
    config.ltm_use_gpu = false;
    config.ltm_default_top_k = 12;
    
    // STM Configuration (Short-Term Memory)
    config.stm_capacity = 100;
    config.stm_ttl = std::chrono::minutes{30};
    config.stm_default_top_k = 10;
    
    // MTM Configuration (Meta-Task Memory)
    config.mtm_pattern_capacity = 10000;
    config.mtm_strategy_capacity = 1000;
    config.mtm_confidence_threshold = 0.7;
    
    // Compute Controller Configuration
    config.vram_budget_gb = 4.0;
    config.enable_adaptive_strategy = true;
    
    // Cross-Memory Attention Configuration
    config.attention_num_heads = 8;
    config.attention_head_dim = 64;
    config.use_rotary_embedding = true;
    
    // Persistence
    config.storage_path = "./tms_data";
    
    std::cout << "  OK LTM: FAISS index, dim=" << config.embedding_dimension << "\n";
    std::cout << "  OK STM: Ring buffer, capacity=" << config.stm_capacity << "\n";
    std::cout << "  OK MTM: Pattern/Strategy graph, max_patterns=" << config.mtm_pattern_capacity << "\n";
    std::cout << "  OK Attention: " << config.attention_num_heads << " heads, RoPE enabled\n";

    // ==========================================================================
    // Step 2: Initialize the TMS System
    // ==========================================================================
    printSeparator();
    std::cout << "\xF0\x9F\x9A\x80 Step 2: Initializing TMS Memory System\n";
    
    TMSMemorySystem tms(config);
    
    try {
        tms.initialize();
        std::cout << "  OK All subsystems initialized successfully\n";
    } catch (const std::exception& e) {
        std::cerr << "  FAIL Failed to initialize TMS: " << e.what() << "\n";
        return 1;
    }

    // ==========================================================================
    // Step 3: Index a Repository (if provided)
    // ==========================================================================
    if (!repo_path.empty()) {
        printSeparator();
        std::cout << "\xF0\x9F\x93\x9A Step 3: Indexing Repository\n";
        std::cout << "  Path: " << repo_path << "\n";
        
        auto start = std::chrono::high_resolution_clock::now();
        
        auto progress_callback = [](float progress, const std::string& status) {
            std::cout << "\r  Progress: " << static_cast<int>(progress * 100) << "% - "
                      << status << std::flush;
        };
        
        try {
            tms.ingestRepository(repo_path, "demo_repo", progress_callback);
            std::cout << "\n";
            
            auto end = std::chrono::high_resolution_clock::now();
            auto duration = std::chrono::duration_cast<std::chrono::seconds>(end - start);
            std::cout << "  OK Repository indexed in " << duration.count() << " seconds\n";
            
            auto stats = tms.getStats();
            std::cout << "  OK Total chunks: " << stats.ltm_total_chunks << "\n";
            std::cout << "  OK Total repos: " << stats.ltm_total_repos << "\n";
            std::cout << "  OK MTM patterns: " << stats.mtm_patterns << "\n";
        } catch (const std::exception& e) {
            std::cerr << "\n  WARN Repository indexing failed: " << e.what() << "\n";
        }
    }

    // ==========================================================================
    // Step 4: Interactive Query Loop
    // ==========================================================================
    printSeparator();
    std::cout << "\xF0\x9F\x94\x8D Step 4: Interactive Query Mode\n";
    std::cout << "  Enter queries to search the codebase. Type 'quit' to exit.\n";
    std::cout << "\n  Example queries:\n";
    std::cout << "  - 'How does authentication flow through the system?'\n";
    std::cout << "  - 'Find all database connection code'\n";
    std::cout << "  - 'Show me error handling patterns'\n";
    std::cout << "  - 'Where is the main entry point?'\n";
    
    std::string query_text;
    std::string session_id = "demo_session";
    tms.startSession(session_id);
    
    while (true) {
        std::cout << "\n> ";
        std::getline(std::cin, query_text);
        
        if (query_text == "quit" || query_text == "exit" || query_text == "q") {
            break;
        }
        
        if (query_text.empty()) {
            continue;
        }
        
        // Execute forward pass through TMS using simplified query API
        try {
            auto response = tms.query(query_text, session_id);
            printResults(response);
        } catch (const std::exception& e) {
            std::cerr << "  Error: " << e.what() << "\n";
        }
    }
    
    tms.endSession(session_id);

    // ==========================================================================
    // Step 5: Print Final Statistics
    // ==========================================================================
    printSeparator();
    std::cout << "\xF0\x9F\x93\x88 Final Statistics\n";
    
    auto stats = tms.getStats();
    std::cout << "  Total queries processed: " << stats.total_queries << "\n";
    std::cout << "  Average query time: " << stats.avg_query_time_ms << " ms\n";
    std::cout << "  LTM chunks: " << stats.ltm_total_chunks << "\n";
    std::cout << "  LTM repos: " << stats.ltm_total_repos << "\n";
    std::cout << "  LTM index size: " << stats.ltm_index_size_mb << " MB\n";
    std::cout << "  STM active sessions: " << stats.stm_active_sessions << "\n";
    std::cout << "  MTM patterns: " << stats.mtm_patterns << "\n";
    std::cout << "  MTM strategies: " << stats.mtm_strategies << "\n";
    std::cout << "  Total memory usage: " << stats.total_memory_mb << " MB\n";
    
    printSeparator();
    std::cout << "\xF0\x9F\x91\x8B TMS Demo Complete. Thank you!\n\n";
    
    // Shutdown gracefully
    tms.shutdown();
    
    return 0;
}
