#!/usr/bin/env python3
"""Metrics calculation for AI-PR-Reviewer evaluation."""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

import numpy as np

if TYPE_CHECKING:
    from .run_eval import EvalResult


@dataclass
class EvaluationMetrics:
    """Computed evaluation metrics."""

    # Core metrics
    precision: float
    recall: float
    f1: float

    # Location metrics
    location_accuracy: float

    # Severity metrics
    severity_accuracy: float
    critical_recall: float

    # Category metrics
    category_accuracy: float

    # Latency metrics
    latency_p50: float
    latency_p95: float
    latency_p99: float
    latency_mean: float

    # Token metrics
    tokens_total: int
    tokens_per_comment: float

    # Counts
    total_tests: int
    successful_tests: int
    true_positives: int
    false_positives: int
    false_negatives: int


def compute_metrics(
    results: list[EvalResult],
    line_tolerance: int = 3,
    similarity_threshold: float = 0.7,
) -> EvaluationMetrics:
    """Compute evaluation metrics from results."""
    true_positives = 0
    false_positives = 0
    false_negatives = 0

    location_correct = 0
    location_total = 0

    severity_correct = 0
    severity_total = 0

    category_correct = 0
    category_total = 0

    critical_found = 0
    critical_total = 0

    latencies: list[float] = []
    tokens_total = 0
    comments_total = 0

    for result in results:
        if not result.success:
            # Count all expected as false negatives
            false_negatives += len(result.expected_comments)
            continue

        latencies.append(result.latency_ms)
        if result.tokens_used:
            tokens_total += result.tokens_used

        # Match comments
        actual = result.actual_comments
        expected = result.expected_comments

        matched_expected: set[int] = set()
        matched_actual: set[int] = set()

        for i, exp in enumerate(expected):
            exp_file = exp.get("file", "")
            exp_line = exp.get("line", 0)
            exp_severity = exp.get("severity", "").lower()
            exp_category = exp.get("category", "").lower()

            if exp_severity == "critical":
                critical_total += 1

            best_match: int | None = None
            best_score = 0.0

            for j, act in enumerate(actual):
                if j in matched_actual:
                    continue

                act_file = act.get("file", "")
                act_line = act.get("line", 0)

                # Check file match
                if exp_file != act_file:
                    continue

                # Check line proximity
                line_diff = abs(exp_line - act_line)
                if line_diff > line_tolerance:
                    continue

                # Compute match score
                score = 1.0 - (line_diff / (line_tolerance + 1))
                if score > best_score:
                    best_score = score
                    best_match = j

            if best_match is not None and best_score >= similarity_threshold:
                true_positives += 1
                matched_expected.add(i)
                matched_actual.add(best_match)

                act = actual[best_match]

                # Check location accuracy
                if exp_line == act.get("line", 0):
                    location_correct += 1
                location_total += 1

                # Check severity accuracy
                if exp_severity == act.get("severity", "").lower():
                    severity_correct += 1
                severity_total += 1

                # Check category accuracy
                if exp_category == act.get("category", "").lower():
                    category_correct += 1
                category_total += 1

                # Check critical recall
                if exp_severity == "critical":
                    critical_found += 1
            else:
                false_negatives += 1

        # Unmatched actual comments are false positives
        false_positives += len(actual) - len(matched_actual)
        comments_total += len(actual)

    # Compute derived metrics
    precision = true_positives / (true_positives + false_positives) if (true_positives + false_positives) > 0 else 0.0
    recall = true_positives / (true_positives + false_negatives) if (true_positives + false_negatives) > 0 else 0.0
    f1 = 2 * precision * recall / (precision + recall) if (precision + recall) > 0 else 0.0

    location_accuracy = location_correct / location_total if location_total > 0 else 0.0
    severity_accuracy = severity_correct / severity_total if severity_total > 0 else 0.0
    category_accuracy = category_correct / category_total if category_total > 0 else 0.0
    critical_recall = critical_found / critical_total if critical_total > 0 else 0.0

    # Latency percentiles
    latencies_array = np.array(latencies) if latencies else np.array([0.0])
    latency_p50 = float(np.percentile(latencies_array, 50))
    latency_p95 = float(np.percentile(latencies_array, 95))
    latency_p99 = float(np.percentile(latencies_array, 99))
    latency_mean = float(np.mean(latencies_array))

    # Token efficiency
    tokens_per_comment = tokens_total / comments_total if comments_total > 0 else 0.0

    return EvaluationMetrics(
        precision=precision,
        recall=recall,
        f1=f1,
        location_accuracy=location_accuracy,
        severity_accuracy=severity_accuracy,
        critical_recall=critical_recall,
        category_accuracy=category_accuracy,
        latency_p50=latency_p50,
        latency_p95=latency_p95,
        latency_p99=latency_p99,
        latency_mean=latency_mean,
        tokens_total=tokens_total,
        tokens_per_comment=tokens_per_comment,
        total_tests=len(results),
        successful_tests=sum(1 for r in results if r.success),
        true_positives=true_positives,
        false_positives=false_positives,
        false_negatives=false_negatives,
    )
