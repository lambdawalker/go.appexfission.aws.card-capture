// ocr-bench uploads a photo pair and invokes the isolated Lambda through IAM.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/ocrbench"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func aws(ctx context.Context, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, "aws", args...)
	c.Stderr = os.Stderr
	return c.Output()
}
func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]int64(nil), values...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[int(math.Ceil(p*float64(len(v))))-1]
}
func run() (runErr error) {
	directory := flag.String("directory", "validation", "directory containing numbered validation fixtures")
	front := flag.String("front", "", "front JPEG")
	back := flag.String("back", "", "back JPEG")
	bucket := flag.String("bucket", "", "bench bucket output")
	fn := flag.String("function", "", "bench function output")
	region := flag.String("region", "", "AWS region")
	profile := flag.String("profile", "", "AWS CLI profile")
	runs := flag.Int("runs", 10, "serial repetitions (1..1000)")
	report := flag.String("report", "ocr-bench-report/index.html", "report path (.html or .json)")
	environment := flag.String("environment", "", "saved deployment environment")
	minSimilarity := flag.Float64("min-similarity", 90, "minimum OCR similarity percentage per fixture")
	flag.Parse()
	if !(*minSimilarity > 0 && *minSimilarity <= 100) {
		return fmt.Errorf("min-similarity must be greater than 0 and at most 100")
	}
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	datasetMode := *front == "" && *back == ""
	if explicit["directory"] && !datasetMode {
		return fmt.Errorf("use --directory or --front/--back")
	}
	if datasetMode && !explicit["runs"] {
		*runs = 1
	}
	fixtures := []fixture{{front: *front, back: *back}}
	if datasetMode {
		var err error
		fixtures, err = loadFixtures(*directory)
		if err != nil {
			return err
		}
	}

	if *runs < 1 || *runs > 1000 {
		return fmt.Errorf("runs must be 1..1000")
	}
	cwd, e := os.Getwd()
	if e != nil {
		return e
	}
	root := findRoot(cwd)
	var saved benchSettings
	if root != "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		saved, *environment, e = loadSaved(root, home, *environment)
		if e != nil {
			return e
		}
	} else {
		root = cwd
	}
	if *profile == "" {
		*profile = saved.Profile
	}
	if *region == "" {
		*region = saved.Region
	}
	if *bucket == "" {
		*bucket = saved.Bucket
	}
	if *fn == "" {
		*fn = saved.Function
	}
	if *profile == "" {
		*profile = os.Getenv("AWS_PROFILE")
	}
	if *region == "" {
		*region = os.Getenv("AWS_REGION")
	}
	if *region == "" {
		*region = os.Getenv("AWS_DEFAULT_REGION")
	}
	reader := bufio.NewReader(os.Stdin)
	if *region, e = askMissing(reader, "AWS region", *region); e != nil {
		return e
	}
	if *bucket, e = askMissing(reader, "OCR input bucket", *bucket); e != nil {
		return e
	}
	if *fn, e = askMissing(reader, "OCR Lambda function", *fn); e != nil {
		return e
	}
	bundle := filepath.Join(root, ".deploy", "ocr-bench", "runs", time.Now().UTC().Format("20060102T150405.000000000Z"))
	if e = bundleJSON(filepath.Join(bundle, "manifest.json"), map[string]any{"schema": 1, "environment": *environment, "started_at": time.Now().UTC(), "runs_requested": *runs}); e != nil {
		return e
	}
	fmt.Println("Diagnostic bundle:", bundle)
	localStage := "resolve_inputs"
	defer func() {
		status := "succeeded"
		if runErr != nil {
			status = "failed"
		}
		if e := bundleJSON(filepath.Join(bundle, "local", "completion.json"), map[string]any{"stage": localStage, "status": status, "finished_at": time.Now().UTC(), "note": "See attempt local/cloud files for diagnostics; setup failures occur before an attempt is created."}); e != nil {
			fmt.Fprintln(os.Stderr, "Could not save bundle completion:", e)
		}
	}()
	if !datasetMode {
		if *front == "" {
			*front = chooseImage(root, "front", saved.Front)
		}
		if *back == "" {
			*back = chooseImage(root, "back", saved.Back)
		}
		if *front, e = askMissing(reader, "Front image path (PNG/JPEG)", *front); e != nil {
			return e
		}
		if *back, e = askMissing(reader, "Back image path (PNG/JPEG)", *back); e != nil {
			return e
		}
		fixtures[0].front, fixtures[0].back = *front, *back
	}
	fmt.Printf("OCR benchmark • environment %s\nProfile: %s • region: %s\nBucket: %s\nFunction: %s\n", *environment, *profile, *region, *bucket, *fn)

	if e = bundleJSON(filepath.Join(bundle, "local", "setup.json"), map[string]any{"environment": *environment, "profile": *profile, "region": *region, "bucket": *bucket, "function": *fn, "expected_account": saved.Account, "runs": *runs}); e != nil {
		return e
	}
	localStage = "authentication"
	if e = verifyAccount(*profile, *region, saved.Account); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*runs)*120*time.Second*time.Duration(len(fixtures))+time.Minute)
	defer cancel()
	htmlReport := strings.EqualFold(filepath.Ext(*report), ".html")
	comparisons := make(map[string][]*jsonComparison)
	perFixture := make([]map[string]any, 0, len(fixtures))
	failed := 0
	progress := newBenchProgress(len(fixtures) * *runs)
	defer func() {
		status := "Finished"
		if runErr != nil && progress.passed+progress.failed < progress.total {
			status = "Stopped"
		}
		progress.finish(status)
	}()
	for _, item := range fixtures {
		itemBundle := bundle
		if datasetMode {
			progress.logf("Validating fixture %s", item.name)
			itemBundle = filepath.Join(bundle, item.name)
		}
		err := func(bundle string) error {
			localStage = "image_preparation"
			imageDir, e := os.MkdirTemp("", "ocr-bench-images-")
			if e != nil {
				return e
			}
			defer os.RemoveAll(imageDir)
			originalFront, _ := filepath.Abs(item.front)
			originalBack, _ := filepath.Abs(item.back)
			stitched := filepath.Join(imageDir, "stitched.jpg")
			prepareStart := time.Now()
			if e = prepareStitched(item.front, item.back, stitched); e != nil {
				return e
			}
			imagePrepareMS := time.Since(prepareStart).Milliseconds()
			info, e := os.Stat(stitched)
			if e != nil {
				return e
			}
			imageConfig, e := uploadedImageConfig(stitched)
			if e != nil {
				return e
			}
			progress.logf("Prepared front + back: %d x %d pixels, grayscale, %.1f KiB (one JPEG)\n", imageConfig.Width, imageConfig.Height, float64(info.Size())/1024)
			if !datasetMode && *environment != "" {
				dir := filepath.Join(root, ".deploy", "ocr-bench", *environment)
				if e = os.MkdirAll(dir, 0700); e != nil {
					return e
				}
				b, _ := json.Marshal(struct{ Front, Back string }{originalFront, originalBack})
				if e = os.WriteFile(filepath.Join(dir, "inputs.json"), b, 0600); e != nil {
					return e
				}
			}
			id := make([]byte, 16)
			if _, e := rand.Read(id); e != nil {
				return e
			}
			prefix := "inputs/" + hex.EncodeToString(id) + "/"
			localStage = "upload"
			uploadStart := time.Now()
			uploaded := []string{}
			defer func() {
				for _, key := range uploaded {
					cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
					_, e := progress.aws(cleanup, "s3api", "delete-object", "--bucket", *bucket, "--key", prefix+key)
					done()
					if e != nil {
						progress.print(os.Stderr, "Input cleanup incomplete; one-day lifecycle remains as fallback")
					}
				}
			}()
			for _, p := range []struct{ file, key string }{{stitched, "stitched.jpg"}} {
				if _, e := progress.aws(ctx, "s3api", "put-object", "--bucket", *bucket, "--key", prefix+p.key, "--body", p.file, "--content-type", "image/jpeg"); e != nil {
					return e
				}
				uploaded = append(uploaded, p.key)
			}
			uploadMS := time.Since(uploadStart).Milliseconds()
			var preview string
			if htmlReport {
				preview, e = saveReportImage(*report, item.name, stitched)
				if e != nil {
					return e
				}
			}
			temp, e := os.MkdirTemp("", "ocr-bench-")
			if e != nil {
				return e
			}
			defer os.RemoveAll(temp)
			invokeRequest := ocrbench.Request{Stitched: prefix + "stitched.jpg", Expected: item.expected}
			if datasetMode {
				invokeRequest.MinSimilarity = *minSimilarity
				invokeRequest.IncludeExtraction = htmlReport
			}
			payload, e := json.Marshal(invokeRequest)
			if e != nil {
				return e
			}
			request := filepath.Join(temp, "request.json")
			if e = os.WriteFile(request, payload, 0600); e != nil {
				return e
			}
			type sample struct {
				ocrbench.Result
				ClientMS   int64       `json:"client_ms"`
				Diagnostic *diagnostic `json:"diagnostic,omitempty"`
			}
			samples := []sample{}
			warm := []int64{}
			all := []int64{}
			success := 0
			validationFailures := 0
			localStage = "invocation"
			for i := 0; i < *runs; i++ {
				progress.show("Running")
				resultPath := filepath.Join(temp, fmt.Sprintf("response-%d.json", i))
				started := time.Now()
				meta, stderr, invokeErr := invokeAWS(ctx, "lambda", "invoke", "--function-name", *fn, "--cli-binary-format", "raw-in-base64-out", "--payload", "fileb://"+request, "--cli-read-timeout", "100", "--output", "json", "--no-cli-pager", "--log-type", "Tail", resultPath)
				r := sample{ClientMS: time.Since(started).Milliseconds()}
				body, _ := os.ReadFile(resultPath)
				r.Result, r.Diagnostic = diagnoseInvoke(meta, body, stderr, invokeErr, *fn)
				if invokeRequest.IncludeExtraction {
					comparisons[item.name] = append(comparisons[item.name], &jsonComparison{
						Expected: *item.expected,
						Actual:   r.Result.Extraction,
					})
				}
				r.Result.Extraction = nil
				clear(body)
				_ = os.Remove(resultPath)
				if datasetMode && r.Validation != nil && r.Validation.MinSimilarity != *minSimilarity {
					r.Validation = nil
				}
				attemptDir := filepath.Join(bundle, fmt.Sprintf("attempt-%03d", i+1))
				requestID, tail, httpStatus := invocationTail(meta)
				exitStatus := 0
				if invokeErr != nil {
					exitStatus = 1
					if exit, ok := invokeErr.(*exec.ExitError); ok {
						exitStatus = exit.ExitCode()
					}
				}
				if e = bundleJSON(filepath.Join(attemptDir, "local", "invoke.json"), map[string]any{"started_at": started.UTC(), "finished_at": time.Now().UTC(), "client_ms": r.ClientMS, "cli_exit_code": exitStatus, "http_status": httpStatus, "diagnostic": r.Diagnostic}); e != nil {
					return e
				}
				logCtx, logCancel := context.WithTimeout(context.Background(), 20*time.Second)
				cloud := collectCloud(logCtx, invokeAWS, "/aws/lambda/"+*fn, requestID, started, time.Now(), tail)
				logCancel()
				clear(tail)
				if e = bundleJSON(filepath.Join(attemptDir, "cloud", "logs.json"), cloud); e != nil {
					return e
				}
				if e = bundleJSON(filepath.Join(attemptDir, "cloud", "result.json"), r.Result); e != nil {
					return e
				}
				if progress.program == nil {
					fmt.Printf("Cloud logs: %s\n", cloud.Status)
				}

				clear(stderr)
				clear(meta)
				if r.Diagnostic != nil {
					message := fmt.Sprintf("Run %d: %s [%s]\nCloudWatch: %s", i+1, r.Diagnostic.Message, r.Diagnostic.Code, r.Diagnostic.LogGroup)
					if r.Diagnostic.RequestID != "" {
						message += " • request " + r.Diagnostic.RequestID
					}
					progress.print(os.Stderr, message)
				}

				passed := r.Success && (!datasetMode || (r.Validation != nil && r.Validation.Passed))
				if datasetMode && !passed {
					validationFailures++
					if r.Validation != nil {
						progress.logf("Fixture %s scored %.2f%% (minimum %.2f%%). Mismatched fields: %v\n",
							item.name, r.Validation.Similarity, *minSimilarity, r.Validation.Mismatches)
					} else if r.Success {
						progress.print(os.Stderr, "Validation result missing. Redeploy the OCR benchmark Lambda.")
					}
				}
				samples = append(samples, r)
				if r.Success {
					success++
					all = append(all, r.ClientMS)
					if !r.ColdStart {
						warm = append(warm, r.ClientMS)
					}
				}
				progress.record(passed)
				if progress.program == nil {
					fmt.Printf("%d/%d passed=%t cold=%t client=%dms error=%s\n", i+1, *runs, passed, r.ColdStart, r.ClientMS, r.Error)
				}
			}
			localStage = "report"
			summary := map[string]any{"schema": 1, "upload_ms": uploadMS, "image_prepare_ms": imagePrepareMS, "upload_bytes": info.Size(), "upload_width_px": imageConfig.Width, "upload_height_px": imageConfig.Height, "input_color_mode": "grayscale", "input_layout": "front_above_back", "runs": samples, "successes": success, "failures": *runs - success, "client_p50_ms": percentile(all, .5), "client_p90_ms": percentile(all, .9), "client_p95_ms": percentile(all, .95), "warm_p50_ms": percentile(warm, .5), "warm_p95_ms": percentile(warm, .95), "warm_successes": len(warm)}
			if datasetMode {
				summary["fixture"] = item.name
				summary["validation_failures"] = validationFailures
			}
			if preview != "" {
				summary["upload_preview"] = preview
			}
			perFixture = append(perFixture, summary)
			failed += max(*runs-success, validationFailures)
			return nil
		}(itemBundle)
		if err != nil {
			return fmt.Errorf("benchmark input %q: %w", item.name, err)
		}
	}
	progress.show("Writing report")
	summary := perFixture[0]
	if datasetMode {
		summary = map[string]any{"schema": 1, "fixtures": perFixture, "fixture_count": len(fixtures), "min_similarity_pct": *minSimilarity, "validation_failures": failed, "validation_passes": len(fixtures)**runs - failed}
	}
	b, e := json.MarshalIndent(summary, "", "  ")
	if e != nil {
		return e
	}
	if e = writeReport(*report, b, comparisons); e != nil {
		return e
	}
	if e = bundleJSON(filepath.Join(bundle, "summary.json"), summary); e != nil {
		return e
	}
	progress.logf("Report: %s", *report)
	if failed != 0 {
		return fmt.Errorf("%d benchmark runs failed; see report", failed)
	}
	return nil
}
