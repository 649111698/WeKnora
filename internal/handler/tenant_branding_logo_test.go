package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// brandingLogoTenantService stubs the slice of TenantService the branding
// logo handlers touch. Embedding the interface keeps the stub tolerant to
// unrelated interface growth.
type brandingLogoTenantService struct {
	interfaces.TenantService

	savedType    string
	savedLen     int
	saveCalled   int
	loadedTenant *types.Tenant
	updated      *types.Tenant
	asset        *types.TenantBrandingAsset
}

func (s *brandingLogoTenantService) SaveBrandingLogo(_ context.Context, _ uint64, contentType string, data []byte) error {
	s.saveCalled++
	s.savedType = contentType
	s.savedLen = len(data)
	return nil
}

func (s *brandingLogoTenantService) GetBrandingLogo(_ context.Context, _ uint64) (*types.TenantBrandingAsset, error) {
	return s.asset, nil
}

func (s *brandingLogoTenantService) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	if s.loadedTenant == nil {
		tenant := &types.Tenant{ID: id}
		tenant.BrandingConfig = &types.BrandingConfig{LoginTitle: "kept"}
		return tenant, nil
	}
	return s.loadedTenant, nil
}

func (s *brandingLogoTenantService) UpdateTenant(_ context.Context, tenant *types.Tenant) (*types.Tenant, error) {
	s.updated = tenant
	return tenant, nil
}

// tinyPNG is a valid 1×1 PNG.
var tinyPNG = func() []byte {
	b, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		panic(err)
	}
	return b
}()

func brandingLogoRouter(svc *brandingLogoTenantService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &TenantHandler{service: svc}
	r := gin.New()
	r.Use(tenantPolicyErrorCapture())
	r.POST("/tenants/:id/branding/logo", h.UploadTenantBrandingLogo)
	r.GET("/branding/logo/:tenant_id", h.GetTenantBrandingLogo)
	return r
}

func multipartLogoBody(t *testing.T, field string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, "logo.png")
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

func TestUploadBrandingLogoStoresAssetAndPointsConfigAtIt(t *testing.T) {
	svc := &brandingLogoTenantService{}
	r := brandingLogoRouter(svc)

	body, ctype := multipartLogoBody(t, "file", tinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/tenants/10000/branding/logo", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, svc.saveCalled)
	require.Equal(t, "image/png", svc.savedType)
	require.Equal(t, len(tinyPNG), svc.savedLen)
	// branding_config.logo_url must point at the public read endpoint and
	// preserve the other branding fields.
	require.NotNil(t, svc.updated)
	require.NotNil(t, svc.updated.BrandingConfig)
	require.Equal(t, "/api/v1/branding/logo/10000", svc.updated.BrandingConfig.LogoURL)
	require.Equal(t, "kept", svc.updated.BrandingConfig.LoginTitle)
	require.Contains(t, w.Body.String(), `"logo_url":"/api/v1/branding/logo/10000"`)
}

func TestUploadBrandingLogoRejectsNonImage(t *testing.T) {
	svc := &brandingLogoTenantService{}
	r := brandingLogoRouter(svc)

	body, ctype := multipartLogoBody(t, "file", []byte("definitely not an image"))
	req := httptest.NewRequest(http.MethodPost, "/tenants/10000/branding/logo", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, 0, svc.saveCalled)
}

func TestUploadBrandingLogoRejectsOversize(t *testing.T) {
	svc := &brandingLogoTenantService{}
	r := brandingLogoRouter(svc)

	oversize := make([]byte, brandingLogoMaxBytes+1)
	copy(oversize, tinyPNG)
	body, ctype := multipartLogoBody(t, "file", oversize)
	req := httptest.NewRequest(http.MethodPost, "/tenants/10000/branding/logo", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, 0, svc.saveCalled)
}

func TestGetBrandingLogoServesBytesWithETag(t *testing.T) {
	svc := &brandingLogoTenantService{asset: &types.TenantBrandingAsset{
		TenantID:    10000,
		ContentType: "image/png",
		Data:        tinyPNG,
	}}
	r := brandingLogoRouter(svc)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/branding/logo/10000", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "image/png", w.Header().Get("Content-Type"))
	etag := w.Header().Get("ETag")
	require.NotEmpty(t, etag)
	require.True(t, bytes.Equal(tinyPNG, w.Body.Bytes()))

	// Revalidation with the matching ETag must 304.
	req := httptest.NewRequest(http.MethodGet, "/branding/logo/10000", nil)
	req.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)
	require.Equal(t, http.StatusNotModified, w2.Code)
}

func TestGetBrandingLogoMissingReturns404(t *testing.T) {
	svc := &brandingLogoTenantService{}
	r := brandingLogoRouter(svc)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/branding/logo/99999", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), apperrors.NewNotFoundError("").Message)
	require.False(t, strings.Contains(w.Body.String(), `"error":null`))
}
