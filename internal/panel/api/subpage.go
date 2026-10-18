package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/subpage"
)

// The subscription page as the admin builds it (subpage): its settings document. Every
// change is the session's: it is what every subscriber sees, links and CSS included, so a
// leaked key must not be able to put a phishing page in front of them. A key may read it.

type SubPageView struct {
	Config      subpage.Page `json:"config"`
	Defaults    subpage.Page `json:"defaults" doc:"Страница по умолчанию — к ней ведёт «Сбросить»"`
	Brand       string       `json:"brand" doc:"Название бренда (settings.brand)"`
	BrandAccent string       `json:"brand_accent" doc:"Цвет бренда (settings.brand_accent); его же получают приложения с брендингом"`
}

type subPageOutput struct{ Body SubPageView }

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
	return v, nil
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
	v, err := h.subPageView(ctx)
	if err != nil {
		return nil, err
	}
	return &subPageOutput{Body: v}, nil
}
