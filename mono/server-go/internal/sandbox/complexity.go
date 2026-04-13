package sandbox

import (
	"math"
	"sort"
	"strings"
	"time"
)

// BuildComplexityLabel classifies build difficulty.
type BuildComplexityLabel string

const (
	BuildComplexityTrivial  BuildComplexityLabel = "trivial"
	BuildComplexitySimple   BuildComplexityLabel = "simple"
	BuildComplexityModerate BuildComplexityLabel = "moderate"
	BuildComplexityHigh     BuildComplexityLabel = "high"
	BuildComplexityCritical BuildComplexityLabel = "critical"
)

const (
	complexityThresholdTrivial  = 0.10
	complexityThresholdSimple   = 0.30
	complexityThresholdModerate = 0.55
	complexityThresholdHigh     = 0.80
)

// BuildComplexity captures multi-dimensional build difficulty signals.
type BuildComplexity struct {
	Score             float64              `json:"score"`
	Label             BuildComplexityLabel `json:"label"`
	Signals           BuildSignals         `json:"signals"`
	ResourceHints     ResourceHints        `json:"resource_hints"`
	DurationBucket    string               `json:"duration_bucket"`
	FailureProbablity float64              `json:"failure_probability"`
}

// BuildSignals are raw inputs used to compute build complexity.
type BuildSignals struct {
	BuildSystem    string  `json:"build_system"`
	FileCount      int     `json:"file_count"`
	WorkspaceBytes int64   `json:"workspace_bytes"`
	SecretCount    int     `json:"secret_count"`
	HasPreCommands bool    `json:"has_pre_commands"`
	LanguageCount  int     `json:"language_count"`
	ArtifactCount  int     `json:"artifact_count"`
	CoverageFound  bool    `json:"coverage_found"`
	DurationSec    float64 `json:"duration_sec"`
	ExitCode       int     `json:"exit_code"`
	RetryCount     int     `json:"retry_count"`
	CacheHit       bool    `json:"cache_hit"`
}

// ResourceHints are recommended container resource settings.
type ResourceHints struct {
	TimeoutSec  int    `json:"timeout_sec"`
	MemoryLimit string `json:"memory_limit"`
	CPULimit    string `json:"cpu_limit"`
}

// ExtractBuildSignals extracts complexity signals from a build plan and result.
func ExtractBuildSignals(plan *BuildPlan, result *BuildResult) BuildSignals {
	s := BuildSignals{
		BuildSystem:    plan.BuildSystem,
		SecretCount:    len(plan.SecretRefs),
		HasPreCommands: len(plan.PreCommands) > 0,
		CacheHit:       plan.Cache != nil,
	}

	if plan.WorkspaceFS != nil {
		s.FileCount = len(plan.WorkspaceFS)
		s.WorkspaceBytes = WorkspaceSize(plan.WorkspaceFS)
	}

	langs := detectLanguages(plan)
	s.LanguageCount = len(langs)

	if result != nil {
		s.ExitCode = result.ExitCode
		s.DurationSec = result.Duration.Seconds()
	}

	return s
}

// ScoreBuildComplexity computes a normalised 0.0–1.0 complexity score.
func ScoreBuildComplexity(s BuildSignals) float64 {
	fileDim := sigmoid(float64(s.FileCount), 15)
	sizeDim := sigmoid(float64(s.WorkspaceBytes)/(1024*1024), 5) // normalise to MB
	secretDim := sigmoid(float64(s.SecretCount), 3)
	langDim := sigmoid(float64(s.LanguageCount), 2)
	durationDim := sigmoid(s.DurationSec, 120)

	var flagScore float64
	if s.HasPreCommands {
		flagScore += 0.08
	}
	if s.SecretCount > 0 {
		flagScore += 0.10
	}
	if s.RetryCount > 0 {
		flagScore += 0.10 * math.Min(float64(s.RetryCount), 2)
	}
	if s.ExitCode != 0 {
		flagScore += 0.15
	}
	if !s.CacheHit {
		flagScore += 0.05
	}

	bsFactor := buildSystemWeight(s.BuildSystem)

	raw := 0.20*fileDim +
		0.10*sizeDim +
		0.10*secretDim +
		0.10*langDim +
		0.15*durationDim +
		0.15*math.Min(flagScore, 0.50) +
		0.20*bsFactor

	return math.Max(0, math.Min(1, raw))
}

// ClassifyBuildComplexity maps a score to a human-readable label.
func ClassifyBuildComplexity(score float64) BuildComplexityLabel {
	switch {
	case score < complexityThresholdTrivial:
		return BuildComplexityTrivial
	case score < complexityThresholdSimple:
		return BuildComplexitySimple
	case score < complexityThresholdModerate:
		return BuildComplexityModerate
	case score < complexityThresholdHigh:
		return BuildComplexityHigh
	default:
		return BuildComplexityCritical
	}
}

// ComputeBuildComplexity runs the full complexity pipeline.
func ComputeBuildComplexity(plan *BuildPlan, result *BuildResult) *BuildComplexity {
	signals := ExtractBuildSignals(plan, result)
	score := ScoreBuildComplexity(signals)
	label := ClassifyBuildComplexity(score)

	return &BuildComplexity{
		Score:             math.Round(score*1000) / 1000,
		Label:             label,
		Signals:           signals,
		ResourceHints:     RecommendResources(signals),
		DurationBucket:    durationBucket(signals.DurationSec),
		FailureProbablity: failureProbability(signals),
	}
}

// RecommendResources suggests container resources based on build signals.
func RecommendResources(s BuildSignals) ResourceHints {
	hints := ResourceHints{
		TimeoutSec:  int(DefaultTimeout.Seconds()),
		MemoryLimit: DefaultMemoryLimit,
		CPULimit:    DefaultCPULimit,
	}

	if s.DurationSec > 300 {
		hints.TimeoutSec = 900
	} else if s.DurationSec > 120 {
		hints.TimeoutSec = 600
	}

	sizeScore := float64(s.FileCount)*0.5 + float64(s.WorkspaceBytes)/(1024*1024)
	if sizeScore > 50 {
		hints.MemoryLimit = "4g"
		hints.CPULimit = "4"
	} else if sizeScore > 20 {
		hints.MemoryLimit = "3g"
		hints.CPULimit = "3"
	}

	switch s.BuildSystem {
	case "gradle", "maven":
		if hints.MemoryLimit == DefaultMemoryLimit {
			hints.MemoryLimit = "3g"
		}
	case "cmake":
		if hints.CPULimit == DefaultCPULimit {
			hints.CPULimit = "3"
		}
	}

	if s.RetryCount > 0 && s.ExitCode != 0 {
		timeout := hints.TimeoutSec
		timeout = int(float64(timeout) * 1.5)
		if timeout > 1200 {
			timeout = 1200
		}
		hints.TimeoutSec = timeout
	}

	return hints
}

// BuildComplexitySummary returns a one-line summary suitable for log messages.
func BuildComplexitySummary(c *BuildComplexity) string {
	if c == nil {
		return "complexity=unknown"
	}
	return "complexity=" + string(c.Label) +
		" score=" + formatFloat(c.Score) +
		" duration=" + c.DurationBucket +
		" resources=" + c.ResourceHints.MemoryLimit + "/" + c.ResourceHints.CPULimit
}

// HistoricalBuildStats aggregates build metrics for a repo+build_system.
type HistoricalBuildStats struct {
	TotalBuilds    int     `json:"total_builds"`
	SuccessRate    float64 `json:"success_rate"`
	AvgDurationSec float64 `json:"avg_duration_sec"`
	P95DurationSec float64 `json:"p95_duration_sec"`
	AvgRetries     float64 `json:"avg_retries"`
}

// ComputeHistoricalStats calculates aggregate stats from a list of build records.
func ComputeHistoricalStats(records []*BuildRecord) *HistoricalBuildStats {
	if len(records) == 0 {
		return &HistoricalBuildStats{}
	}

	stats := &HistoricalBuildStats{TotalBuilds: len(records)}

	var successCount int
	var totalDuration float64
	var totalRetries int
	durations := make([]float64, 0, len(records))

	for _, rec := range records {
		if rec.Status == "success" {
			successCount++
		}
		totalRetries += rec.RetryCount

		if rec.DurationMS != nil {
			d := float64(*rec.DurationMS) / 1000.0
			totalDuration += d
			durations = append(durations, d)
		}
	}

	stats.SuccessRate = float64(successCount) / float64(len(records))
	stats.AvgRetries = float64(totalRetries) / float64(len(records))

	if len(durations) > 0 {
		stats.AvgDurationSec = totalDuration / float64(len(durations))
		sort.Float64s(durations)
		p95Idx := int(math.Ceil(float64(len(durations))*0.95)) - 1
		if p95Idx < 0 {
			p95Idx = 0
		}
		if p95Idx >= len(durations) {
			p95Idx = len(durations) - 1
		}
		stats.P95DurationSec = durations[p95Idx]
	}

	return stats
}

// RefineResourceHints adjusts resource recommendations based on historical data.
func RefineResourceHints(base ResourceHints, history *HistoricalBuildStats) ResourceHints {
	if history == nil || history.TotalBuilds == 0 {
		return base
	}

	refined := base

	if history.P95DurationSec > 0 {
		suggested := int(history.P95DurationSec * 1.3)
		if suggested > refined.TimeoutSec {
			refined.TimeoutSec = suggested
		}
		if refined.TimeoutSec > 1200 {
			refined.TimeoutSec = 1200
		}
	}

	if history.SuccessRate < 0.5 && history.TotalBuilds >= 3 {
		switch refined.MemoryLimit {
		case "2g":
			refined.MemoryLimit = "3g"
		case "3g":
			refined.MemoryLimit = "4g"
		}
	}

	return refined
}

func buildSystemWeight(bs string) float64 {
	switch strings.ToLower(bs) {
	case "gradle", "maven":
		return 0.7
	case "cmake":
		return 0.6
	case "rust", "cargo":
		return 0.5
	case "go":
		return 0.3
	case "node", "npm", "yarn":
		return 0.35
	case "python", "pip":
		return 0.25
	case "make":
		return 0.4
	default:
		return 0.5
	}
}

func detectLanguages(plan *BuildPlan) []string {
	langSet := make(map[string]bool)

	for path := range plan.WorkspaceFS {
		ext := strings.ToLower(extensionOf(path))
		switch ext {
		case ".go":
			langSet["go"] = true
		case ".py":
			langSet["python"] = true
		case ".ts", ".tsx":
			langSet["typescript"] = true
		case ".js", ".jsx":
			langSet["javascript"] = true
		case ".java":
			langSet["java"] = true
		case ".rs":
			langSet["rust"] = true
		case ".c", ".h":
			langSet["c"] = true
		case ".cpp", ".cc", ".hpp":
			langSet["cpp"] = true
		case ".rb":
			langSet["ruby"] = true
		}
	}

	if len(langSet) == 0 {
		switch strings.ToLower(plan.BuildSystem) {
		case "go":
			langSet["go"] = true
		case "gradle", "maven":
			langSet["java"] = true
		case "python", "pip":
			langSet["python"] = true
		case "node", "npm", "yarn":
			langSet["javascript"] = true
		case "cargo", "rust":
			langSet["rust"] = true
		case "cmake", "make":
			langSet["cpp"] = true
		}
	}

	langs := make([]string, 0, len(langSet))
	for l := range langSet {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

func extensionOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' {
			return ""
		}
	}
	return ""
}

func sigmoid(value, midpoint float64) float64 {
	if value <= 0 {
		return 0
	}
	return value / (value + midpoint)
}

func durationBucket(sec float64) string {
	switch {
	case sec <= 0:
		return "unknown"
	case sec < 10:
		return "instant"
	case sec < 30:
		return "fast"
	case sec < 120:
		return "normal"
	case sec < 300:
		return "slow"
	default:
		return "very_slow"
	}
}

func failureProbability(s BuildSignals) float64 {
	var p float64

	if s.SecretCount > 0 {
		p += 0.15
	}
	if s.LanguageCount > 2 {
		p += 0.10
	}
	if !s.CacheHit {
		p += 0.05
	}
	if s.HasPreCommands {
		p += 0.05
	}

	p += 0.05 * buildSystemWeight(s.BuildSystem)

	return math.Max(0, math.Min(1, p))
}

func formatFloat(f float64) string {
	s := strings.TrimRight(strings.TrimRight(
		strings.Replace(
			time.Duration(int64(f*1e9)).String(),
			"s", "", 1,
		), "0"), ".")
	_ = s
	// Simple formatting: 3 decimal places.
	i := int(f * 1000)
	whole := i / 1000
	frac := i % 1000
	if frac == 0 {
		return intToStr(whole) + ".000"
	}
	result := intToStr(whole) + "."
	if frac < 10 {
		result += "00"
	} else if frac < 100 {
		result += "0"
	}
	result += intToStr(frac)
	return result
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
