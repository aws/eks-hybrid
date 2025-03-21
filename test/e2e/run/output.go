package run

import (
	"encoding/json"
	"fmt"
	"strings"
)

type E2EOutput struct {
	ArtifactsBucket     string
	ArtifactsBucketPath string
	ClusterRegion       string
}

func (e *E2EOutput) PrintJSON(e2eResult E2EResult) error {
	jsonBytes, err := json.Marshal(e2eResult)
	if err != nil {
		return fmt.Errorf("marshalling e2e result: %w", err)
	}
	fmt.Printf("%s\n", string(jsonBytes))
	return nil
}

func (e *E2EOutput) PrintText(e2eResult E2EResult) {
	for _, phase := range e2eResult.Phases {
		if phase.Status == "success" {
			continue
		}
		fmt.Printf("Phase: %s Failure: %s Status: %s\n", phase.Name, phase.FailureMessage, phase.Status)
	}
	fmt.Printf("%d/%d Tests ran\n\n", e2eResult.TestRan, e2eResult.TotalTests)

	fmt.Printf("Artifacts Bucket Path: %s\n", e2eResult.ArtifactsBucketPath)
	fmt.Printf("Ginkgo Test Log: %s\n", e2eResult.GinkgoLog)
	fmt.Printf("Setup Log: %s\n", e2eResult.SetupLog)
	fmt.Printf("Cleanup Log: %s\n\n", e2eResult.CleanupLog)

	if len(e2eResult.FailedTests) == 0 {
		fmt.Printf("All tests passed\n")
		return
	}

	fmt.Printf("Failed tests:\n")
	for _, failedTest := range e2eResult.FailedTests {
		fmt.Printf("\n\t[%s] - %s\n", failedTest.Name, failedTest.State)
		fmt.Printf("\tArtifacts Bucket Path: %s\n", failedTest.GinkgoLog[:strings.LastIndex(failedTest.GinkgoLog, "/")+1])
		fmt.Printf("\tGinkgo Test Log: %s\n", failedTest.GinkgoLog)
		fmt.Printf("\tSerial Log: %s\n", failedTest.SerialLog)
		fmt.Printf("\tLog Collector Bundle: %s\n", failedTest.CollectorLogsBundle)
		if failedTest.FailureMessage != "" {
			fmt.Printf("\t%s\n", strings.ReplaceAll(failedTest.FailureMessage, "\n", "\n\t"))
		}
		fmt.Printf("\n")
	}
}
