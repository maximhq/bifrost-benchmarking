package main

import (
	"io"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/valyala/fasthttp"
)

// The Files API is the transport for OpenAI batches: input arrives as an
// uploaded JSONL file and results are handed back as generated ones. The mocker
// implements the slice of it that a batch workflow touches end to end.

// OpenAIFileObject mirrors the OpenAI File object.
type OpenAIFileObject struct {
	ID        string `json:"id"`
	Object    string `json:"object"` // "file"
	Bytes     int    `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt *int64 `json:"expires_at,omitempty"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
	Status    string `json:"status"` // deprecated upstream but still returned
}

// OpenAIFileListResponse mirrors the response of GET /v1/files.
type OpenAIFileListResponse struct {
	Object  string             `json:"object"` // "list"
	Data    []OpenAIFileObject `json:"data"`
	FirstID *string            `json:"first_id,omitempty"`
	LastID  *string            `json:"last_id,omitempty"`
	HasMore bool               `json:"has_more"`
}

// OpenAIFileDeleteResponse mirrors the response of DELETE /v1/files/{file_id}.
type OpenAIFileDeleteResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"` // "file"
	Deleted bool   `json:"deleted"`
}

// uploadFilePurposes are the purposes POST /v1/files accepts. "batch_output" is
// deliberately absent: it is only ever set by the server on generated files.
var uploadFilePurposes = []string{"assistants", "batch", "fine-tune", "vision", "user_data", "evals"}

type mockFile struct {
	object  OpenAIFileObject
	content []byte
}

// fileStore keeps uploaded and generated files in memory for the lifetime of
// the process; the mocker never touches disk.
var fileStore = struct {
	mu    sync.RWMutex
	byID  map[string]*mockFile
	order []string // insertion order, oldest first
}{byID: make(map[string]*mockFile)}

const idAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// randomID builds a provider-shaped identifier, e.g. randomID("file-", 24).
func randomID(prefix string, n int) string {
	var sb strings.Builder
	sb.Grow(len(prefix) + n)
	sb.WriteString(prefix)
	for i := 0; i < n; i++ {
		sb.WriteByte(idAlphabet[rand.Intn(len(idAlphabet))])
	}
	return sb.String()
}

// storeFile registers content under a fresh file id and returns the file object.
func storeFile(filename string, purpose string, content []byte) OpenAIFileObject {
	obj := OpenAIFileObject{
		ID:        randomID("file-", 24),
		Object:    "file",
		Bytes:     len(content),
		CreatedAt: time.Now().Unix(),
		Filename:  filename,
		Purpose:   purpose,
		Status:    "processed",
	}

	fileStore.mu.Lock()
	defer fileStore.mu.Unlock()
	fileStore.byID[obj.ID] = &mockFile{object: obj, content: content}
	fileStore.order = append(fileStore.order, obj.ID)
	return obj
}

func lookupFile(id string) (*mockFile, bool) {
	fileStore.mu.RLock()
	defer fileStore.mu.RUnlock()
	f, ok := fileStore.byID[id]
	return f, ok
}

func removeFile(id string) bool {
	fileStore.mu.Lock()
	defer fileStore.mu.Unlock()
	if _, ok := fileStore.byID[id]; !ok {
		return false
	}
	delete(fileStore.byID, id)
	for i, existing := range fileStore.order {
		if existing == id {
			fileStore.order = append(fileStore.order[:i], fileStore.order[i+1:]...)
			break
		}
	}
	return true
}

// listFilesNewestFirst returns every stored file, optionally filtered by
// purpose, ordered the way OpenAI orders them by default (newest first).
func listFilesNewestFirst(purpose string) []OpenAIFileObject {
	fileStore.mu.RLock()
	defer fileStore.mu.RUnlock()
	files := make([]OpenAIFileObject, 0, len(fileStore.order))
	for i := len(fileStore.order) - 1; i >= 0; i-- {
		f, ok := fileStore.byID[fileStore.order[i]]
		if !ok {
			continue
		}
		if purpose != "" && f.object.Purpose != purpose {
			continue
		}
		files = append(files, f.object)
	}
	return files
}

// paginate applies OpenAI-style cursor pagination to an already ordered page of
// ids: everything strictly after the cursor, capped at limit.
func paginate(size int, limit int, cursorIndex int) (start int, end int, hasMore bool) {
	start = 0
	if cursorIndex >= 0 {
		start = cursorIndex + 1
	}
	if start > size {
		start = size
	}
	end = start + limit
	if end > size {
		end = size
	}
	return start, end, end < size
}

// queryLimit reads the "limit" query param, falling back to the endpoint default.
func queryLimit(ctx *fasthttp.RequestCtx, defaultLimit int) int {
	raw := string(ctx.QueryArgs().Peek("limit"))
	if raw == "" {
		return defaultLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return defaultLimit
	}
	return limit
}

// mockFilesHandler serves POST /v1/files (upload) and GET /v1/files (list).
func mockFilesHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) {
		return
	}
	if simulateProviderConditions(ctx, "openai") {
		return
	}

	switch {
	case ctx.IsPost():
		handleFileUpload(ctx)
	case ctx.IsGet():
		handleFileList(ctx)
	default:
		sendOpenAIAPIError(ctx, fasthttp.StatusMethodNotAllowed, "invalid_request_error",
			"Not allowed to "+string(ctx.Method())+" on /v1/files.", nil, nil)
	}
}

func handleFileUpload(ctx *fasthttp.RequestCtx) {
	form, err := ctx.MultipartForm()
	if err != nil {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Request must be multipart/form-data with a 'file' part.", StrPtr("file"), nil)
		return
	}
	defer ctx.Request.RemoveMultipartFormFiles()

	purpose := ""
	if values := form.Value["purpose"]; len(values) > 0 {
		purpose = values[0]
	}
	if purpose == "" {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Missing required parameter: 'purpose'.", StrPtr("purpose"), StrPtr("missing_required_parameter"))
		return
	}
	if !containsString(uploadFilePurposes, purpose) {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Invalid value: '"+purpose+"'. Supported values are: "+strings.Join(uploadFilePurposes, ", ")+".",
			StrPtr("purpose"), StrPtr("invalid_value"))
		return
	}

	headers := form.File["file"]
	if len(headers) == 0 {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Missing required parameter: 'file'.", StrPtr("file"), StrPtr("missing_required_parameter"))
		return
	}

	opened, err := headers[0].Open()
	if err != nil {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Failed to read the uploaded file.", StrPtr("file"), nil)
		return
	}
	defer opened.Close()

	content, err := io.ReadAll(opened)
	if err != nil {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Failed to read the uploaded file.", StrPtr("file"), nil)
		return
	}

	filename := headers[0].Filename
	if filename == "" {
		filename = "file.jsonl"
	}

	obj := storeFile(filename, purpose, content)
	log.Printf("[files] uploaded id=%s purpose=%s bytes=%d", obj.ID, obj.Purpose, obj.Bytes)
	writeJSON(ctx, fasthttp.StatusOK, obj)
}

func handleFileList(ctx *fasthttp.RequestCtx) {
	files := listFilesNewestFirst(string(ctx.QueryArgs().Peek("purpose")))

	cursorIndex := -1
	if after := string(ctx.QueryArgs().Peek("after")); after != "" {
		for i, f := range files {
			if f.ID == after {
				cursorIndex = i
				break
			}
		}
	}

	start, end, hasMore := paginate(len(files), queryLimit(ctx, 10000), cursorIndex)
	page := files[start:end]

	resp := OpenAIFileListResponse{Object: "list", Data: page, HasMore: hasMore}
	if len(page) > 0 {
		resp.FirstID = StrPtr(page[0].ID)
		resp.LastID = StrPtr(page[len(page)-1].ID)
	}

	log.Printf("[files] listing %d file(s)", len(page))
	writeJSON(ctx, fasthttp.StatusOK, resp)
}

// mockFileByIDHandler serves GET and DELETE on /v1/files/{file_id}.
func mockFileByIDHandler(ctx *fasthttp.RequestCtx, fileID string) {
	if !checkAuth(ctx) {
		return
	}
	if simulateProviderConditions(ctx, "openai") {
		return
	}

	switch {
	case ctx.IsGet():
		f, ok := lookupFile(fileID)
		if !ok {
			sendFileNotFound(ctx, fileID)
			return
		}
		writeJSON(ctx, fasthttp.StatusOK, f.object)
	case ctx.IsDelete():
		if !removeFile(fileID) {
			sendFileNotFound(ctx, fileID)
			return
		}
		log.Printf("[files] deleted id=%s", fileID)
		writeJSON(ctx, fasthttp.StatusOK, OpenAIFileDeleteResponse{ID: fileID, Object: "file", Deleted: true})
	default:
		sendOpenAIAPIError(ctx, fasthttp.StatusMethodNotAllowed, "invalid_request_error",
			"Not allowed to "+string(ctx.Method())+" on /v1/files/{file_id}.", nil, nil)
	}
}

// mockFileContentHandler serves GET /v1/files/{file_id}/content, which is how
// OpenAI batch results are downloaded.
func mockFileContentHandler(ctx *fasthttp.RequestCtx, fileID string) {
	if !checkAuth(ctx) {
		return
	}
	if simulateProviderConditions(ctx, "openai") {
		return
	}
	if !ctx.IsGet() {
		sendOpenAIAPIError(ctx, fasthttp.StatusMethodNotAllowed, "invalid_request_error",
			"Not allowed to "+string(ctx.Method())+" on /v1/files/{file_id}/content.", nil, nil)
		return
	}

	f, ok := lookupFile(fileID)
	if !ok {
		sendFileNotFound(ctx, fileID)
		return
	}

	log.Printf("[files] content id=%s bytes=%d", fileID, len(f.content))
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(f.content)
}

func sendFileNotFound(ctx *fasthttp.RequestCtx, fileID string) {
	sendOpenAIAPIError(ctx, fasthttp.StatusNotFound, "invalid_request_error",
		"No such File object: "+fileID, StrPtr("id"), nil)
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// writeJSON encodes payload as the response body, mirroring how the inference
// handlers report encoding failures.
func writeJSON(ctx *fasthttp.RequestCtx, statusCode int, payload any) {
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(statusCode)
	if err := sonic.ConfigDefault.NewEncoder(ctx).Encode(payload); err != nil {
		log.Printf("Error encoding response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode response")
	}
}
