package kinocore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/TeamShryne/kino-core/core/pow"
)

func TestUploadFileStreamsMultipartWithUploadProof(t *testing.T) {
	const salt = "upload-test"
	const expiry = int64(1785644803019)
	const answer = uint64(3)
	digest := pow.HashV1([]byte(fmt.Sprintf("%s_%d_%d", salt, expiry, answer)))
	target := fmt.Sprintf("%x", digest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathPowChallenge:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["target_path"] != pathUploadFile {
				t.Errorf("PoW target %q", body["target_path"])
			}
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"biz_code":0,"biz_data":{"challenge":{"algorithm":"DeepSeekHashV1","challenge":%q,"salt":%q,"signature":"sig","difficulty":4,"expire_at":%d,"target_path":%q}}}}`, target, salt, expiry, pathUploadFile)
		case pathUploadFile:
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
				t.Errorf("content type %q", r.Header.Get("Content-Type"))
			}
			if r.Header.Get("X-Ds-Pow-Response") == "" {
				t.Error("missing upload proof header")
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				http.Error(w, "bad file", 400)
				return
			}
			defer file.Close()
			content, _ := io.ReadAll(file)
			if header.Filename != "sample.txt" || string(content) != "hello file" {
				t.Errorf("uploaded file %q: %q", header.Filename, content)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"biz_code":0,"biz_data":{"id":"file-1","status":"PENDING"}}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	path := t.TempDir() + "/sample.txt"
	if err := os.WriteFile(path, []byte("hello file"), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewClientWithHTTPClient(server.URL, server.Client())
	got, err := c.UploadFile(context.Background(), testCredentials(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "file-1" || got.FileName != "sample.txt" || got.FileSize != 10 {
		t.Fatalf("upload result: %+v", got)
	}
}
