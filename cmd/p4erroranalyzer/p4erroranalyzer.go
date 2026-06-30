package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
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
	ErrorName string
	User      string
	Command   string
	Program   string
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
	TopPrograms      [][2]string       `json:"top_programs"`
	SevereEvents     []record          `json:"severe_events"`
	Spikes           []bucketSpike     `json:"spikes"`
	NewSignatures    [][2]string       `json:"new_signatures"`
	ParseErrors      int               `json:"parse_errors"`
	BucketDuration   string            `json:"bucket_duration"`
	AnalyserVersion  string            `json:"analyser_version"`
	SeverityExamples map[string]string `json:"severity_examples"`
}

type triageConfig struct {
	Model         string
	URL           string
	Timeout       time.Duration
	Temperature   float64
	TopSignatures int
	TopSpikes     int
	SevereSamples int
	MaxFindings   int
}

type ollamaGenerateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Format  string                 `json:"format,omitempty"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Error    string `json:"error"`
}

const analyzerVersion = "0.1.0"

//go:generate python3 generate_errschema_go.py --in logschema.txt --out error_schema_generated.go
//go:generate python3 generate_error_lookup_go.py --errors all_errors.txt --errornum errornum.h --out error_lookup_generated.go

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

		triageModel         string
		triageURL           string
		triageOut           string
		triageTimeout       time.Duration
		triageTemperature   float64
		triageTopSignatures int
		triageTopSpikes     int
		triageSevereSamples int
		triageMaxFindings   int
	)

	flag.StringVar(&filePath, "file", "", "Path to structured p4 errors CSV file")
	flag.IntVar(&topN, "top", 10, "How many top signatures/users/commands/programs to display")
	flag.DurationVar(&bucketDuration, "bucket", 5*time.Minute, "Bucket duration for anomaly detection")
	flag.IntVar(&minSpikeCount, "min-spike-count", 8, "Minimum bucket count for a spike")
	flag.Float64Var(&zThreshold, "z", 4.0, "Z-score threshold for spike detection")
	flag.IntVar(&newMinCount, "new-min-count", 5, "Minimum count to mark a signature as new")
	flag.BoolVar(&jsonOut, "json", false, "Output report as JSON")
	flag.IntVar(&severeLimit, "severe-limit", 50, "Maximum number of severe events to include in output")

	flag.StringVar(&triageModel, "triage-ollama-model", "", "Optional local Ollama model to run triage, for example deepseek-coder:6.7b")
	flag.StringVar(&triageURL, "triage-ollama-url", "http://localhost:11434/api/generate", "Ollama generate endpoint")
	flag.StringVar(&triageOut, "triage-out", "", "Optional file path to write triage JSON envelope")
	flag.DurationVar(&triageTimeout, "triage-timeout", 90*time.Second, "Timeout for Ollama triage call")
	flag.Float64Var(&triageTemperature, "triage-temperature", 0.1, "Temperature for triage model")
	flag.IntVar(&triageTopSignatures, "triage-top-signatures", 12, "How many top signatures to include in triage input")
	flag.IntVar(&triageTopSpikes, "triage-top-spikes", 20, "How many spikes to include in triage input")
	flag.IntVar(&triageSevereSamples, "triage-severe-samples", 20, "How many severe events to include in triage input")
	flag.IntVar(&triageMaxFindings, "triage-max-findings", 5, "Maximum findings requested from triage model")
	flag.Parse()

	if filePath == "" {
		fmt.Fprintln(os.Stderr, "missing required -file")
		os.Exit(2)
	}

	if triageOut != "" && strings.TrimSpace(triageModel) == "" {
		fmt.Fprintln(os.Stderr, "-triage-out requires -triage-ollama-model")
		os.Exit(2)
	}

	rep, err := analyzeFile(filePath, topN, bucketDuration, minSpikeCount, zThreshold, newMinCount, severeLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "analysis failed: %v\n", err)
		os.Exit(1)
	}

	var triageResult interface{}
	var triageCompact map[string]interface{}
	if strings.TrimSpace(triageModel) != "" {
		cfg := triageConfig{
			Model:         triageModel,
			URL:           triageURL,
			Timeout:       triageTimeout,
			Temperature:   triageTemperature,
			TopSignatures: triageTopSignatures,
			TopSpikes:     triageTopSpikes,
			SevereSamples: triageSevereSamples,
			MaxFindings:   triageMaxFindings,
		}

		triageCompact = buildTriageCompact(rep, cfg)
		triageResult, err = runLocalTriage(cfg, triageCompact)
		if err != nil {
			fmt.Fprintf(os.Stderr, "triage failed: %v\n", err)
			os.Exit(1)
		}

		if triageOut != "" {
			err = writeTriageEnvelope(triageOut, filePath, cfg, triageCompact, triageResult)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed writing triage output: %v\n", err)
				os.Exit(1)
			}
		}
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if triageResult == nil {
			_ = enc.Encode(rep)
		} else {
			_ = enc.Encode(map[string]interface{}{
				"report":               rep,
				"triage_model":         triageModel,
				"triage_generated_utc": time.Now().UTC().Format(time.RFC3339),
				"compact_input":        triageCompact,
				"triage":               triageResult,
			})
		}
		return
	}

	printTextReport(rep)

	if triageResult != nil {
		fmt.Println("\nLocal LLM triage:")
		printJSONBlock(triageResult)
	}
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
	progTotals := map[string]int{}
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
		if rec.Program != "" {
			progTotals[rec.Program]++
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
		TopPrograms:      topPairs(progTotals, topN),
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
	if len(row) < 12 {
		return record{}, fmt.Errorf("expected >=12 columns, got %d", len(row))
	}

	schema, ok := findSchemaForRowLen(len(row))

	dateVal := fieldFromSchema(row, schema, "f_date")
	epochVal := fieldFromSchema(row, schema, "f_timestamp")
	if dateVal == "" && len(row) > 3 {
		dateVal = strings.TrimSpace(row[3])
	}
	if epochVal == "" && len(row) > 1 {
		epochVal = strings.TrimSpace(row[1])
	}

	tm, err := parseRecordTime(dateVal, epochVal)
	if err != nil {
		return record{}, err
	}

	msg := fieldFromSchema(row, schema, "f_text")
	if msg == "" {
		msg = strings.TrimSpace(row[len(row)-1])
	}
	if decoded, err := url.QueryUnescape(msg); err == nil {
		msg = decoded
	}

	level := normalizeEventType(fieldFromSchema(row, schema, "f_eventtype"))
	severity := fieldFromSchema(row, schema, "f_severity")
	subsystem := fieldFromSchema(row, schema, "f_subsys")
	errorID := fieldFromSchema(row, schema, "f_subcode")
	errorName := ""

	if !ok || level == "" || severity == "" || subsystem == "" {
		fLevel, fSeverity, fSubsystem, fErrorID := parseTailFields(row)
		if level == "" {
			level = fLevel
		}
		if severity == "" {
			severity = fSeverity
		}
		if subsystem == "" {
			subsystem = fSubsystem
		}
		if errorID == "" {
			errorID = fErrorID
		}
	}
	errorName = lookupErrorShortName(subsystem, errorID)

	user := fieldFromSchema(row, schema, "f_user")
	command := fieldFromSchema(row, schema, "f_func")
	program := fieldFromSchema(row, schema, "f_prog")
	ip := fieldFromSchema(row, schema, "f_host")

	if user == "" && len(row) > 8 {
		user = strings.TrimSpace(row[8])
	}
	if command == "" && len(row) > 10 {
		command = strings.TrimSpace(row[10])
	}
	if program == "" && len(row) > 12 {
		program = strings.TrimSpace(row[12])
	}
	if program == "" {
		program = "unknown"
	}
	if ip == "" && len(row) > 11 {
		ip = strings.TrimSpace(row[11])
	}

	return record{
		Time:      tm,
		Level:     level,
		Severity:  severity,
		Subsystem: subsystem,
		ErrorID:   errorID,
		ErrorName: errorName,
		User:      user,
		Command:   command,
		Program:   program,
		IP:        ip,
		Message:   msg,
	}, nil
}

func findSchemaForRowLen(rowLen int) (errorCSVSchema, bool) {
	if s, ok := generatedErrorCSVByFieldCount[rowLen]; ok {
		return s, true
	}

	bestCount := -1
	var best errorCSVSchema
	for count, schema := range generatedErrorCSVByFieldCount {
		if count <= rowLen && count > bestCount {
			bestCount = count
			best = schema
		}
	}
	if bestCount >= 0 {
		return best, true
	}
	return errorCSVSchema{}, false
}

func fieldFromSchema(row []string, schema errorCSVSchema, fieldName string) string {
	idx, ok := schema.FieldIndex[fieldName]
	if !ok || idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

func parseRecordTime(dateVal string, epochVal string) (time.Time, error) {
	if dateVal != "" {
		if tm, err := time.Parse(time.RFC3339Nano, dateVal); err == nil {
			return tm, nil
		}
	}
	if epochVal != "" {
		epochSec, err := strconv.ParseInt(epochVal, 10, 64)
		if err == nil {
			return time.Unix(epochSec, 0).UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp date=%q epoch=%q", dateVal, epochVal)
}

func normalizeEventType(eventType string) string {
	v := strings.TrimSpace(strings.ToLower(eventType))
	switch v {
	case "":
		return ""
	case "4", "error":
		return "error"
	case "5", "fatal", "fatalerror", "fatal_error":
		return "fatal"
	case "3", "anyerror", "any_error":
		return "error"
	default:
		return strings.TrimSpace(eventType)
	}
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
	idPart := r.ErrorID
	if r.ErrorName != "" {
		idPart = fmt.Sprintf("%s(%s)", r.ErrorID, r.ErrorName)
	}
	if idPart == "" {
		idPart = r.ErrorName
	}
	return fmt.Sprintf("%s|subsys=%s|id=%s", r.Level, r.Subsystem, idPart)
}

func lookupErrorShortName(subsysStr, subcodeStr string) string {
	subsys, err := strconv.Atoi(strings.TrimSpace(subsysStr))
	if err != nil {
		return ""
	}
	subcode, err := strconv.Atoi(strings.TrimSpace(subcodeStr))
	if err != nil {
		return ""
	}
	return generatedErrorShortBySubsysSubcode[[2]int{subsys, subcode}]
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
	printTop("Top programs", rep.TopPrograms)

	fmt.Printf("\nSevere events (first %d):\n", len(rep.SevereEvents))
	if len(rep.SevereEvents) == 0 {
		fmt.Println("  none")
	} else {
		for _, e := range rep.SevereEvents {
			fmt.Printf("  %s %s sev=%s subsys=%s id=%s user=%s cmd=%s prog=%s msg=%q\n",
				e.Time.Format(time.RFC3339), e.Level, e.Severity, e.Subsystem, e.ErrorID, e.User, e.Command, e.Program, e.Message)
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

func buildTriageCompact(rep *report, cfg triageConfig) map[string]interface{} {
	severeSample := make([]map[string]interface{}, 0)
	for i, ev := range rep.SevereEvents {
		if i >= cfg.SevereSamples {
			break
		}
		severeSample = append(severeSample, map[string]interface{}{
			"time":      ev.Time.Format(time.RFC3339),
			"level":     ev.Level,
			"severity":  ev.Severity,
			"subsystem": ev.Subsystem,
			"error_id":  ev.ErrorID,
			"user":      ev.User,
			"command":   ev.Command,
			"program":   ev.Program,
			"message":   ev.Message,
		})
	}

	spikes := rep.Spikes
	if len(spikes) > cfg.TopSpikes {
		spikes = spikes[:cfg.TopSpikes]
	}

	newSigs := rep.NewSignatures
	if len(newSigs) > 20 {
		newSigs = newSigs[:20]
	}

	return map[string]interface{}{
		"file_path":            rep.FilePath,
		"window_start":         rep.WindowStart.Format(time.RFC3339),
		"window_end":           rep.WindowEnd.Format(time.RFC3339),
		"total_records":        rep.TotalRecords,
		"parse_errors":         rep.ParseErrors,
		"bucket_duration":      rep.BucketDuration,
		"by_level":             rep.ByLevel,
		"top_signatures":       topPairObjects(rep.TopSignatures, cfg.TopSignatures),
		"top_users":            topPairObjects(rep.TopUsers, 10),
		"top_commands":         topPairObjects(rep.TopCommands, 10),
		"top_programs":         topPairObjects(rep.TopPrograms, 10),
		"new_signatures":       newSigs,
		"spikes":               spikes,
		"severity_examples":    rep.SeverityExamples,
		"severe_events_sample": severeSample,
	}
}

func topPairObjects(in [][2]string, limit int) []map[string]interface{} {
	if limit > len(in) {
		limit = len(in)
	}
	out := make([]map[string]interface{}, 0, limit)
	for i := 0; i < limit; i++ {
		count, err := strconv.Atoi(in[i][1])
		if err != nil {
			out = append(out, map[string]interface{}{"key": in[i][0], "count": in[i][1]})
			continue
		}
		out = append(out, map[string]interface{}{"key": in[i][0], "count": count})
	}
	return out
}

func buildTriagePrompt(compact map[string]interface{}, maxFindings int) (string, error) {
	instructions := map[string]interface{}{
		"role": "You are an SRE anomaly triage assistant for Perforce structured errors.",
		"constraints": []string{
			"Use only provided evidence.",
			"Do not invent missing telemetry.",
			"Return valid JSON only.",
			fmt.Sprintf("Return at most %d findings.", maxFindings),
		},
		"risk_ranking": []string{
			"Prioritize sustained spikes, fatal or critical error patterns, and broad blast radius.",
			"Downgrade low-volume or weakly evidenced findings.",
		},
		"output_schema": map[string]interface{}{
			"ranked_findings": []map[string]interface{}{
				{
					"anomaly_id":          "string",
					"probable_cause":      "string",
					"confidence_0_to_1":   0.0,
					"evidence_points":     []string{"string"},
					"impact_scope":        "string",
					"immediate_actions":   []string{"string"},
					"false_positive_risk": "low|medium|high",
				},
			},
		},
	}

	instJSON, err := json.MarshalIndent(instructions, "", "  ")
	if err != nil {
		return "", err
	}

	compactJSON, err := json.MarshalIndent(compact, "", "  ")
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("SYSTEM_AND_TASK:\n")
	b.Write(instJSON)
	b.WriteString("\n\nINPUT_ANOMALY_PACKET:\n")
	b.Write(compactJSON)
	b.WriteString("\n\nRespond with JSON only.")
	return b.String(), nil
}

func runLocalTriage(cfg triageConfig, compact map[string]interface{}) (interface{}, error) {
	prompt, err := buildTriagePrompt(compact, cfg.MaxFindings)
	if err != nil {
		return nil, err
	}

	reqBody := ollamaGenerateRequest{
		Model:  cfg.Model,
		Prompt: prompt,
		Format: "json",
		Stream: false,
		Options: map[string]interface{}{
			"temperature": cfg.Temperature,
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: cfg.Timeout}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("ollama HTTP %d: %s", httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	var modelResp ollamaGenerateResponse
	if err := json.Unmarshal(body, &modelResp); err != nil {
		return nil, fmt.Errorf("invalid ollama response: %w", err)
	}
	if strings.TrimSpace(modelResp.Error) != "" {
		return nil, fmt.Errorf("ollama error: %s", modelResp.Error)
	}
	if strings.TrimSpace(modelResp.Response) == "" {
		return nil, fmt.Errorf("ollama returned empty response")
	}

	var triage interface{}
	if err := json.Unmarshal([]byte(modelResp.Response), &triage); err != nil {
		return nil, fmt.Errorf("model output was not valid JSON: %w", err)
	}

	return triage, nil
}

func writeTriageEnvelope(path, reportPath string, cfg triageConfig, compact map[string]interface{}, triage interface{}) error {
	env := map[string]interface{}{
		"source_report":    reportPath,
		"model":            cfg.Model,
		"ollama_url":       cfg.URL,
		"generated_at_utc": time.Now().UTC().Format(time.RFC3339),
		"compact_input":    compact,
		"triage":           triage,
	}

	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func printJSONBlock(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
