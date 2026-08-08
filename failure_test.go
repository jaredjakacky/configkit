package configkit_test

import opskit "github.com/jaredjakacky/opskit"

func testFailure(message string) *opskit.Failure {
	return &opskit.Failure{Code: "test_failure", Message: message}
}

func failureMessage(failure *opskit.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Message
}
