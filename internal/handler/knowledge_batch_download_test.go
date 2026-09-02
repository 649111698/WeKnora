package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type batchDownloadKnowledgeStub struct {
	interfaces.KnowledgeService
	byID     map[string]*types.Knowledge
	files    map[string]string            // id -> file content
	failOpen map[string]error             // id -> forced open error
	names    map[string]string            // id -> filename
	batch    func(ids []string) ([]*types.Knowledge, error)
}

func (s *batchDownloadKnowledgeStub) GetKnowledgeBatch(_ context.Context, _ uint64, ids []string) ([]*types.Knowledge, error) {
	if s.batch != nil {
		return s.batch(ids)
	}
	out := make([]*types.Knowledge, 0, len(ids))
	for _, id := range ids {
		if k, ok := s.byID[id]; ok {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *batchDownloadKnowledgeStub) GetKnowledgeFile(_ context.Context, id string) (io.ReadCloser, string, error) {
	if err, ok := s.failOpen[id]; ok {
		return nil, "", err
	}
	name := s.names[id]
	if name == "" {
		name = id + ".bin"
	}
	return io.NopCloser(strings.NewReader(s.files[id])), name, nil
}

type batchDownloadKBServiceStub struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s *batchDownloadKBServiceStub) GetKnowledgeBaseByID(_ context.Context, _ string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type batchDownloadShareStub struct {
	interfaces.KBShareService
	permission types.OrgMemberRole
	source     uint64
}

func (s *batchDownloadShareStub) CheckTenantKBPermission(context.Context, string, uint64, types.TenantRole) (types.OrgMemberRole, bool, error) {
	return s.permission, true, nil
}

func (s *batchDownloadShareStub) GetKBSourceTenant(context.Context, string) (uint64, error) {
	return s.source, nil
}

func newBatchDownloadRouter(kg interfaces.KnowledgeService, kb interfaces.KnowledgeBaseService, share interfaces.KBShareService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(42))
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), uint64(42))
		c.Next()
	})

	h := &KnowledgeHandler{kgService: kg, kbService: kb, kbShareService: share}
	router.POST("/knowledge/batch-download", h.BatchDownloadKnowledgeFiles)
	return router
}

func postBatchDownload(router *gin.Engine, body string) *closeNotifyRecorder {
	req := httptest.NewRequest(http.MethodPost, "/knowledge/batch-download", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder()}
	router.ServeHTTP(w, req)
	return w
}

func zipEntryNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("failed to parse zip: %v", err)
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func zipEntryContent(t *testing.T, zr *zip.Reader, name string) string {
	t.Helper()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("failed to open zip entry %s: %v", name, err)
		}
		defer rc.Close()
		buf, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("failed to read zip entry %s: %v", name, err)
		}
		return string(buf)
	}
	t.Fatalf("zip entry %s not found", name)
	return ""
}

func TestBatchDownloadStreamsZipInCallerOrder(t *testing.T) {
	kg := &batchDownloadKnowledgeStub{
		byID: map[string]*types.Knowledge{
			"k1": {ID: "k1", TenantID: 42, KnowledgeBaseID: "kb1", FileName: "report.pdf"},
			"k2": {ID: "k2", TenantID: 42, KnowledgeBaseID: "kb1", FileName: "report.pdf"},
			"k3": {ID: "k3", TenantID: 42, KnowledgeBaseID: "kb1", FileName: "../escape/notes.md"},
		},
		files: map[string]string{"k1": "one", "k2": "two", "k3": "three"},
		names: map[string]string{"k1": "report.pdf", "k2": "report.pdf", "k3": "../escape/notes.md"},
	}
	router := newBatchDownloadRouter(kg, &batchDownloadKBServiceStub{kb: &types.KnowledgeBase{ID: "kb1", TenantID: 42}}, nil)

	w := postBatchDownload(router, `{"kb_id":"kb1","ids":["k3","k1","k2"]}`)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "weknora-documents-") || !strings.HasSuffix(got, ".zip") {
		t.Fatalf("Content-Disposition = %q, want weknora-documents-*.zip attachment", got)
	}

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("failed to parse zip: %v", err)
	}
	// Path traversal is flattened and duplicate filenames get a " (2)" suffix.
	want := []string{"notes.md", "report.pdf", "report (2).pdf"}
	got := zipEntryNames(t, w.Body.Bytes())
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("entry names = %v, want %v", got, want)
	}
	if got, want := zipEntryContent(t, zr, "notes.md"), "three"; got != want {
		t.Fatalf("notes.md content = %q, want %q", got, want)
	}
	if got, want := zipEntryContent(t, zr, "report.pdf"), "one"; got != want {
		t.Fatalf("report.pdf content = %q, want %q (caller order must be preserved)", got, want)
	}
	if got, want := zipEntryContent(t, zr, "report (2).pdf"), "two"; got != want {
		t.Fatalf("report (2).pdf content = %q, want %q", got, want)
	}
}

func TestBatchDownloadSkipsFailedFilesAndReportsThem(t *testing.T) {
	kg := &batchDownloadKnowledgeStub{
		byID: map[string]*types.Knowledge{
			"k1": {ID: "k1", TenantID: 42, KnowledgeBaseID: "kb1", FileName: "ok.txt"},
			"k2": {ID: "k2", TenantID: 42, KnowledgeBaseID: "kb1", FileName: "broken.txt"},
		},
		files:    map[string]string{"k1": "fine", "k2": "unused"},
		names:    map[string]string{"k1": "ok.txt", "k2": "broken.txt"},
		failOpen: map[string]error{"k2": errors.NewInternalServerError("storage boom")},
	}
	router := newBatchDownloadRouter(kg, &batchDownloadKBServiceStub{kb: &types.KnowledgeBase{ID: "kb1", TenantID: 42}}, nil)

	w := postBatchDownload(router, `{"kb_id":"kb1","ids":["k1","k2"]}`)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("failed to parse zip: %v", err)
	}
	if got, want := zipEntryContent(t, zr, "ok.txt"), "fine"; got != want {
		t.Fatalf("ok.txt content = %q, want %q", got, want)
	}
	report := zipEntryContent(t, zr, "_download-errors.txt")
	if !strings.Contains(report, "k2") || !strings.Contains(report, "broken.txt") {
		t.Fatalf("error report = %q, want it to mention k2 / broken.txt", report)
	}
}

func TestBatchDownloadRejectsSharedViewer(t *testing.T) {
	kg := &batchDownloadKnowledgeStub{
		byID: map[string]*types.Knowledge{
			"k1": {ID: "k1", TenantID: 7, KnowledgeBaseID: "kb1", FileName: "secret.pdf"},
		},
		files: map[string]string{"k1": "secret"},
		names: map[string]string{"k1": "secret.pdf"},
	}
	// KB belongs to tenant 7; caller tenant 42 only has a Viewer share.
	router := newBatchDownloadRouter(
		kg,
		&batchDownloadKBServiceStub{kb: &types.KnowledgeBase{ID: "kb1", TenantID: 7}},
		&batchDownloadShareStub{permission: types.OrgRoleViewer, source: 7},
	)

	w := postBatchDownload(router, `{"kb_id":"kb1","ids":["k1"]}`)
	if got, want := w.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
}

func TestBatchDownloadRejectsKnowledgeOutsideKB(t *testing.T) {
	kg := &batchDownloadKnowledgeStub{
		byID: map[string]*types.Knowledge{
			"k1": {ID: "k1", TenantID: 42, KnowledgeBaseID: "kb1"},
			"k2": {ID: "k2", TenantID: 42, KnowledgeBaseID: "kb-other"},
		},
	}
	router := newBatchDownloadRouter(kg, &batchDownloadKBServiceStub{kb: &types.KnowledgeBase{ID: "kb1", TenantID: 42}}, nil)

	w := postBatchDownload(router, `{"kb_id":"kb1","ids":["k1","k2"]}`)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
}

func TestBatchDownloadRejectsMissingEntries(t *testing.T) {
	kg := &batchDownloadKnowledgeStub{
		byID: map[string]*types.Knowledge{
			"k1": {ID: "k1", TenantID: 42, KnowledgeBaseID: "kb1"},
		},
	}
	router := newBatchDownloadRouter(kg, &batchDownloadKBServiceStub{kb: &types.KnowledgeBase{ID: "kb1", TenantID: 42}}, nil)

	w := postBatchDownload(router, `{"kb_id":"kb1","ids":["k1","k-missing"]}`)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
}

func TestUniqueZipEntryName(t *testing.T) {
	used := map[string]int{}
	cases := []struct {
		in       string
		fallback string
		want     string
	}{
		{"report.pdf", "kid", "report.pdf"},
		{"report.pdf", "kid", "report (2).pdf"},
		{"report.pdf", "kid", "report (3).pdf"},
		{"..\\windows\\evil.txt", "kid", "evil.txt"},
		{"", "kid", "kid"},
		// A second empty filename falls back to the knowledge id as well and
		// must still get a dedup suffix rather than shadow the first entry.
		{"/", "kid", "kid (2)"},
		{"no-extension", "kid", "no-extension"},
	}
	for i, tc := range cases {
		if got := uniqueZipEntryName(tc.in, tc.fallback, used); got != tc.want {
			t.Fatalf("case %d: uniqueZipEntryName(%q) = %q, want %q", i, tc.in, got, tc.want)
		}
	}
}
