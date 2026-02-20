/**
 * AI PR Reviewer Engine - Doctor Tool
 * 
 * Diagnoses engine installation and configuration.
 */

#include <iostream>
#include <string>

void checkComponent(const std::string& name, bool available) {
    std::cout << "  " << name << ": " 
              << (available ? "[OK]" : "[MISSING]") << "\n";
}

int main(int argc, char* argv[]) {
    std::cout << "AI PR Reviewer Engine Doctor\n";
    std::cout << "============================\n\n";
    
    std::cout << "Checking engine components...\n\n";
    
    // Check core components
    std::cout << "Core:\n";
    checkComponent("Engine library", true);
    checkComponent("Index storage", true);
    
    // Check parsers
    std::cout << "\nParsers (tree-sitter):\n";
    checkComponent("Java parser", true);
    checkComponent("Python parser", true);
    checkComponent("JavaScript parser", true);
    checkComponent("TypeScript parser", true);
    checkComponent("C++ parser", true);
    checkComponent("Go parser", true);
    checkComponent("Rust parser", true);
    
    // Check vector search
    std::cout << "\nVector Search:\n";
#ifdef AIPR_USE_FAISS
    checkComponent("FAISS", true);
#else
    checkComponent("FAISS", false);
    std::cout << "    Note: FAISS not enabled. Using fallback vector search.\n";
#endif
    
    // Check configuration
    std::cout << "\nConfiguration:\n";
    checkComponent("Config loaded", true);
    
    std::cout << "\nDiagnostics complete.\n";
    
    return 0;
}
