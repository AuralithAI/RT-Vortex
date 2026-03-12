/**
 * AI PR Reviewer — Profiler Public Interface
 *
 * Thin header exposing perf::Profiler's read-only API so that other
 * compilation units (e.g. metrics.cpp) can pull timing data without
 * including the full perf_timer.cpp internals.
 */

#pragma once

#include <string>
#include <vector>

namespace aipr {
namespace perf {

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
};

/**
 * Profiler singleton — collects high-resolution timing samples.
 *
 * The full implementation lives in perf_timer.cpp.  This header exposes
 * only the read-only query surface needed by metrics::Registry.
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
    Profiler();
    // Members live in perf_timer.cpp — this header only declares the API.
};

} // namespace perf
} // namespace aipr
