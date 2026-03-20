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

// CheckEmbeddingModelForTest exposes the unexported check for direct testing.
var CheckEmbeddingModelForTest = checkEmbeddingModel

// CheckExtractionModelForTest exposes the unexported check for direct testing.
var CheckExtractionModelForTest = checkExtractionModel
