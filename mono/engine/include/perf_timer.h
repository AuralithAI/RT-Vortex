/**
 * AI PR Reviewer — Profiler Public Interface
 *
 * Timing infrastructure for profiling engine operations.
 * Used by metrics::Registry::syncFromProfiler() to bridge profiler
 * samples into the Prometheus-compatible metrics registry.
 */

#pragma once

#include <chrono>
#include <mutex>
#include <string>
#include <unordered_map>
#include <vector>

namespace aipr {
namespace perf {

using Clock     = std::chrono::high_resolution_clock;
using TimePoint = Clock::time_point;

/**
 * Timing statistics for a single named metric.
 */
struct TimingStats {
    size_t count    = 0;
    double total_ms = 0.0;
    double min_ms   = 0.0;
    double max_ms   = 0.0;
    double avg_ms   = 0.0;
    double p50_ms   = 0.0;
    double p95_ms   = 0.0;
    double p99_ms   = 0.0;

    std::string toString() const;
};

/**
 * Profiler singleton — collects high-resolution timing samples.
 */
class Profiler {
public:
    static Profiler& instance();

    /** Record a timing sample (milliseconds). */
    void record(const std::string& name, double ms);

    /** Start a named timer. */
    void start(const std::string& name);

    /** Stop a named timer and record the elapsed time. */
    void stop(const std::string& name);

    /** Get statistics for a named metric. */
    TimingStats getStats(const std::string& name) const;

    /** Get all recorded metric names. */
    std::vector<std::string> getMetricNames() const;

    /** Clear all recorded timings. */
    void clear();

    /** Generate a human-readable report. */
    std::string report() const;

private:
    Profiler() = default;

    double percentile(const std::vector<double>& sorted, int p) const;

    mutable std::mutex mutex_;
    std::unordered_map<std::string, std::vector<double>> timings_;
    std::unordered_map<std::string, TimePoint> active_timers_;
};

/**
 * Simple scoped timer (start on construction, query elapsed on demand).
 */
class ScopedTimer {
public:
    ScopedTimer() : start_(Clock::now()) {}

    double elapsedMs() const {
        return std::chrono::duration<double, std::milli>(Clock::now() - start_).count();
    }
    double elapsedUs() const {
        return std::chrono::duration<double, std::micro>(Clock::now() - start_).count();
    }
    double elapsedNs() const {
        return std::chrono::duration<double, std::nano>(Clock::now() - start_).count();
    }
    void reset() { start_ = Clock::now(); }

private:
    TimePoint start_;
};

/**
 * RAII timer that records to Profiler on destruction.
 */
class AutoTimer {
public:
    explicit AutoTimer(const std::string& name) : name_(name) {}
    ~AutoTimer() { Profiler::instance().record(name_, timer_.elapsedMs()); }

    AutoTimer(const AutoTimer&) = delete;
    AutoTimer& operator=(const AutoTimer&) = delete;

private:
    std::string name_;
    ScopedTimer timer_;
};

// Convenience macros
#define AIPR_PROFILE_SCOPE(name)    aipr::perf::AutoTimer _aipr_timer_##__LINE__(name)
#define AIPR_PROFILE_FUNCTION()     aipr::perf::AutoTimer _aipr_timer_##__LINE__(__func__)

/**
 * Token-bucket rate limiter.
 */
class RateLimiter {
public:
    explicit RateLimiter(size_t max_per_second);
    bool tryAcquire();
    void waitAndAcquire();

private:
    void refill();

    size_t max_per_second_;
    size_t tokens_;
    TimePoint last_update_;
    std::mutex mutex_;
};

} // namespace perf
} // namespace aipr
