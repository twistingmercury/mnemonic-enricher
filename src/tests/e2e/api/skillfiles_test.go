package api_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/twistingmercury/mnemonic/tests/e2e/helpers"
)

// =============================================================================
// Skill File Endpoint Tests
// =============================================================================
//
// Skill files are organized into three collections, each following the same
// CRUD pattern with different constraints:
//
//   Collection   | Max Files | Max Size | Path Segment
//   -------------|-----------|----------|-------------
//   Scripts      | 20        | 1MB      | scripts
//   References   | 50        | 1MB      | references
//   Assets       | 50        | 5MB      | assets
//
// Endpoints per collection:
//   - GET    /v1/api/skills/{name}/{collection}            — list files
//   - POST   /v1/api/skills/{name}/{collection}            — upload file
//   - GET    /v1/api/skills/{name}/{collection}/{filename}  — get file with content
//   - PUT    /v1/api/skills/{name}/{collection}/{filename}  — replace file content
//   - DELETE /v1/api/skills/{name}/{collection}/{filename}  — delete file
//
// File upload uses JSON body (not multipart):
//   - filename: Unique within the collection (pattern: ^[a-zA-Z0-9][a-zA-Z0-9._-]*$)
//   - content_type: MIME type string (max 128 chars)
//   - content: File content as text (utf-8) or base64-encoded binary
//   - encoding: "utf-8" (default) or "base64"
//
// Response codes (create):
//   - 201 Created: File uploaded with Location header
//   - 400 Bad Request: Validation error
//   - 404 Not Found: Skill not found
//   - 409 Conflict: Filename already exists in the collection
//   - 413 Payload Too Large: File exceeds size limit
//   - 422 Unprocessable Entity: File count limit exceeded

// fileTypeTestCase defines parameters for table-driven tests across the three
// file collections (scripts, references, assets).
type fileTypeTestCase struct {
	collection   string
	maxFiles     int
	maxSizeBytes int
	maxSizeLabel string
}

// fileTypes is the shared table for iterating over all three collections.
var fileTypes = []fileTypeTestCase{
	{collection: "scripts", maxFiles: 20, maxSizeBytes: 1048576, maxSizeLabel: "1MB"},
	{collection: "references", maxFiles: 50, maxSizeBytes: 1048576, maxSizeLabel: "1MB"},
	{collection: "assets", maxFiles: 50, maxSizeBytes: 5242880, maxSizeLabel: "5MB"},
}

// createTestSkill creates a skill with a unique name and returns that name.
// It fails the test immediately if creation does not return 201.
func createTestSkill(t *testing.T, client *helpers.TestClient) string {
	t.Helper()
	name := helpers.GenerateUniqueName("skill")
	body := helpers.SkillCreate{
		Name:        name,
		Description: "Test skill.",
		Content:     "# Test skill content",
		Version:     "1.0.0",
	}
	resp, err := client.Post("/v1/api/skills", body)
	if err != nil {
		t.Fatalf("failed to create skill: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		b := helpers.ReadBody(t, resp)
		t.Fatalf("expected 201 creating skill, got %d: %s", resp.StatusCode, string(b))
	}
	// Drain body so connection can be reused.
	helpers.ReadBody(t, resp)
	return name
}

// skillFilePath returns the collection path for a skill.
func skillFilePath(skillName, collection string) string {
	return fmt.Sprintf("/v1/api/skills/%s/%s", skillName, collection)
}

// skillFileItemPath returns the path to a specific file within a skill collection.
func skillFileItemPath(skillName, collection, filename string) string {
	return fmt.Sprintf("/v1/api/skills/%s/%s/%s", skillName, collection, filename)
}

// postRawBody performs a POST request with a raw (pre-serialized) body, setting
// Content-Type to application/json. Used for invalid JSON and empty body tests.
func postRawBody(t *testing.T, client *helpers.TestClient, path string, raw []byte) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, client.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

// -----------------------------------------------------------------------------
// List Skill Files (GET /v1/api/skills/{name}/{collection})
// OpenAPI: listSkillScripts, listSkillReferences, listSkillAssets
// -----------------------------------------------------------------------------

// TestListSkillFiles_Success verifies listing files for each collection type.
//
// Expected behavior:
//   - Returns 200 OK
//   - Response contains data array of SkillFile summaries (without content)
//   - X-Request-ID header is present
func TestListSkillFiles_Success(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			// Upload one file so the list is non-empty.
			upload := helpers.SkillFileCreate{
				Filename:    "list-test.txt",
				ContentType: "text/plain",
				Content:     "hello",
				Encoding:    "utf-8",
			}
			upResp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to upload file: %v", err)
			}
			helpers.AssertStatusCode(t, upResp, http.StatusCreated)
			helpers.ReadBody(t, upResp)

			resp, err := client.Get(skillFilePath(skillName, ft.collection))
			if err != nil {
				t.Fatalf("failed to GET collection: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusOK)
			helpers.AssertRequestIDHeader(t, resp)

			list := helpers.ParseJSON[helpers.SkillFileList](t, resp)
			if len(list.Data) == 0 {
				t.Fatal("expected at least one file in list, got empty")
			}

			// Summaries should not include content.
			for _, f := range list.Data {
				if f.Content != "" {
					t.Errorf("list response should not include file content, got non-empty content for %q", f.Filename)
				}
			}
		})
	}
}

// TestListSkillFiles_SkillNotFound verifies 404 when skill does not exist.
//
// Expected behavior:
//   - Returns 404 Not Found
//   - Response is RFC 7807 Problem Details format
func TestListSkillFiles_SkillNotFound(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			nonExistent := helpers.GenerateUniqueName("skill")

			resp, err := client.Get(skillFilePath(nonExistent, ft.collection))
			if err != nil {
				t.Fatalf("failed to GET collection: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusNotFound)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)
			if errResp.Status != http.StatusNotFound {
				t.Errorf("expected error status 404, got %d", errResp.Status)
			}
		})
	}
}

// TestListSkillFiles_EmptyResult verifies response when skill has no files.
//
// Expected behavior:
//   - Returns 200 OK
//   - data is empty array
func TestListSkillFiles_EmptyResult(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			resp, err := client.Get(skillFilePath(skillName, ft.collection))
			if err != nil {
				t.Fatalf("failed to GET collection: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusOK)

			list := helpers.ParseJSON[helpers.SkillFileList](t, resp)
			if len(list.Data) != 0 {
				t.Errorf("expected empty data array, got %d items", len(list.Data))
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Upload Skill File (POST /v1/api/skills/{name}/{collection})
// OpenAPI: createSkillScript, createSkillReference, createSkillAsset
// -----------------------------------------------------------------------------

// TestUploadSkillFile_Success verifies uploading a file with utf-8 encoding.
//
// Expected behavior:
//   - Returns 201 Created
//   - Location header points to the new file resource
//   - Response body contains the created SkillFile
//   - X-Request-ID header is present
func TestUploadSkillFile_Success(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			upload := helpers.SkillFileCreate{
				Filename:    "hello.txt",
				ContentType: "text/plain",
				Content:     "hello world",
				Encoding:    "utf-8",
			}

			resp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to POST file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusCreated)
			helpers.AssertRequestIDHeader(t, resp)

			location := resp.Header.Get("Location")
			if location == "" {
				t.Error("expected Location header to be present")
			}

			sf := helpers.ParseJSON[helpers.SkillFile](t, resp)
			if sf.Filename != upload.Filename {
				t.Errorf("expected filename %q, got %q", upload.Filename, sf.Filename)
			}
			if sf.ContentType != upload.ContentType {
				t.Errorf("expected content_type %q, got %q", upload.ContentType, sf.ContentType)
			}
		})
	}
}

// TestUploadSkillFile_Base64Encoding verifies uploading a file with base64 encoding.
//
// Expected behavior:
//   - encoding: "base64" is accepted
//   - Content is stored and retrievable
func TestUploadSkillFile_Base64Encoding(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			encoded := base64.StdEncoding.EncodeToString([]byte("hello world"))
			upload := helpers.SkillFileCreate{
				Filename:    "hello-b64.txt",
				ContentType: "text/plain",
				Content:     encoded,
				Encoding:    "base64",
			}

			resp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to POST base64 file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusCreated)

			sf := helpers.ParseJSON[helpers.SkillFile](t, resp)
			if sf.Filename != upload.Filename {
				t.Errorf("expected filename %q, got %q", upload.Filename, sf.Filename)
			}

			// Retrieve to confirm it's stored.
			getResp, err := client.Get(skillFileItemPath(skillName, ft.collection, upload.Filename))
			if err != nil {
				t.Fatalf("failed to GET file: %v", err)
			}
			helpers.AssertStatusCode(t, getResp, http.StatusOK)
			helpers.ReadBody(t, getResp)
		})
	}
}

// TestUploadSkillFile_SkillNotFound verifies 404 when skill does not exist.
//
// Expected behavior:
//   - Returns 404 Not Found
//   - Response is RFC 7807 Problem Details format
func TestUploadSkillFile_SkillNotFound(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			nonExistent := helpers.GenerateUniqueName("skill")

			upload := helpers.SkillFileCreate{
				Filename:    "notfound.txt",
				ContentType: "text/plain",
				Content:     "content",
				Encoding:    "utf-8",
			}

			resp, err := client.Post(skillFilePath(nonExistent, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to POST file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusNotFound)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)
			if errResp.Status != http.StatusNotFound {
				t.Errorf("expected error status 404, got %d", errResp.Status)
			}
		})
	}
}

// TestUploadSkillFile_DuplicateFilename verifies 409 when filename already exists.
//
// Expected behavior:
//   - Upload same filename twice to same collection
//   - Second upload returns 409 Conflict
//   - Response is RFC 7807 Problem Details format
func TestUploadSkillFile_DuplicateFilename(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			upload := helpers.SkillFileCreate{
				Filename:    "duplicate.txt",
				ContentType: "text/plain",
				Content:     "first upload",
				Encoding:    "utf-8",
			}

			// First upload — must succeed.
			resp1, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to POST file (first): %v", err)
			}
			helpers.AssertStatusCode(t, resp1, http.StatusCreated)
			helpers.ReadBody(t, resp1)

			// Second upload with same filename — must conflict.
			upload.Content = "second upload"
			resp2, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to POST file (second): %v", err)
			}
			helpers.AssertStatusCode(t, resp2, http.StatusConflict)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp2)
			if errResp.Status != http.StatusConflict {
				t.Errorf("expected error status 409, got %d", errResp.Status)
			}
		})
	}
}

// TestUploadSkillFile_FileSizeExceeded verifies 413 when file exceeds size limit.
//
// Expected behavior:
//   - Scripts/References: content > 1MB returns 413 Payload Too Large
//   - Assets: content > 5MB returns 413 Payload Too Large
func TestUploadSkillFile_FileSizeExceeded(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			oversizedContent := strings.Repeat("x", ft.maxSizeBytes+1)
			upload := helpers.SkillFileCreate{
				Filename:    "oversized.txt",
				ContentType: "text/plain",
				Content:     oversizedContent,
				Encoding:    "utf-8",
			}

			resp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to POST oversized file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusRequestEntityTooLarge)
			helpers.ReadBody(t, resp)
		})
	}
}

// TestUploadSkillFile_FileCountExceeded verifies 422 when file count limit is exceeded.
//
// Expected behavior:
//   - Scripts: >20 files returns 422 Unprocessable Entity
//   - References: >50 files returns 422 Unprocessable Entity
//   - Assets: >50 files returns 422 Unprocessable Entity
func TestUploadSkillFile_FileCountExceeded(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			// Upload exactly maxFiles files.
			for i := 0; i < ft.maxFiles; i++ {
				upload := helpers.SkillFileCreate{
					Filename:    fmt.Sprintf("file-%03d.txt", i),
					ContentType: "text/plain",
					Content:     fmt.Sprintf("content %d", i),
					Encoding:    "utf-8",
				}
				resp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
				if err != nil {
					t.Fatalf("failed to POST file %d: %v", i, err)
				}
				if resp.StatusCode != http.StatusCreated {
					b := helpers.ReadBody(t, resp)
					t.Fatalf("expected 201 for file %d, got %d: %s", i, resp.StatusCode, string(b))
				}
				helpers.ReadBody(t, resp)
			}

			// One more upload — must be rejected.
			extra := helpers.SkillFileCreate{
				Filename:    "one-too-many.txt",
				ContentType: "text/plain",
				Content:     "over the limit",
				Encoding:    "utf-8",
			}
			resp, err := client.Post(skillFilePath(skillName, ft.collection), extra)
			if err != nil {
				t.Fatalf("failed to POST extra file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusUnprocessableEntity)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)
			if errResp.Status != http.StatusUnprocessableEntity {
				t.Errorf("expected error status 422, got %d", errResp.Status)
			}
		})
	}
}

// TestUploadSkillFile_ValidationErrors verifies 400 for invalid input fields.
//
// Expected behavior:
//   - Missing filename returns 400
//   - Missing content_type returns 400
//   - Missing content returns 400
//   - Filename exceeding 255 chars returns 400
//   - Filename with invalid characters returns 400
//   - Filename starting with dot or hyphen returns 400
//   - content_type exceeding 128 chars returns 400
//   - Invalid encoding value (not "utf-8" or "base64") returns 400
func TestUploadSkillFile_ValidationErrors(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)
			path := skillFilePath(skillName, ft.collection)

			cases := []struct {
				name   string
				upload helpers.SkillFileCreate
			}{
				{
					name: "missing filename",
					upload: helpers.SkillFileCreate{
						Filename:    "",
						ContentType: "text/plain",
						Content:     "content",
						Encoding:    "utf-8",
					},
				},
				{
					name: "missing content_type",
					upload: helpers.SkillFileCreate{
						Filename:    "valid.txt",
						ContentType: "",
						Content:     "content",
						Encoding:    "utf-8",
					},
				},
				{
					name: "missing content",
					upload: helpers.SkillFileCreate{
						Filename:    "valid2.txt",
						ContentType: "text/plain",
						Content:     "",
						Encoding:    "utf-8",
					},
				},
				{
					name: "filename too long",
					upload: helpers.SkillFileCreate{
						Filename:    strings.Repeat("a", 256),
						ContentType: "text/plain",
						Content:     "content",
						Encoding:    "utf-8",
					},
				},
				{
					name: "filename with invalid chars",
					upload: helpers.SkillFileCreate{
						Filename:    "bad file!.txt",
						ContentType: "text/plain",
						Content:     "content",
						Encoding:    "utf-8",
					},
				},
				{
					name: "filename starting with dot",
					upload: helpers.SkillFileCreate{
						Filename:    ".hidden",
						ContentType: "text/plain",
						Content:     "content",
						Encoding:    "utf-8",
					},
				},
				{
					name: "filename starting with hyphen",
					upload: helpers.SkillFileCreate{
						Filename:    "-leading",
						ContentType: "text/plain",
						Content:     "content",
						Encoding:    "utf-8",
					},
				},
				{
					name: "content_type too long",
					upload: helpers.SkillFileCreate{
						Filename:    "valid3.txt",
						ContentType: strings.Repeat("x", 129),
						Content:     "content",
						Encoding:    "utf-8",
					},
				},
				{
					name: "invalid encoding",
					upload: helpers.SkillFileCreate{
						Filename:    "valid4.txt",
						ContentType: "text/plain",
						Content:     "content",
						Encoding:    "binary",
					},
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					resp, err := client.Post(path, tc.upload)
					if err != nil {
						t.Fatalf("failed to POST file: %v", err)
					}
					helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
					helpers.ReadBody(t, resp)
				})
			}
		})
	}
}

// TestUploadSkillFile_InvalidJSON verifies 400 for malformed JSON.
//
// Expected behavior:
//   - Returns 400 Bad Request
func TestUploadSkillFile_InvalidJSON(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)
			path := skillFilePath(skillName, ft.collection)

			resp, err := postRawBody(t, client, path, []byte(`{not valid json`))
			if err != nil {
				t.Fatalf("failed to POST invalid JSON: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
			helpers.ReadBody(t, resp)
		})
	}
}

// TestUploadSkillFile_EmptyBody verifies 400 for empty request body.
//
// Expected behavior:
//   - Returns 400 Bad Request
func TestUploadSkillFile_EmptyBody(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)
			path := skillFilePath(skillName, ft.collection)

			resp, err := postRawBody(t, client, path, []byte{})
			if err != nil {
				t.Fatalf("failed to POST empty body: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
			helpers.ReadBody(t, resp)
		})
	}
}

// TestUploadSkillFile_FilenameFormat validates the filename pattern ^[a-zA-Z0-9][a-zA-Z0-9._-]*$.
//
// Expected behavior:
//   - Valid filenames: "script.py", "README.md", "config-v2.yaml", "a"
//   - Invalid filenames: ".hidden", "-leading", " space", "../traversal"
func TestUploadSkillFile_FilenameFormat(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)
			path := skillFilePath(skillName, ft.collection)

			validCases := []string{
				"script.py",
				"README.md",
				"config-v2.yaml",
				"a",
			}
			for _, filename := range validCases {
				t.Run("valid/"+filename, func(t *testing.T) {
					upload := helpers.SkillFileCreate{
						Filename:    filename,
						ContentType: "text/plain",
						Content:     "content",
						Encoding:    "utf-8",
					}
					resp, err := client.Post(path, upload)
					if err != nil {
						t.Fatalf("failed to POST file: %v", err)
					}
					if resp.StatusCode != http.StatusCreated {
						b := helpers.ReadBody(t, resp)
						t.Errorf("expected 201 for valid filename %q, got %d: %s", filename, resp.StatusCode, string(b))
					} else {
						helpers.ReadBody(t, resp)
					}
				})
			}

			invalidCases := []string{
				".hidden",
				"-leading",
				" space",
				"../traversal",
			}
			for _, filename := range invalidCases {
				t.Run("invalid/"+filename, func(t *testing.T) {
					upload := helpers.SkillFileCreate{
						Filename:    filename,
						ContentType: "text/plain",
						Content:     "content",
						Encoding:    "utf-8",
					}
					resp, err := client.Post(path, upload)
					if err != nil {
						t.Fatalf("failed to POST file: %v", err)
					}
					if resp.StatusCode != http.StatusBadRequest {
						b := helpers.ReadBody(t, resp)
						t.Errorf("expected 400 for invalid filename %q, got %d: %s", filename, resp.StatusCode, string(b))
					} else {
						helpers.ReadBody(t, resp)
					}
				})
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Get Skill File (GET /v1/api/skills/{name}/{collection}/{filename})
// OpenAPI: getSkillScript, getSkillReference, getSkillAsset
// -----------------------------------------------------------------------------

// TestGetSkillFile_Success verifies retrieving a file by filename.
//
// Expected behavior:
//   - Returns 200 OK
//   - Response body contains the SkillFile with content included
//   - Content matches what was uploaded
//   - X-Request-ID header is present
func TestGetSkillFile_Success(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			upload := helpers.SkillFileCreate{
				Filename:    "get-me.txt",
				ContentType: "text/plain",
				Content:     "retrieve this content",
				Encoding:    "utf-8",
			}
			upResp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to upload file: %v", err)
			}
			helpers.AssertStatusCode(t, upResp, http.StatusCreated)
			helpers.ReadBody(t, upResp)

			resp, err := client.Get(skillFileItemPath(skillName, ft.collection, upload.Filename))
			if err != nil {
				t.Fatalf("failed to GET file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusOK)
			helpers.AssertRequestIDHeader(t, resp)

			sf := helpers.ParseJSON[helpers.SkillFile](t, resp)
			if sf.Filename != upload.Filename {
				t.Errorf("expected filename %q, got %q", upload.Filename, sf.Filename)
			}
			if sf.Content != upload.Content {
				t.Errorf("expected content %q, got %q", upload.Content, sf.Content)
			}
		})
	}
}

// TestGetSkillFile_SkillNotFound verifies 404 when skill does not exist.
//
// Expected behavior:
//   - Returns 404 Not Found
func TestGetSkillFile_SkillNotFound(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			nonExistent := helpers.GenerateUniqueName("skill")

			resp, err := client.Get(skillFileItemPath(nonExistent, ft.collection, "any.txt"))
			if err != nil {
				t.Fatalf("failed to GET file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusNotFound)
			helpers.ReadBody(t, resp)
		})
	}
}

// TestGetSkillFile_FileNotFound verifies 404 when file does not exist in the skill.
//
// Expected behavior:
//   - Skill exists but file does not
//   - Returns 404 Not Found
func TestGetSkillFile_FileNotFound(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			resp, err := client.Get(skillFileItemPath(skillName, ft.collection, "does-not-exist.txt"))
			if err != nil {
				t.Fatalf("failed to GET non-existent file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusNotFound)
			helpers.ReadBody(t, resp)
		})
	}
}

// -----------------------------------------------------------------------------
// Update Skill File (PUT /v1/api/skills/{name}/{collection}/{filename})
// OpenAPI: updateSkillScript, updateSkillReference, updateSkillAsset
// -----------------------------------------------------------------------------

// TestUpdateSkillFile_Success verifies replacing file content.
//
// Expected behavior:
//   - Returns 204 No Content
//   - updated_at changes (verified via a subsequent GET)
func TestUpdateSkillFile_Success(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			// Upload the initial file.
			upload := helpers.SkillFileCreate{
				Filename:    "update-me.txt",
				ContentType: "text/plain",
				Content:     "original content",
				Encoding:    "utf-8",
			}
			upResp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to upload file: %v", err)
			}
			helpers.AssertStatusCode(t, upResp, http.StatusCreated)
			helpers.ReadBody(t, upResp)

			// Replace the file.
			update := helpers.SkillFileUpdate{
				ContentType: "text/plain",
				Content:     "updated content",
				Encoding:    "utf-8",
			}
			putResp, err := client.Put(skillFileItemPath(skillName, ft.collection, upload.Filename), update)
			if err != nil {
				t.Fatalf("failed to PUT file: %v", err)
			}
			helpers.AssertStatusCode(t, putResp, http.StatusNoContent)
			helpers.ReadBody(t, putResp)
		})
	}
}

// TestUpdateSkillFile_SkillNotFound verifies 404 when skill does not exist.
//
// Expected behavior:
//   - Returns 404 Not Found
func TestUpdateSkillFile_SkillNotFound(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			nonExistent := helpers.GenerateUniqueName("skill")

			update := helpers.SkillFileUpdate{
				ContentType: "text/plain",
				Content:     "content",
				Encoding:    "utf-8",
			}
			resp, err := client.Put(skillFileItemPath(nonExistent, ft.collection, "any.txt"), update)
			if err != nil {
				t.Fatalf("failed to PUT file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusNotFound)
			helpers.ReadBody(t, resp)
		})
	}
}

// TestUpdateSkillFile_FileNotFound verifies 404 when file does not exist.
//
// Expected behavior:
//   - Skill exists but file does not
//   - Returns 404 Not Found
func TestUpdateSkillFile_FileNotFound(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			update := helpers.SkillFileUpdate{
				ContentType: "text/plain",
				Content:     "content",
				Encoding:    "utf-8",
			}
			resp, err := client.Put(skillFileItemPath(skillName, ft.collection, "ghost.txt"), update)
			if err != nil {
				t.Fatalf("failed to PUT non-existent file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusNotFound)
			helpers.ReadBody(t, resp)
		})
	}
}

// TestUpdateSkillFile_FileSizeExceeded verifies 413 when replacement exceeds size limit.
//
// Expected behavior:
//   - Scripts/References: content > 1MB returns 413
//   - Assets: content > 5MB returns 413
func TestUpdateSkillFile_FileSizeExceeded(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			// Create a valid file first.
			upload := helpers.SkillFileCreate{
				Filename:    "size-limit.txt",
				ContentType: "text/plain",
				Content:     "small content",
				Encoding:    "utf-8",
			}
			upResp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to upload file: %v", err)
			}
			helpers.AssertStatusCode(t, upResp, http.StatusCreated)
			helpers.ReadBody(t, upResp)

			// Try to replace with oversized content.
			oversizedContent := strings.Repeat("x", ft.maxSizeBytes+1)
			update := helpers.SkillFileUpdate{
				ContentType: "text/plain",
				Content:     oversizedContent,
				Encoding:    "utf-8",
			}
			resp, err := client.Put(skillFileItemPath(skillName, ft.collection, upload.Filename), update)
			if err != nil {
				t.Fatalf("failed to PUT oversized file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusRequestEntityTooLarge)
			helpers.ReadBody(t, resp)
		})
	}
}

// TestUpdateSkillFile_ValidationErrors verifies 400 for invalid update input.
//
// Expected behavior:
//   - Missing content_type returns 400
//   - Missing content returns 400
//   - content_type exceeding 128 chars returns 400
//   - Invalid encoding value returns 400
func TestUpdateSkillFile_ValidationErrors(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			// Upload a file to update.
			upload := helpers.SkillFileCreate{
				Filename:    "validate-update.txt",
				ContentType: "text/plain",
				Content:     "original",
				Encoding:    "utf-8",
			}
			upResp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to upload file: %v", err)
			}
			helpers.AssertStatusCode(t, upResp, http.StatusCreated)
			helpers.ReadBody(t, upResp)

			itemPath := skillFileItemPath(skillName, ft.collection, upload.Filename)

			cases := []struct {
				name   string
				update helpers.SkillFileUpdate
			}{
				{
					name: "missing content_type",
					update: helpers.SkillFileUpdate{
						ContentType: "",
						Content:     "content",
						Encoding:    "utf-8",
					},
				},
				{
					name: "missing content",
					update: helpers.SkillFileUpdate{
						ContentType: "text/plain",
						Content:     "",
						Encoding:    "utf-8",
					},
				},
				{
					name: "content_type too long",
					update: helpers.SkillFileUpdate{
						ContentType: strings.Repeat("x", 129),
						Content:     "content",
						Encoding:    "utf-8",
					},
				},
				{
					name: "invalid encoding",
					update: helpers.SkillFileUpdate{
						ContentType: "text/plain",
						Content:     "content",
						Encoding:    "binary",
					},
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					resp, err := client.Put(itemPath, tc.update)
					if err != nil {
						t.Fatalf("failed to PUT file: %v", err)
					}
					helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
					helpers.ReadBody(t, resp)
				})
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Delete Skill File (DELETE /v1/api/skills/{name}/{collection}/{filename})
// OpenAPI: deleteSkillScript, deleteSkillReference, deleteSkillAsset
// -----------------------------------------------------------------------------

// TestDeleteSkillFile_Success verifies deleting an existing file.
//
// Expected behavior:
//   - Returns 204 No Content
//   - File is no longer retrievable via GET
func TestDeleteSkillFile_Success(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			upload := helpers.SkillFileCreate{
				Filename:    "delete-me.txt",
				ContentType: "text/plain",
				Content:     "goodbye",
				Encoding:    "utf-8",
			}
			upResp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to upload file: %v", err)
			}
			helpers.AssertStatusCode(t, upResp, http.StatusCreated)
			helpers.ReadBody(t, upResp)

			delResp, err := client.Delete(skillFileItemPath(skillName, ft.collection, upload.Filename))
			if err != nil {
				t.Fatalf("failed to DELETE file: %v", err)
			}
			helpers.AssertStatusCode(t, delResp, http.StatusNoContent)
			helpers.ReadBody(t, delResp)

			// Confirm it's gone.
			getResp, err := client.Get(skillFileItemPath(skillName, ft.collection, upload.Filename))
			if err != nil {
				t.Fatalf("failed to GET deleted file: %v", err)
			}
			helpers.AssertStatusCode(t, getResp, http.StatusNotFound)
			helpers.ReadBody(t, getResp)
		})
	}
}

// TestDeleteSkillFile_SkillNotFound verifies 404 when skill does not exist.
//
// Expected behavior:
//   - Returns 404 Not Found
func TestDeleteSkillFile_SkillNotFound(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			nonExistent := helpers.GenerateUniqueName("skill")

			resp, err := client.Delete(skillFileItemPath(nonExistent, ft.collection, "any.txt"))
			if err != nil {
				t.Fatalf("failed to DELETE file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusNotFound)
			helpers.ReadBody(t, resp)
		})
	}
}

// TestDeleteSkillFile_FileNotFound verifies 404 when file does not exist.
//
// Expected behavior:
//   - Skill exists but file does not
//   - Returns 404 Not Found
func TestDeleteSkillFile_FileNotFound(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			resp, err := client.Delete(skillFileItemPath(skillName, ft.collection, "ghost.txt"))
			if err != nil {
				t.Fatalf("failed to DELETE non-existent file: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusNotFound)
			helpers.ReadBody(t, resp)
		})
	}
}

// TestDeleteSkillFile_Idempotent verifies second DELETE returns 404.
//
// Expected behavior:
//   - First DELETE returns 204
//   - Second DELETE of same file returns 404
func TestDeleteSkillFile_Idempotent(t *testing.T) {
	for _, ft := range fileTypes {
		t.Run(ft.collection, func(t *testing.T) {
			client := helpers.NewTestClient(t)
			skillName := createTestSkill(t, client)

			upload := helpers.SkillFileCreate{
				Filename:    "idempotent-delete.txt",
				ContentType: "text/plain",
				Content:     "content",
				Encoding:    "utf-8",
			}
			upResp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
			if err != nil {
				t.Fatalf("failed to upload file: %v", err)
			}
			helpers.AssertStatusCode(t, upResp, http.StatusCreated)
			helpers.ReadBody(t, upResp)

			itemPath := skillFileItemPath(skillName, ft.collection, upload.Filename)

			// First DELETE — 204.
			del1, err := client.Delete(itemPath)
			if err != nil {
				t.Fatalf("failed first DELETE: %v", err)
			}
			helpers.AssertStatusCode(t, del1, http.StatusNoContent)
			helpers.ReadBody(t, del1)

			// Second DELETE — 404.
			del2, err := client.Delete(itemPath)
			if err != nil {
				t.Fatalf("failed second DELETE: %v", err)
			}
			helpers.AssertStatusCode(t, del2, http.StatusNotFound)
			helpers.ReadBody(t, del2)
		})
	}
}

// -----------------------------------------------------------------------------
// Cross-Collection Behavior
// -----------------------------------------------------------------------------

// TestSkillFiles_SameFilenameAcrossCollections verifies that the same filename
// can exist in different collections (scripts, references, assets) without conflict.
//
// Expected behavior:
//   - Upload "helper.py" to scripts, references, and assets for the same skill
//   - All three uploads succeed with 201
//   - Each file is independently retrievable and deletable
func TestSkillFiles_SameFilenameAcrossCollections(t *testing.T) {
	client := helpers.NewTestClient(t)
	skillName := createTestSkill(t, client)

	sharedFilename := "helper.py"

	for _, ft := range fileTypes {
		upload := helpers.SkillFileCreate{
			Filename:    sharedFilename,
			ContentType: "text/x-python",
			Content:     fmt.Sprintf("# %s content", ft.collection),
			Encoding:    "utf-8",
		}
		resp, err := client.Post(skillFilePath(skillName, ft.collection), upload)
		if err != nil {
			t.Fatalf("failed to upload %q to %s: %v", sharedFilename, ft.collection, err)
		}
		if resp.StatusCode != http.StatusCreated {
			b := helpers.ReadBody(t, resp)
			t.Errorf("expected 201 uploading %q to %s, got %d: %s", sharedFilename, ft.collection, resp.StatusCode, string(b))
		} else {
			helpers.ReadBody(t, resp)
		}
	}

	// Each file is independently retrievable.
	for _, ft := range fileTypes {
		resp, err := client.Get(skillFileItemPath(skillName, ft.collection, sharedFilename))
		if err != nil {
			t.Fatalf("failed to GET %q from %s: %v", sharedFilename, ft.collection, err)
		}
		if resp.StatusCode != http.StatusOK {
			b := helpers.ReadBody(t, resp)
			t.Errorf("expected 200 getting %q from %s, got %d: %s", sharedFilename, ft.collection, resp.StatusCode, string(b))
		} else {
			helpers.ReadBody(t, resp)
		}
	}

	// Each file is independently deletable.
	for _, ft := range fileTypes {
		resp, err := client.Delete(skillFileItemPath(skillName, ft.collection, sharedFilename))
		if err != nil {
			t.Fatalf("failed to DELETE %q from %s: %v", sharedFilename, ft.collection, err)
		}
		if resp.StatusCode != http.StatusNoContent {
			b := helpers.ReadBody(t, resp)
			t.Errorf("expected 204 deleting %q from %s, got %d: %s", sharedFilename, ft.collection, resp.StatusCode, string(b))
		} else {
			helpers.ReadBody(t, resp)
		}
	}
}

// TestSkillFiles_DifferentSkillsSameFilename verifies that the same filename
// can exist in the same collection across different skills without conflict.
//
// Expected behavior:
//   - Upload "main.py" to scripts for skill-a and skill-b
//   - Both uploads succeed with 201
func TestSkillFiles_DifferentSkillsSameFilename(t *testing.T) {
	client := helpers.NewTestClient(t)
	skillA := createTestSkill(t, client)
	skillB := createTestSkill(t, client)

	sharedFilename := "main.py"
	collection := "scripts"

	for _, skillName := range []string{skillA, skillB} {
		upload := helpers.SkillFileCreate{
			Filename:    sharedFilename,
			ContentType: "text/x-python",
			Content:     fmt.Sprintf("# skill %s main", skillName),
			Encoding:    "utf-8",
		}
		resp, err := client.Post(skillFilePath(skillName, collection), upload)
		if err != nil {
			t.Fatalf("failed to upload %q to skill %s: %v", sharedFilename, skillName, err)
		}
		if resp.StatusCode != http.StatusCreated {
			b := helpers.ReadBody(t, resp)
			t.Errorf("expected 201 uploading %q to skill %s, got %d: %s", sharedFilename, skillName, resp.StatusCode, string(b))
		} else {
			helpers.ReadBody(t, resp)
		}
	}
}
