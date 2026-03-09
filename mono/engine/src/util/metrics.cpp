/**
 * TMS Metrics Registry — Implementation
 *
 * See include/metrics.h for design notes.
 */

#include "metrics.h"

#include <algorithm>
#include <cmath>
#include <numeric>

namespace aipr {

// Forward-declare Profiler access (perf_timer.cpp is in the same library).
namespace perf {
class Profiler;
} // namespace perf

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
    // TODO: We access Profiler::instance() which lives in perf_timer.cpp.
    // Both are linked into the same static lib, so this always works.
    //
    // We intentionally DON'T #include perf_timer's header (it's a .cpp-only
    // class) — instead we declare the minimal API we need.
    //
    // For now this is a no-op placeholder. A future commit can expose
    // Profiler's getMetricNames() + getStats() through a thin header and
    // call them here to copy samples into our histograms.
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
