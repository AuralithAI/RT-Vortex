/**
 * AI PR Reviewer Engine - Test Main
 */

#include <gtest/gtest.h>
#include "logging.h"
#include <iostream>

// Provide logMessage() for the test binary since the engine library
// only declares it in the header — the real definition lives in
// server/main.cpp which is not linked into tests.
namespace aipr {
void logMessage(LogLevel level, const std::string& msg) {
    const char* tag = "???";
    switch (level) {
        case LogLevel::DEBUG: tag = "DBG"; break;
        case LogLevel::INFO:  tag = "INF"; break;
        case LogLevel::WARN:  tag = "WRN"; break;
        case LogLevel::ERROR: tag = "ERR"; break;
        case LogLevel::FATAL: tag = "FTL"; break;
    }
    std::cerr << "[" << tag << "] " << msg << "\n";
}
} // namespace aipr

int main(int argc, char** argv) {
    testing::InitGoogleTest(&argc, argv);
    return RUN_ALL_TESTS();
}
