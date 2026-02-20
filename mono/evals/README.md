# AI-PR-Reviewer Evaluation Framework

This directory contains the evaluation framework for measuring and benchmarking the AI-PR-Reviewer system.

## Overview

The evaluation framework helps:
1. **Measure accuracy** of code review comments
2. **Benchmark performance** across different model configurations
3. **Track regression** over time
4. **Compare** different retrieval strategies

## Structure

```
evals/
├── datasets/                 # Test datasets
│   ├── security/            # Security-focused PRs
│   ├── performance/         # Performance-focused PRs
│   ├── testing/             # Testing-focused PRs
│   └── mixed/               # General PRs
├── scripts/                  # Evaluation scripts
│   ├── run_eval.py          # Main evaluation runner
│   ├── metrics.py           # Metric calculations
│   └── report.py            # Report generation
├── configs/                  # Evaluation configurations
│   └── default.yaml         # Default eval settings
├── baselines/                # Baseline results
└── results/                  # Evaluation results
```

## Quick Start

```bash
# Install dependencies
pip install -e .[dev]

# Run evaluation
python scripts/run_eval.py --dataset datasets/mixed --config configs/default.yaml

# Generate report
python scripts/report.py --results results/latest.json --output report.html
```

## Datasets

Each dataset contains annotated pull requests with expected review comments:

```json
{
  "pr_id": "security-001",
  "repository": "example/repo",
  "files": [
    {
      "path": "src/auth.py",
      "patch": "...",
      "expected_comments": [
        {
          "line": 42,
          "severity": "critical",
          "category": "security",
          "pattern": "sql_injection"
        }
      ]
    }
  ]
}
```

## Metrics

### Comment Quality Metrics
- **Precision**: Correct comments / Total comments
- **Recall**: Found issues / Total issues
- **F1 Score**: Harmonic mean of precision and recall
- **Location Accuracy**: Correct line numbers / Total comments

### Severity Metrics
- **Severity Accuracy**: Correct severity / Total comments
- **Critical Recall**: Found critical issues / Total critical issues

### Performance Metrics
- **Latency P50/P95/P99**: Response time percentiles
- **Throughput**: Reviews per minute
- **Token Efficiency**: Accuracy per token used

## Adding New Datasets

1. Create a directory under `datasets/`
2. Add PR JSON files following the schema
3. Run validation: `python scripts/validate_dataset.py datasets/new_dataset`

## License

Apache License 2.0
