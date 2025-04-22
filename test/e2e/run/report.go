package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	ginkgoTypes "github.com/onsi/ginkgo/v2/types"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

type E2EReport struct {
	ArtifactsFolder string
}

func (e *E2EReport) Parse(ctx context.Context, reportPath string) (E2EResult, error) {
	_, err := os.Stat(reportPath)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return E2EResult{}, nil
	}
	if err != nil {
		return E2EResult{}, fmt.Errorf("report file not found: %w", err)
	}

	e2eResult, err := e.parseJSONReport(reportPath)
	if err != nil {
		return e2eResult, fmt.Errorf("parsing ginkgo json report: %w", err)
	}

	return e2eResult, nil
}

func (e *E2EReport) parseJSONReport(reportPath string) (E2EResult, error) {
	e2eResult := E2EResult{}
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		return e2eResult, fmt.Errorf("reading report: %w", err)
	}

	var reports []ginkgoTypes.Report
	err = json.Unmarshal(reportData, &reports)
	if err != nil {
		return e2eResult, fmt.Errorf("unmarshalling report: %w", err)
	}

	if len(reports) > 1 {
		return e2eResult, fmt.Errorf("multiple reports found, expected 1")
	}
	report := reports[0]

	e2eResult.TotalTests = report.PreRunStats.TotalSpecs

	foundFailedBeforeSuite := false
	reportErrors := []error{}
	for _, spec := range report.SpecReports {
		if spec.State == ginkgoTypes.SpecStateSkipped {
			continue
		}

		if spec.LeafNodeType == ginkgoTypes.NodeTypeIt {
			e2eResult.TestRan = e2eResult.TestRan + 1
		}

		// s3 artifacts path is set on the spec, try to find it to set at
		// the root e2eresult level
		artifactsPath := getReportEntry(spec, constants.TestArtifactsPath)
		if artifactsPath != "" && e2eResult.ArtifactsBucketPath == "" {
			// strip test name from path, which is the last part of the path
			// ex: ?prefix=logs/<cluster-name>/<instance-name>/
			path := strings.TrimSuffix(artifactsPath, "/")
			e2eResult.ArtifactsBucketPath = path[:strings.LastIndex(path, "/")+1]
		}

		if spec.State == ginkgoTypes.SpecStatePassed {
			continue
		}

		// when running multiple process with ginkgo, if the first beforesuite failes,
		// it will mark all the others as failed, we onlt want to show this error once
		if spec.LeafNodeType == ginkgoTypes.NodeTypeSynchronizedBeforeSuite {
			if foundFailedBeforeSuite {
				continue
			}
			foundFailedBeforeSuite = true
		}

		// This can be one of BeforeSuite, DeferCleanup and It
		// if it failed, it will be included
		// only Its will have log files created since ginkgo only captures stdout/stderr for It
		leafType := spec.LeafNodeType
		name := leafType.String()
		if leafType == ginkgoTypes.NodeTypeIt {
			name = strings.Join(spec.LeafNodeLabels, ", ")
		}

		// Get instance name from report entries
		instanceName := getReportEntry(spec, constants.TestInstanceName)
		failedTest := FailedTest{
			InstanceName:   instanceName,
			Name:           name,
			State:          spec.State.String(),
			FailureMessage: specFailureMessage(spec),
		}

		if artifactsPath != "" {
			failedTest.GinkgoLog = artifactsPath + testGinkgoOutputLog
			failedTest.SerialLog = artifactsPath + constants.SerialOutputLogFile
		}
		collectorLogsURL := getReportEntry(spec, constants.TestLogBundleFile)
		if collectorLogsURL != "" {
			failedTest.CollectorLogsBundle = collectorLogsURL
		}

		nodeadmVersion := getReportEntry(spec, constants.TestNodeadmVersion)
		if nodeadmVersion != "" {
			failedTest.NodeadmVersion = nodeadmVersion
		}

		e2eResult.FailedTests = append(e2eResult.FailedTests, failedTest)

		// Only process "It" test nodes for detailed logs
		if spec.LeafNodeType != ginkgoTypes.NodeTypeIt {
			continue
		}

		e2eResult.TestFailed = e2eResult.TestFailed + 1
		if failedTest.InstanceName == "" {
			reportErrors = append(reportErrors, fmt.Errorf("no instance name found for test"))
			continue
		}

		if saveErr := e.saveTestLogFiles(spec, failedTest.InstanceName, failedTest.Name); saveErr != nil {
			reportErrors = append(reportErrors, fmt.Errorf("saving log file: %w", saveErr))
		}
	}

	if e2eResult.ArtifactsBucketPath != "" {
		e2eResult.GinkgoLog = e2eResult.ArtifactsBucketPath + testGinkgoOutputLog
		e2eResult.SetupLog = e2eResult.ArtifactsBucketPath + testSetupLogFile
		e2eResult.CleanupLog = e2eResult.ArtifactsBucketPath + testCleanupLogFile
	}

	return e2eResult, errors.Join(reportErrors...)
}

// saveTestLogFile creates a detailed log file for a test and uploads it to S3 if configured
// Returns the S3 path where the log was uploaded or an error
func (e *E2EReport) saveTestLogFiles(spec ginkgoTypes.SpecReport, instanceName, specName string) error {
	logsDir := filepath.Join(e.ArtifactsFolder, instanceName)
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("creating test logs directory: %w", err)
	}

	logFilePath := filepath.Join(logsDir, testGinkgoOutputLog)
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("Test: [%s]\n", specName))
	sb.WriteString(fmt.Sprintf("State: %s\n", spec.State.String()))
	sb.WriteString(fmt.Sprintf("Duration: %.3f seconds\n", spec.RunTime.Seconds()))
	sb.WriteString(fmt.Sprintf("Start Time: %s\n", spec.StartTime.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("End Time: %s\n\n", spec.EndTime.Format(time.RFC3339)))

	if spec.CapturedStdOutErr != "" {
		sb.WriteString("Captured StdOut/StdErr Output >>\n")
		sb.WriteString(spec.CapturedStdOutErr)
		sb.WriteString("\n\n")
	}

	sb.WriteString(specFailureMessage(spec))

	if err := os.WriteFile(logFilePath, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("writing test log file: %w", err)
	}

	// check if test created the serial-output.log file and copy it to the logs directory
	serialOutputLogFilePath := getReportEntry(spec, constants.TestSerialOutputLogFile)
	if _, err := os.Stat(serialOutputLogFilePath); err != nil {
		// sometimes the test will not produce the serial-output.log file
		// ok to ignore
		return nil
	}

	if err := os.Rename(serialOutputLogFilePath, filepath.Join(logsDir, constants.SerialOutputLogFile)); err != nil {
		return fmt.Errorf("moving serial output log file: %w", err)
	}

	return nil
}

func specFailureMessage(spec ginkgoTypes.SpecReport) string {
	if spec.Failure.Message == "" {
		return ""
	}
	sb := strings.Builder{}
	sb.WriteString("Failure Details >>\n")
	sb.WriteString(fmt.Sprintf("[FAILED] %s\n", spec.LeafNodeText))
	sb.WriteString(fmt.Sprintf("\tExpected %s\n", strings.ReplaceAll(spec.Failure.Message, "\n", "\n\t\t")))
	if spec.Failure.Location.FileName != "" {
		timestamp := spec.Failure.TimelineLocation.Time.Format(time.DateTime)
		sb.WriteString(fmt.Sprintf("\tIn [%s] at: %s:%d @ %s\n",
			spec.LeafNodeType.String(),
			spec.Failure.Location.FileName,
			spec.Failure.Location.LineNumber,
			timestamp))
	}
	sb.WriteString("\n")
	return sb.String()
}

func getReportEntry(spec ginkgoTypes.SpecReport, name string) string {
	for _, entry := range spec.ReportEntries {
		if entry.Name == name {
			return entry.Value.String()
		}
	}
	return ""
}
