package health

// ResetForTest clears package-level state so tests start from a clean slate.
// This must only be used in tests.
func ResetForTest() {
	healthDeps = nil
	descriptors = nil
}

// CheckPostgreSQLHealthForTest exposes the unexported check for direct testing.
var CheckPostgreSQLHealthForTest = checkPostgreSQLHealth

// CheckNeo4jHealthForTest exposes the unexported check for direct testing.
var CheckNeo4jHealthForTest = checkNeo4jHealth

func init() {
	// Keep unit tests off the network: nothing listens on port 0.
	defaultOpenAIBaseURL = "http://127.0.0.1:0"
}

// OpenAIModelCheckForTest exposes the unexported check for direct testing.
var OpenAIModelCheckForTest = openAIModelCheck
