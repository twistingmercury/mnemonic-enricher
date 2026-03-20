package skillfiles

import (
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/twistingmercury/mnemonic/internal/handlers"
	skillfilerepo "github.com/twistingmercury/mnemonic/internal/repository/skillfile"
	skillfilesvc "github.com/twistingmercury/mnemonic/internal/service/skillfile"
)

// filenameRe matches valid filenames: start with letter/digit, then letters/digits/dot/hyphen/underscore.
var filenameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// validEncodings lists the only accepted encoding values.
var validEncodings = map[string]bool{
	"utf-8":  true,
	"base64": true,
}

// fileTypeConfig holds per-collection size and count limits.
type fileTypeConfig struct {
	maxSizeBytes int
	maxFiles     int
}

// fileTypeLimits defines the limits for each skill file collection.
var fileTypeLimits = map[string]fileTypeConfig{
	"scripts":    {maxSizeBytes: 1048576, maxFiles: 20}, // 1MB, 20 files
	"references": {maxSizeBytes: 1048576, maxFiles: 50}, // 1MB, 50 files
	"assets":     {maxSizeBytes: 5242880, maxFiles: 50}, // 5MB, 50 files
}

// ValidFileTypes are the allowed file type path segments.
var ValidFileTypes = map[string]bool{
	"scripts":    true,
	"references": true,
	"assets":     true,
}

// Handler provides HTTP handlers for skill file CRUD operations.
type Handler struct {
	svc skillfilesvc.Service
}

// New creates a new skill file Handler backed by the given service.
func New(svc skillfilesvc.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes binds skill file endpoints to the given router group.
// The group should be mounted at /v1/api.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	// Scripts
	rg.GET("/skills/:name/scripts", h.listFiles("scripts"))
	rg.POST("/skills/:name/scripts", h.createFile("scripts"))
	rg.GET("/skills/:name/scripts/:filename", h.getFile("scripts"))
	rg.PUT("/skills/:name/scripts/:filename", h.updateFile("scripts"))
	rg.DELETE("/skills/:name/scripts/:filename", h.deleteFile("scripts"))

	// References
	rg.GET("/skills/:name/references", h.listFiles("references"))
	rg.POST("/skills/:name/references", h.createFile("references"))
	rg.GET("/skills/:name/references/:filename", h.getFile("references"))
	rg.PUT("/skills/:name/references/:filename", h.updateFile("references"))
	rg.DELETE("/skills/:name/references/:filename", h.deleteFile("references"))

	// Assets
	rg.GET("/skills/:name/assets", h.listFiles("assets"))
	rg.POST("/skills/:name/assets", h.createFile("assets"))
	rg.GET("/skills/:name/assets/:filename", h.getFile("assets"))
	rg.PUT("/skills/:name/assets/:filename", h.updateFile("assets"))
	rg.DELETE("/skills/:name/assets/:filename", h.deleteFile("assets"))
}

// --- Request/Response Types ---

// fileCreateRequest is the request body for creating a skill file.
// @Description Request body for creating a skill file
type fileCreateRequest struct {
	Filename    string `json:"filename" binding:"required"`
	ContentType string `json:"content_type" binding:"required"`
	Content     string `json:"content" binding:"required"`
	Encoding    string `json:"encoding"`
}

// fileUpdateRequest is the request body for updating a skill file.
// @Description Request body for updating a skill file
type fileUpdateRequest struct {
	ContentType string `json:"content_type" binding:"required"`
	Content     string `json:"content" binding:"required"`
	Encoding    string `json:"encoding"`
}

// fileResponse is the full representation of a skill file.
// @Description Full skill file resource returned by create, get, and update operations
type fileResponse struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Content     string `json:"content,omitempty"`
	Encoding    string `json:"encoding"`
	Size        int    `json:"size"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// fileSummaryResponse is a compact skill file representation used in list results.
// @Description Summary of a skill file used in list responses (no content field)
type fileSummaryResponse struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// fileListResponse is the envelope returned by list endpoints.
// @Description List of skill file summaries
type fileListResponse struct {
	Data []fileSummaryResponse `json:"data"`
}

func toFileResponse(f *skillfilerepo.SkillFile) fileResponse {
	filename := path.Base(f.Path)
	encoding := "utf-8"

	return fileResponse{
		Filename:    filename,
		ContentType: inferContentType(filename),
		Content:     f.Content,
		Encoding:    encoding,
		Size:        len(f.Content),
		CreatedAt:   f.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   f.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func toFileSummary(f *skillfilerepo.SkillFile) fileSummaryResponse {
	filename := path.Base(f.Path)

	return fileSummaryResponse{
		Filename:    filename,
		ContentType: inferContentType(filename),
		Size:        len(f.Content),
		CreatedAt:   f.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   f.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// inferContentType returns a MIME type based on file extension.
func inferContentType(filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".py":
		return "text/x-python"
	case ".sh":
		return "text/x-shellscript"
	case ".md":
		return "text/markdown"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "text/yaml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}

// validateFileFields checks filename format, content_type length, and encoding.
// filename is only checked when checkFilename is true (i.e. on create, not update).
func validateFileFields(filename, contentType, encoding string, checkFilename bool) []handlers.FieldError {
	var errs []handlers.FieldError

	if checkFilename {
		if len(filename) > 255 {
			errs = append(errs, handlers.FieldError{
				Field:   "filename",
				Code:    "MAX_LENGTH",
				Message: "filename must be 255 characters or fewer",
			})
		} else if !filenameRe.MatchString(filename) {
			errs = append(errs, handlers.FieldError{
				Field:   "filename",
				Code:    "INVALID_FORMAT",
				Message: "filename must start with a letter or digit and contain only letters, digits, dots, hyphens, and underscores",
			})
		}
	}

	if len(contentType) > 128 {
		errs = append(errs, handlers.FieldError{
			Field:   "content_type",
			Code:    "MAX_LENGTH",
			Message: "content_type must be 128 characters or fewer",
		})
	}

	if encoding != "" && !validEncodings[encoding] {
		errs = append(errs, handlers.FieldError{
			Field:   "encoding",
			Code:    "INVALID_VALUE",
			Message: "encoding must be utf-8 or base64",
		})
	}

	return errs
}

// --- Handler factories ---

func (h *Handler) createFile(fileType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		skillName := c.Param("name")

		var req fileCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			handlers.RespondValidationError(c, "The request body contains invalid fields", nil)
			return
		}

		if fieldErrs := validateFileFields(req.Filename, req.ContentType, req.Encoding, true); len(fieldErrs) > 0 {
			handlers.RespondValidationError(c, "The request body contains invalid fields", fieldErrs)
			return
		}

		// Content size limit.
		if limits, ok := fileTypeLimits[fileType]; ok && len(req.Content) > limits.maxSizeBytes {
			c.JSON(http.StatusRequestEntityTooLarge, handlers.ProblemDetail{
				Type:     handlers.ProblemBaseURI + "payload-too-large",
				Title:    "Payload Too Large",
				Status:   http.StatusRequestEntityTooLarge,
				Detail:   fmt.Sprintf("content exceeds the maximum size for %s", fileType),
				Instance: c.Request.URL.Path,
				TraceID:  c.GetHeader("X-Request-ID"),
			})
			return
		}

		// File count limit.
		if limits, ok := fileTypeLimits[fileType]; ok {
			ft := fileType
			existing, listErr := h.svc.ListBySkill(c.Request.Context(), skillName, &ft)
			if listErr != nil {
				handlers.RespondError(c, listErr)
				return
			}
			if len(existing) >= limits.maxFiles {
				c.JSON(http.StatusUnprocessableEntity, handlers.ProblemDetail{
					Type:     handlers.ProblemBaseURI + "unprocessable-entity",
					Title:    "Unprocessable Entity",
					Status:   http.StatusUnprocessableEntity,
					Detail:   fmt.Sprintf("maximum file count (%d) exceeded for %s", limits.maxFiles, fileType),
					Instance: c.Request.URL.Path,
					TraceID:  c.GetHeader("X-Request-ID"),
				})
				return
			}
		}

		encoding := req.Encoding
		if encoding == "" {
			encoding = "utf-8"
		}

		file, err := h.svc.Create(c.Request.Context(), skillName, fileType, skillfilesvc.CreateInput{
			Filename:    req.Filename,
			ContentType: req.ContentType,
			Content:     req.Content,
			Encoding:    encoding,
		})
		if err != nil {
			handlers.RespondError(c, err)
			return
		}

		resp := toFileResponse(file)
		resp.ContentType = req.ContentType
		resp.Encoding = encoding

		c.Header("Location", fmt.Sprintf("/v1/api/skills/%s/%s/%s", skillName, fileType, req.Filename))
		c.JSON(http.StatusCreated, resp)
	}
}

func (h *Handler) listFiles(fileType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		skillName := c.Param("name")

		ft := fileType
		files, err := h.svc.ListBySkill(c.Request.Context(), skillName, &ft)
		if err != nil {
			handlers.RespondError(c, err)
			return
		}

		data := make([]fileSummaryResponse, len(files))
		for i, f := range files {
			data[i] = toFileSummary(f)
		}

		c.JSON(http.StatusOK, fileListResponse{Data: data})
	}
}

func (h *Handler) getFile(fileType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		skillName := c.Param("name")
		filename := c.Param("filename")

		file, err := h.svc.Get(c.Request.Context(), skillName, fileType, filename)
		if err != nil {
			handlers.RespondError(c, err)
			return
		}

		c.JSON(http.StatusOK, toFileResponse(file))
	}
}

func (h *Handler) updateFile(fileType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		skillName := c.Param("name")
		filename := c.Param("filename")

		var req fileUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			handlers.RespondValidationError(c, "The request body contains invalid fields", nil)
			return
		}

		if fieldErrs := validateFileFields("", req.ContentType, req.Encoding, false); len(fieldErrs) > 0 {
			handlers.RespondValidationError(c, "The request body contains invalid fields", fieldErrs)
			return
		}

		// Content size limit.
		if limits, ok := fileTypeLimits[fileType]; ok && len(req.Content) > limits.maxSizeBytes {
			c.JSON(http.StatusRequestEntityTooLarge, handlers.ProblemDetail{
				Type:     handlers.ProblemBaseURI + "payload-too-large",
				Title:    "Payload Too Large",
				Status:   http.StatusRequestEntityTooLarge,
				Detail:   fmt.Sprintf("content exceeds the maximum size for %s", fileType),
				Instance: c.Request.URL.Path,
				TraceID:  c.GetHeader("X-Request-ID"),
			})
			return
		}

		encoding := req.Encoding
		if encoding == "" {
			encoding = "utf-8"
		}

		if _, err := h.svc.Update(c.Request.Context(), skillName, fileType, filename, skillfilesvc.UpdateInput{
			ContentType: req.ContentType,
			Content:     req.Content,
			Encoding:    encoding,
		}); err != nil {
			handlers.RespondError(c, err)
			return
		}

		c.Status(http.StatusNoContent)
	}
}

func (h *Handler) deleteFile(fileType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		skillName := c.Param("name")
		filename := c.Param("filename")

		if err := h.svc.Delete(c.Request.Context(), skillName, fileType, filename); err != nil {
			handlers.RespondError(c, err)
			return
		}

		c.Status(http.StatusNoContent)
	}
}
