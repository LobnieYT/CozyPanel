package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/subpage"
)

// The subscription page as the admin builds it (subpage): its settings document. Every
// change is the session's: it is what every subscriber sees, links and CSS included, so a
// leaked key must not be able to put a phishing page in front of them. A key may read it.

type SubPageView struct {
	Config      subpage.Page       `json:"config"`
	Defaults    subpage.Page       `json:"defaults" doc:"Страница по умолчанию — к ней ведёт «Сбросить»"`
	Brand       string             `json:"brand" doc:"Название бренда (settings.brand)"`
	BrandAccent string             `json:"brand_accent" doc:"Цвет бренда (settings.brand_accent); его же получают приложения с брендингом"`
	Logo        *subpage.PageAsset `json:"logo,omitempty" doc:"Загруженный логотип страницы"`
}

type subPageOutput struct{ Body SubPageView }

// putSubLogoInput is the logo as the browser has it: base64 in the body, since the file
// is small and the page's editor sends it with its form.
type putSubLogoInput struct {
	Body struct {
		Image string `json:"image" minLength:"50" maxLength:"800000" doc:"Картинка base64: PNG, JPEG или WebP до 512 КБ. SVG не принимается"`
	}
}

type putSubLogoOutput struct{ Body subpage.PageAsset }

// getSubLogoOutput is the picture itself, base64 for the editor's preview: the panel
// serves it to subscribers under the subscription path, this copy is for the admin.
type getSubLogoOutput struct {
	Body struct {
		Image string `json:"image"`
		Type  string `json:"content_type"`
	}
}

type deleteSubLogoOutput struct {
	Body struct {
		Deleted bool `json:"deleted"`
	}
}

type putSubPageInput struct {
	Body struct {
		Config      subpage.Page `json:"config"`
		Brand       *string      `json:"brand,omitempty" maxLength:"40"`
		BrandAccent *string      `json:"brand_accent,omitempty" maxLength:"7" doc:"#RRGGBB или пусто"`
	}
}

func (h *handlers) registerSubPage() {
	tags := []string{"sub-page"}
	huma.Register(h.api, huma.Operation{OperationID: "get-sub-page", Method: http.MethodGet, Path: "/api/v1/sub-page", Summary: "Страница подписки: вид, блоки, приложения", Tags: tags}, h.getSubPage)
	huma.Register(h.api, huma.Operation{OperationID: "update-sub-page", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPut, Path: "/api/v1/sub-page", Summary: "Сохранить страницу подписки", Tags: tags}, h.putSubPage)
	huma.Register(h.api, huma.Operation{OperationID: "get-sub-page-logo", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodGet, Path: "/api/v1/sub-page/logo", Summary: "Загруженный логотип страницы подписки (для предпросмотра)", Tags: tags}, h.getSubLogo)
	huma.Register(h.api, huma.Operation{OperationID: "upload-sub-page-logo", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPut, Path: "/api/v1/sub-page/logo", Summary: "Загрузить логотип страницы подписки", Tags: tags}, h.putSubLogo)
	huma.Register(h.api, huma.Operation{OperationID: "delete-sub-page-logo", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodDelete, Path: "/api/v1/sub-page/logo", Summary: "Убрать логотип страницы подписки", Tags: tags}, h.deleteSubLogo)
}

func (h *handlers) subPages() *subpage.Service { return subpage.NewService(h.d.Store.Q) }

func (h *handlers) subPageView(ctx context.Context) (SubPageView, error) {
	c, err := h.subPages().Page(ctx)
	if err != nil {
		return SubPageView{}, err
	}
	v := SubPageView{Config: c, Defaults: subpage.Default()}
	if v.Brand, err = h.d.Settings.String(ctx, settings.KeyBrand); err != nil {
		return v, err
	}
	if v.Brand == "" {
		v.Brand = "VPN"
	}
	if v.BrandAccent, err = h.d.Settings.String(ctx, settings.KeyBrandAccent); err != nil {
		return v, err
	}
	if logo, ok, err := h.subPages().Logo(ctx); err == nil && ok {
		v.Logo = &logo
	}
	return v, nil
}

// subLogoDir is where the page's picture lives; the name under it is the one the setting
// keeps, and nothing else ever reaches this folder.
func (h *handlers) subLogoDir() string { return filepath.Join(h.d.DataDir, "subpage") }

// subLogoPath resolves the stored name inside the folder; a hostile name from an older or
// hand-made setting never escapes it.
func (h *handlers) subLogoPath(name string) (string, bool) {
	if h.d.DataDir == "" || name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\\`) || strings.HasPrefix(name, ".") {
		return "", false
	}
	return filepath.Join(h.subLogoDir(), name), true
}

func (h *handlers) getSubLogo(ctx context.Context, _ *struct{}) (*getSubLogoOutput, error) {
	logo, ok, err := h.subPages().Logo(ctx)
	if err != nil || !ok {
		return nil, huma.Error404NotFound("no_logo")
	}
	path, ok := h.subLogoPath(logo.Name)
	if !ok {
		return nil, huma.Error404NotFound("no_logo")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, huma.Error404NotFound("no_logo")
	}
	return &getSubLogoOutput{Body: struct {
		Image string `json:"image"`
		Type  string `json:"content_type"`
	}{Image: base64.StdEncoding.EncodeToString(raw), Type: logo.Type}}, nil
}

// refreshSubPage puts the new logo into the page's <head> of the running panel: the
// subscriber's page is served from the panel, so it changes without a restart.
func (h *handlers) refreshSubPage() {
	if h.d.RefreshSubPage != nil {
		h.d.RefreshSubPage()
	}
}

func (h *handlers) putSubLogo(ctx context.Context, in *putSubLogoInput) (*putSubLogoOutput, error) {
	raw, err := base64.StdEncoding.DecodeString(in.Body.Image)
	if err != nil || len(raw) == 0 {
		return nil, huma.Error422UnprocessableEntity("bad_image")
	}
	ct, err := subpage.CheckImage(subpage.AssetLogo, raw)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("bad_image")
	}
	if h.d.DataDir == "" {
		return nil, huma.Error409Conflict("no_data_dir")
	}
	name, err := randHex(16)
	if err != nil {
		return nil, err
	}
	name += subpage.Ext(ct)
	if err := os.MkdirAll(h.subLogoDir(), 0o700); err != nil {
		return nil, err
	}
	svc := h.subPages()
	if err := os.WriteFile(filepath.Join(h.subLogoDir(), name), raw, 0o600); err != nil {
		return nil, err
	}
	old, _, _ := svc.Logo(ctx)
	asset := subpage.PageAsset{
		Name:      name,
		Type:      ct,
		Hash:      subpage.Hash(raw),
		Size:      int64(len(raw)),
		UpdatedAt: h.d.Now().Unix(),
	}
	if err := svc.SetLogo(ctx, asset); err != nil {
		_ = os.Remove(filepath.Join(h.subLogoDir(), name))
		return nil, err
	}
	// The old picture is dropped only after the new one is recorded.
	if old.Name != "" {
		if path, ok := h.subLogoPath(old.Name); ok {
			_ = os.Remove(path)
		}
	}
	h.audit(ctx, sessionOf(ctx).AdminID, "subpage.logo", "sub-page", "logo", nil)
	h.refreshSubPage()
	return &putSubLogoOutput{Body: asset}, nil
}

func (h *handlers) deleteSubLogo(ctx context.Context, _ *struct{}) (*deleteSubLogoOutput, error) {
	svc := h.subPages()
	old, ok, err := svc.Logo(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &deleteSubLogoOutput{Body: struct {
			Deleted bool `json:"deleted"`
		}{}}, nil
	}
	if err := svc.SetLogo(ctx, subpage.PageAsset{}); err != nil {
		return nil, err
	}
	if path, ok := h.subLogoPath(old.Name); ok {
		_ = os.Remove(path)
	}
	h.audit(ctx, sessionOf(ctx).AdminID, "subpage.logo", "sub-page", "logo", nil)
	h.refreshSubPage()
	return &deleteSubLogoOutput{Body: struct {
		Deleted bool `json:"deleted"`
	}{Deleted: true}}, nil
}

func (h *handlers) getSubPage(ctx context.Context, _ *struct{}) (*subPageOutput, error) {
	v, err := h.subPageView(ctx)
	if err != nil {
		return nil, err
	}
	return &subPageOutput{Body: v}, nil
}

// problems turns what subpage found into the API's field errors under prefix.
func problems(prefix string, ps []subpage.Problem) []*huma.ErrorDetail {
	out := make([]*huma.ErrorDetail, 0, len(ps))
	for _, p := range ps {
		out = append(out, &huma.ErrorDetail{Location: prefix + p.Field, Message: p.Code, Value: p.Value})
	}
	return out
}

func (h *handlers) putSubPage(ctx context.Context, in *putSubPageInput) (*subPageOutput, error) {
	c := in.Body.Config
	details := problems("body.config.", subpage.Normalize(&c))
	accent := ""
	if in.Body.BrandAccent != nil {
		accent = strings.ToUpper(strings.TrimSpace(*in.Body.BrandAccent))
		if accent != "" && !subpage.ValidColor(accent) {
			details = append(details, &huma.ErrorDetail{Location: "body.brand_accent", Message: "color_invalid"})
		}
	} else if saved, err := h.d.Settings.String(ctx, settings.KeyBrandAccent); err != nil {
		return nil, err
	} else {
		accent = saved
	}
	if c.Look.Accent == "brand" && accent == "" {
		details = append(details, &huma.ErrorDetail{Location: "body.brand_accent", Message: "color_required"})
	}
	if in.Body.Brand != nil && (strings.ContainsAny(*in.Body.Brand, "\r\n") || subpage.HasControl(*in.Body.Brand)) {
		details = append(details, &huma.ErrorDetail{Location: "body.brand", Message: "one_line"})
	}
	if len(details) > 0 {
		return nil, &huma.ErrorModel{Status: http.StatusUnprocessableEntity, Title: "Validation failed", Errors: details}
	}
	if in.Body.BrandAccent != nil {
		if err := settings.Set(ctx, h.d.Settings, settings.KeyBrandAccent, accent); err != nil {
			return nil, err
		}
	}
	if in.Body.Brand != nil {
		brand := strings.TrimSpace(*in.Body.Brand)
		if err := settings.Set(ctx, h.d.Settings, settings.KeyBrand, brand); err != nil {
			return nil, err
		}
	}
	if _, err := h.subPages().Save(ctx, c); err != nil {
		return nil, err
	}
	// Страницу отдаёт сам панель: новое содержимое — в её <head> сразу, без перезапуска.
	h.refreshSubPage()
	v, err := h.subPageView(ctx)
	if err != nil {
		return nil, err
	}
	return &subPageOutput{Body: v}, nil
}
