package kinocore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const pathUploadFile = "/api/v0/file/upload_file"

// UploadFile streams a local file through multipart without buffering its
// contents in memory. Android can copy a content URI to an app-private temp
// file and pass that path through the mobile bridge.
func (c *Client) UploadFile(ctx context.Context, creds Credentials, path string) (FileReference, error) {
	file, err := os.Open(path)
	if err != nil {
		return FileReference{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return FileReference{}, err
	}
	if !info.Mode().IsRegular() {
		return FileReference{}, fmt.Errorf("upload source must be a regular file")
	}
	if info.Size() > 100<<20 {
		return FileReference{}, fmt.Errorf("file exceeds the DeepSeek 100 MiB upload limit")
	}
	filename := filepath.Base(path)
	mimeType := mime.TypeByExtension(filepath.Ext(filename))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	proof, err := c.proofHeader(ctx, creds, pathUploadFile)
	if err != nil {
		return FileReference{}, err
	}
	pipeReader, pipeWriter := io.Pipe()
	form := multipart.NewWriter(pipeWriter)
	writeDone := make(chan error, 1)
	go func() {
		partHeaders := make(textproto.MIMEHeader)
		partHeaders.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filename}))
		partHeaders.Set("Content-Type", mimeType)
		part, createErr := form.CreatePart(partHeaders)
		if createErr == nil {
			_, createErr = io.Copy(part, file)
		}
		if closeErr := form.Close(); createErr == nil {
			createErr = closeErr
		}
		_ = pipeWriter.CloseWithError(createErr)
		writeDone <- createErr
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+pathUploadFile, pipeReader)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		return FileReference{}, err
	}
	h, err := c.headers(creds, "application/json, text/plain, */*")
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		return FileReference{}, err
	}
	h.Del("Content-Type")
	h.Set("Content-Type", form.FormDataContentType())
	h.Set("X-Ds-Pow-Response", proof)
	req.Header = h
	resp, err := c.http.Do(req)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		<-writeDone
		return FileReference{}, fmt.Errorf("DeepSeek upload: %w", err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return FileReference{}, readErr
	}
	if writeErr := <-writeDone; writeErr != nil {
		return FileReference{}, fmt.Errorf("write upload body: %w", writeErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FileReference{}, &APIError{HTTPStatus: resp.StatusCode, Message: fmt.Sprintf("DeepSeek upload returned HTTP %d: %s", resp.StatusCode, safeBody(raw))}
	}
	var envelope apiEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return FileReference{}, fmt.Errorf("DeepSeek upload: invalid JSON response: %w", err)
	}
	if envelope.Code != 0 {
		return FileReference{}, &APIError{Code: envelope.Code, Message: "DeepSeek upload: " + envelope.Msg}
	}
	if envelope.Data == nil || envelope.Data.BizCode != 0 {
		return FileReference{}, &APIError{Message: "DeepSeek upload: provider rejected the file"}
	}
	var result struct {
		File json.RawMessage `json:"file"`
		ID   string          `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data.BizData, &result); err != nil {
		return FileReference{}, err
	}
	fileRaw := result.File
	if len(fileRaw) == 0 {
		fileRaw = envelope.Data.BizData
	}
	var uploaded FileReference
	if err := json.Unmarshal(fileRaw, &uploaded); err != nil {
		return FileReference{}, err
	}
	if uploaded.ID == "" {
		uploaded.ID = result.ID
	}
	if uploaded.FileName == "" {
		uploaded.FileName = filename
	}
	if uploaded.FileSize == 0 {
		uploaded.FileSize = info.Size()
	}
	if uploaded.ID == "" {
		return FileReference{}, fmt.Errorf("DeepSeek upload response did not include a file id: %s", strings.TrimSpace(string(envelope.Data.BizData)))
	}
	return uploaded, nil
}

func (c *Client) FetchFiles(ctx context.Context, creds Credentials, fileIDs []string) ([]FileReference, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}
	query := mapValues("file_ids", fileIDs)
	var response struct {
		Files []FileReference `json:"files"`
	}
	_, err := c.doJSON(ctx, creds, http.MethodGet, "/api/v0/file/fetch_files", query, nil, &response)
	if err != nil {
		return nil, err
	}
	return response.Files, nil
}

func mapValues(key string, values []string) url.Values {
	query := make(url.Values, 1)
	query[key] = append([]string(nil), values...)
	return query
}
