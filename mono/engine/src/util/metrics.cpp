/**
 * TMS Metrics Registry — Implementation
 *
 * See include/metrics.h for design notes.
 */

#include "metrics.h"
#include "perf_timer.h"

#include <algorithm>
#include <cmath>
#include <numeric>

namespace aipr {

namespace metrics {

// ── HistogramData ──────────────────────────────────────────────────────────

void Registry::HistogramData::observe(double v) {
    if (samples.size() < kMaxSamples) {
        samples.push_back(v);
    } else {
        samples[head] = v;
    }
    head = (head + 1) % kMaxSamples;
    count++;
    sum += v;
    if (v < min_val) min_val = v;
    if (v > max_val) max_val = v;
}

HistogramSnapshot Registry::HistogramData::snap() const {
    HistogramSnapshot s;
    s.count = count;
    s.sum   = sum;
    if (count == 0) return s;

    s.min_val = min_val;
    s.max_val = max_val;
    s.avg     = sum / static_cast<double>(count);

    // Sort a copy for percentiles (only over the ring window)
    std::vector<double> sorted(samples);
    std::sort(sorted.begin(), sorted.end());

    auto pct = [&](int p) -> double {
        if (sorted.empty()) return 0.0;
        size_t idx = static_cast<size_t>(
            std::ceil(static_cast<double>(p) / 100.0 * static_cast<double>(sorted.size())) - 1);
        if (idx >= sorted.size()) idx = sorted.size() - 1;
        return sorted[idx];
    };

    s.p50 = pct(50);
    s.p90 = pct(90);
    s.p95 = pct(95);
    s.p99 = pct(99);

    return s;
}

// ── Registry (singleton) ──────────────────────────────────────────────────

Registry& Registry::instance() {
    static Registry reg;
    return reg;
}

Registry::Registry()
    : start_time_(std::chrono::steady_clock::now()) {}

// ── Counters ──────────────────────────────────────────────────────────────

void Registry::incCounter(const std::string& name, double delta) {
    std::lock_guard<std::mutex> lk(mu_);
    counters_[name] += delta;
}

double Registry::getCounter(const std::string& name) const {
    std::lock_guard<std::mutex> lk(mu_);
    auto it = counters_.find(name);
    return (it != counters_.end()) ? it->second : 0.0;
}

// ── Gauges ────────────────────────────────────────────────────────────────

void Registry::setGauge(const std::string& name, double value) {
    std::lock_guard<std::mutex> lk(mu_);
    gauges_[name] = value;
}

double Registry::getGauge(const std::string& name) const {
    std::lock_guard<std::mutex> lk(mu_);
    auto it = gauges_.find(name);
    return (it != gauges_.end()) ? it->second : 0.0;
}

// ── Histograms ────────────────────────────────────────────────────────────

void Registry::observeHistogram(const std::string& name, double value) {
    std::lock_guard<std::mutex> lk(mu_);
    histograms_[name].observe(value);
}

HistogramSnapshot Registry::getHistogram(const std::string& name) const {
    std::lock_guard<std::mutex> lk(mu_);
    auto it = histograms_.find(name);
    if (it == histograms_.end()) return {};
    return it->second.snap();
}

// ── Snapshot ──────────────────────────────────────────────────────────────

Snapshot Registry::snapshot() const {
    std::lock_guard<std::mutex> lk(mu_);

    Snapshot snap;
    snap.timestamp_ms = static_cast<uint64_t>(
        std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::system_clock::now().time_since_epoch())
            .count());
    snap.uptime_s = static_cast<uint64_t>(
        std::chrono::duration_cast<std::chrono::seconds>(
            std::chrono::steady_clock::now() - start_time_)
            .count());

    for (const auto& [name, val] : counters_) {
        MetricValue mv;
        mv.type   = MetricType::COUNTER;
        mv.scalar = val;
        snap.metrics[name] = mv;
    }

    for (const auto& [name, val] : gauges_) {
        MetricValue mv;
        mv.type   = MetricType::GAUGE;
        mv.scalar = val;
        snap.metrics[name] = mv;
    }

    for (const auto& [name, hd] : histograms_) {
        MetricValue mv;
        mv.type      = MetricType::HISTOGRAM;
        mv.histogram = hd.snap();
        snap.metrics[name] = mv;
    }

    return snap;
}

// ── Bridge from perf::Profiler ────────────────────────────────────────────

void Registry::syncFromProfiler() {
    // Pull all timing data from perf::Profiler into our histogram registry.
    // Profiler stores raw millisecond samples; we convert to seconds for
    // consistency with our histogram naming convention (*_latency_s).
    auto& profiler = perf::Profiler::instance();

    auto names = profiler.getMetricNames();
    for (const auto& name : names) {
        auto stats = profiler.getStats(name);
        if (stats.count == 0) continue;

        // Use the profiler metric name prefixed with "perf_" to avoid
        // collisions with existing metrics registry names.
        std::string hist_name = "perf_" + name;

        // Record the average as a single representative sample.
        // The Profiler accumulates since startup; we can't replay every
        // individual sample, so we record avg, p50, p95, p99 as synthetic
        // observations that approximate the distribution.
        std::lock_guard<std::mutex> lk(mu_);
        auto& hd = histograms_[hist_name];

        // Only import new samples — track how many we've already imported.
        // We use the counter to avoid double-counting across sync calls.
        double prev_count = 0.0;
        auto cit = counters_.find(hist_name + "_synced_count");
        if (cit != counters_.end()) {
            prev_count = cit->second;
        }

        if (static_cast<double>(stats.count) > prev_count) {
            // Record percentile values as representative samples.
            // This gives the histogram a reasonable approximation of the
            // Profiler's distribution without replaying every raw sample.
            double ms_to_s = 0.001;
            hd.observe(stats.avg_ms * ms_to_s);
            hd.observe(stats.p50_ms * ms_to_s);
            hd.observe(stats.p95_ms * ms_to_s);
            hd.observe(stats.p99_ms * ms_to_s);

            counters_[hist_name + "_synced_count"] = static_cast<double>(stats.count);
        }

        // Also publish a gauge with the latest average.
        gauges_[hist_name + "_avg_s"] = stats.avg_ms * 0.001;
    }
}

// ── Reset ─────────────────────────────────────────────────────────────────

void Registry::reset() {
    std::lock_guard<std::mutex> lk(mu_);
    counters_.clear();
    gauges_.clear();
    histograms_.clear();
    start_time_ = std::chrono::steady_clock::now();
}

} // namespace metrics
} // namespace aipr
