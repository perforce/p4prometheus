package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type record struct {
	Time      time.Time
	Level     string
	Severity  string
	Subsystem string
	ErrorID   string
	User      string
	Command   string
	IP        string
	Message   string
}

type bucketSpike struct {
	Signature string    `json:"signature"`
	Bucket    time.Time `json:"bucket"`
	Count     int       `json:"count"`
	Mean      float64   `json:"mean"`
	StdDev    float64   `json:"stddev"`
	Median    float64   `json:"median"`
}

type report struct {
	FilePath         string            `json:"file_path"`
	TotalRecords     int               `json:"total_records"`
	WindowStart      time.Time         `json:"window_start"`
	WindowEnd        time.Time         `json:"window_end"`
	ByLevel          map[string]int    `json:"by_level"`
	TopSignatures    [][2]string       `json:"top_signatures"`
	TopUsers         [][2]string       `json:"top_users"`
	TopCommands      [][2]string       `json:"top_commands"`
	SevereEvents     []record          `json:"severe_events"`
	Spikes           []bucketSpike     `json:"spikes"`
	NewSignatures    [][2]string       `json:"new_signatures"`
	ParseErrors      int               `json:"parse_errors"`
	BucketDuration   string            `json:"bucket_duration"`
	AnalyserVersion  string            `json:"analyser_version"`
	SeverityExamples map[string]string `json:"severity_examples"`
}

const analyzerVersion = "0.1.0"

func main() {
	var (
		filePath       string
		topN           int
		bucketDuration time.Duration
		minSpikeCount  int
		zThreshold     float64
		newMinCount    int
		jsonOut        bool
		severeLimit    int
	)

	flag.StringVar(&filePath, "file", "", "Path to structured p4 errors CSV file")
	flag.IntVar(&topN, "top", 10, "How many top signatures/users/commands to display")
	flag.DurationVar(&bucketDuration, "bucket", 5*time.Minute, "Bucket duration for anomaly detection")
	flag.IntVar(&minSpikeCount, "min-spike-count", 8, "Minimum bucket count for a spike")
	flag.Float64Var(&zThreshold, "z", 4.0, "Z-score threshold for spike detection")
	flag.IntVar(&newMinCount, "new-min-count", 5, "Minimum count to mark a signature as new")
	flag.BoolVar(&jsonOut, "json", false, "Output report as JSON")
	flag.IntVar(&severeLimit, "severe-limit", 50, "Maximum number of severe events to include in output")
	flag.Parse()

	if filePath == "" {
		fmt.Fprintln(os.Stderr, "missing required -file")
		os.Exit(2)
	}

	rep, err := analyzeFile(filePath, topN, bucketDuration, minSpikeCount, zThreshold, newMinCount, severeLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "analysis failed: %v\n", err)
		os.Exit(1)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return
	}

	printTextReport(rep)
}

func analyzeFile(filePath string, topN int, bucket time.Duration, minSpikeCount int, zThreshold float64, newMinCount int, severeLimit int) (*report, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.ReuseRecord = true

	byLevel := map[string]int{}
	sigTotals := map[string]int{}
	userTotals := map[string]int{}
	cmdTotals := map[string]int{}
	severityExamples := map[string]string{}
	sigBuckets := map[string]map[time.Time]int{}
	sigFirstSeen := map[string]time.Time{}
	sigLastSeen := map[string]time.Time{}
	severe := make([]record, 0)

	var total, parseErrors int
	var minTime, maxTime time.Time

	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			parseErrors++
			continue
		}
		rec, err := parseRecord(row)
		if err != nil {
			parseErrors++
			continue
		}
		total++

		if minTime.IsZero() || rec.Time.Before(minTime) {
			minTime = rec.Time
		}
		if maxTime.IsZero() || rec.Time.After(maxTime) {
			maxTime = rec.Time
		}

		byLevel[rec.Level]++
		sig := signature(rec)
		sigTotals[sig]++
		if rec.User != "" {
			userTotals[rec.User]++
		}
		if rec.Command != "" {
			cmdTotals[rec.Command]++
		}
		if _, ok := severityExamples[rec.Severity]; !ok {
			severityExamples[rec.Severity] = rec.Message
		}

		if _, ok := sigBuckets[sig]; !ok {
			sigBuckets[sig] = map[time.Time]int{}
		}
		b := rec.Time.Truncate(bucket)
		sigBuckets[sig][b]++

		if t, ok := sigFirstSeen[sig]; !ok || rec.Time.Before(t) {
			sigFirstSeen[sig] = rec.Time
		}
		if t, ok := sigLastSeen[sig]; !ok || rec.Time.After(t) {
			sigLastSeen[sig] = rec.Time
		}

		if isSevere(rec) && len(severe) < severeLimit {
			severe = append(severe, rec)
		}
	}

	if total == 0 {
		return nil, fmt.Errorf("no valid records parsed from %s", filePath)
	}

	spikes := detectSpikes(sigBuckets, minSpikeCount, zThreshold)
	newSigs := detectNewSignatures(sigTotals, sigFirstSeen, sigLastSeen, minTime, maxTime, newMinCount)

	sort.Slice(severe, func(i, j int) bool {
		return severe[i].Time.Before(severe[j].Time)
	})

	rep := &report{
		FilePath:         filePath,
		TotalRecords:     total,
		WindowStart:      minTime,
		WindowEnd:        maxTime,
		ByLevel:          byLevel,
		TopSignatures:    topPairs(sigTotals, topN),
		TopUsers:         topPairs(userTotals, topN),
		TopCommands:      topPairs(cmdTotals, topN),
		SevereEvents:     severe,
		Spikes:           spikes,
		NewSignatures:    newSigs,
		ParseErrors:      parseErrors,
		BucketDuration:   bucket.String(),
		AnalyserVersion:  analyzerVersion,
		SeverityExamples: severityExamples,
	}
	return rep, nil
}

func parseRecord(row []string) (record, error) {
	if len(row) < 20 {
		return record{}, fmt.Errorf("expected >=20 columns, got %d", len(row))
	}
	n := len(row)

	ts := strings.TrimSpace(row[3])
	tm, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		epochSec, secErr := strconv.ParseInt(strings.TrimSpace(row[1]), 10, 64)
		if secErr != nil {
			return record{}, fmt.Errorf("invalid timestamp %q", ts)
		}
		tm = time.Unix(epochSec, 0).UTC()
	}

	msg := strings.TrimSpace(row[n-1])
	if decoded, err := url.QueryUnescape(msg); err == nil {
		msg = decoded
	}

	level, severity, subsystem, errorID := parseTailFields(row)

	return record{
		Time:      tm,
		Level:     level,
		Severity:  severity,
		Subsystem: subsystem,
		ErrorID:   errorID,
		User:      strings.TrimSpace(row[8]),
		Command:   strings.TrimSpace(row[10]),
		IP:        strings.TrimSpace(row[11]),
		Message:   msg,
	}, nil
}

func parseTailFields(row []string) (level, severity, subsystem, errorID string) {
	n := len(row)
	levelIdx := -1
	for i := n - 2; i >= 0 && i >= n-8; i-- {
		v := strings.ToLower(strings.TrimSpace(row[i]))
		switch v {
		case "error", "warn", "warning", "fatal", "info", "failed":
			levelIdx = i
			level = strings.TrimSpace(row[i])
			break
		}
		if levelIdx >= 0 {
			break
		}
	}

	if levelIdx < 0 {
		// Fallback for unexpected layouts.
		level = strings.TrimSpace(row[n-4])
		severity = strings.TrimSpace(row[n-3])
		errorID = strings.TrimSpace(row[n-2])
		subsystem = severity
		return
	}

	nums := make([]string, 0, 3)
	for i := levelIdx + 1; i < n-1; i++ {
		v := strings.TrimSpace(row[i])
		if v == "" {
			continue
		}
		nums = append(nums, v)
	}

	switch len(nums) {
	case 0:
		severity, subsystem, errorID = "", "", ""
	case 1:
		severity, subsystem, errorID = nums[0], nums[0], ""
	case 2:
		severity, subsystem, errorID = nums[0], nums[0], nums[1]
	default:
		severity, subsystem, errorID = nums[0], nums[1], nums[2]
	}

	return
}

func signature(r record) string {
	return fmt.Sprintf("%s|subsys=%s|id=%s", r.Level, r.Subsystem, r.ErrorID)
}

func isSevere(r record) bool {
	if strings.EqualFold(r.Level, "fatal") {
		return true
	}
	msg := strings.ToLower(r.Message)
	keywords := []string{
		"fatal", "panic", "out of memory", "server low on resources", "command terminated",
		"partner exited unexpectedly", "database corruption", "journal", "checkpoint", "crash",
	}
	for _, k := range keywords {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}

func detectSpikes(sigBuckets map[string]map[time.Time]int, minSpikeCount int, zThreshold float64) []bucketSpike {
	spikes := make([]bucketSpike, 0)
	for sig, buckets := range sigBuckets {
		if len(buckets) < 3 {
			continue
		}
		vals := make([]int, 0, len(buckets))
		for _, c := range buckets {
			vals = append(vals, c)
		}
		mean, std := meanStd(vals)
		median := medianInt(vals)

		for bucket, c := range buckets {
			if c < minSpikeCount {
				continue
			}
			if std > 0 {
				z := (float64(c) - mean) / std
				if z >= zThreshold {
					spikes = append(spikes, bucketSpike{Signature: sig, Bucket: bucket, Count: c, Mean: mean, StdDev: std, Median: median})
				}
				continue
			}
			if float64(c) >= math.Max(3.0*median, mean+float64(minSpikeCount)) {
				spikes = append(spikes, bucketSpike{Signature: sig, Bucket: bucket, Count: c, Mean: mean, StdDev: std, Median: median})
			}
		}
	}

	sort.Slice(spikes, func(i, j int) bool {
		if spikes[i].Count == spikes[j].Count {
			return spikes[i].Bucket.Before(spikes[j].Bucket)
		}
		return spikes[i].Count > spikes[j].Count
	})
	return spikes
}

func detectNewSignatures(sigTotals map[string]int, firstSeen map[string]time.Time, lastSeen map[string]time.Time, minTime, maxTime time.Time, minCount int) [][2]string {
	result := make([][2]string, 0)
	if minTime.IsZero() || maxTime.IsZero() || !maxTime.After(minTime) {
		return result
	}

	window := maxTime.Sub(minTime)
	newThreshold := minTime.Add(time.Duration(float64(window) * 0.8))

	for sig, total := range sigTotals {
		if total < minCount {
			continue
		}
		if firstSeen[sig].After(newThreshold) || firstSeen[sig].Equal(newThreshold) {
			duration := lastSeen[sig].Sub(firstSeen[sig]).Round(time.Second)
			result = append(result, [2]string{sig, fmt.Sprintf("count=%d first=%s span=%s", total, firstSeen[sig].Format(time.RFC3339), duration)})
		}
	}
	return result
}

func topPairs(m map[string]int, n int) [][2]string {
	type kv struct {
		k string
		v int
	}
	items := make([]kv, 0, len(m))
	for k, v := range m {
		items = append(items, kv{k: k, v: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v == items[j].v {
			return items[i].k < items[j].k
		}
		return items[i].v > items[j].v
	})
	if n > len(items) {
		n = len(items)
	}
	out := make([][2]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, [2]string{items[i].k, strconv.Itoa(items[i].v)})
	}
	return out
}

func meanStd(vals []int) (float64, float64) {
	if len(vals) == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range vals {
		sum += float64(v)
	}
	mean := sum / float64(len(vals))
	if len(vals) == 1 {
		return mean, 0
	}
	var varSum float64
	for _, v := range vals {
		d := float64(v) - mean
		varSum += d * d
	}
	std := math.Sqrt(varSum / float64(len(vals)-1))
	return mean, std
}

func medianInt(vals []int) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := append([]int(nil), vals...)
	sort.Ints(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return float64(cp[mid])
	}
	return float64(cp[mid-1]+cp[mid]) / 2.0
}

func printTextReport(rep *report) {
	fmt.Printf("p4erroranalyzer %s\n", rep.AnalyserVersion)
	fmt.Printf("File: %s\n", rep.FilePath)
	fmt.Printf("Records: %d (parse errors: %d)\n", rep.TotalRecords, rep.ParseErrors)
	fmt.Printf("Window: %s -> %s\n", rep.WindowStart.Format(time.RFC3339), rep.WindowEnd.Format(time.RFC3339))
	fmt.Printf("Bucket: %s\n\n", rep.BucketDuration)

	fmt.Println("By level:")
	for _, kv := range topPairs(rep.ByLevel, len(rep.ByLevel)) {
		fmt.Printf("  %-12s %s\n", kv[0], kv[1])
	}

	printTop("Top signatures", rep.TopSignatures)
	printTop("Top users", rep.TopUsers)
	printTop("Top commands", rep.TopCommands)

	fmt.Printf("\nSevere events (first %d):\n", len(rep.SevereEvents))
	if len(rep.SevereEvents) == 0 {
		fmt.Println("  none")
	} else {
		for _, e := range rep.SevereEvents {
			fmt.Printf("  %s %s sev=%s subsys=%s id=%s user=%s cmd=%s msg=%q\n",
				e.Time.Format(time.RFC3339), e.Level, e.Severity, e.Subsystem, e.ErrorID, e.User, e.Command, e.Message)
		}
	}

	fmt.Printf("\nDetected spikes: %d\n", len(rep.Spikes))
	for i, s := range rep.Spikes {
		if i >= 20 {
			fmt.Println("  ...")
			break
		}
		fmt.Printf("  %s %s count=%d mean=%.2f std=%.2f median=%.2f\n",
			s.Bucket.Format(time.RFC3339), s.Signature, s.Count, s.Mean, s.StdDev, s.Median)
	}

	fmt.Printf("\nNew signatures: %d\n", len(rep.NewSignatures))
	for i, kv := range rep.NewSignatures {
		if i >= 20 {
			fmt.Println("  ...")
			break
		}
		fmt.Printf("  %s %s\n", kv[0], kv[1])
	}

	fmt.Println("\nSeverity examples:")
	for _, kv := range sortedStringMap(rep.SeverityExamples) {
		fmt.Printf("  sev=%s example=%q\n", kv[0], kv[1])
	}
}

func printTop(title string, vals [][2]string) {
	fmt.Printf("\n%s:\n", title)
	if len(vals) == 0 {
		fmt.Println("  none")
		return
	}
	for _, kv := range vals {
		fmt.Printf("  %-80s %s\n", kv[0], kv[1])
	}
}

func sortedStringMap(m map[string]string) [][2]string {
	out := make([][2]string, 0, len(m))
	for k, v := range m {
		out = append(out, [2]string{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
