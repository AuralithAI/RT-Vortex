/**
 * RTVortex Engine — Shared Logging
 *
 * Provides LOG_DEBUG / LOG_INFO / LOG_WARN / LOG_ERROR / LOG_FATAL macros
 * that can be used from any translation unit.  The actual logMessage()
 * function is defined once in main.cpp and linked in.
 */

#ifndef AIPR_LOGGING_H
#define AIPR_LOGGING_H

#include <string>

namespace aipr {

enum class LogLevel { DEBUG, INFO, WARN, ERROR, FATAL };

/// Implemented in main.cpp — writes to console + log file.
void logMessage(LogLevel level, const std::string& msg);

} // namespace aipr

#define LOG_DEBUG(msg) ::aipr::logMessage(::aipr::LogLevel::DEBUG, msg)
#define LOG_INFO(msg)  ::aipr::logMessage(::aipr::LogLevel::INFO,  msg)
#define LOG_WARN(msg)  ::aipr::logMessage(::aipr::LogLevel::WARN,  msg)
#define LOG_ERROR(msg) ::aipr::logMessage(::aipr::LogLevel::ERROR, msg)
#define LOG_FATAL(msg) ::aipr::logMessage(::aipr::LogLevel::FATAL, msg)

#endif // AIPR_LOGGING_H
