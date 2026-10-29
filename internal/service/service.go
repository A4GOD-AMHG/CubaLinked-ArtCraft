// Package service implements artcraft.v1.ArtCraftService: handmade works with options and extra
// prices, their materials and cost estimate, custom orders on kit CUSTOM orders and the production
// board.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	artv1 "github.com/A4GOD-AMHG/CubaLinked-ArtCraftModule/proto/artcraft/v1"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/conv"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/core"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/domain"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/orders"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/retail"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/server"
	corev1 "github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/proto/core/v1"
	retailv1 "github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/proto/retail/v1"
)

const svcPrefix = "/artcraft.v1.ArtCraftService/"

// PublicMethods lists the ArtCraftService RPCs that work without a token.
func PublicMethods() []string {
	return []string{svcPrefix + "GetWork", svcPrefix + "ListWorks", svcPrefix + "GetPublicProfile"}
}

var Occasions = []string{"san valentín", "día de las madres", "día de los padres", "cumpleaños", "15 años", "boda", "aniversario", "graduación", "navidad", "baby shower"}

// Service implements artcraft.v1.ArtCraftService on top of retail and orders.
type Service struct {
	artv1.UnimplementedArtCraftServiceServer
	app    *server.App
	core   *core.Core
	retail *retail.Service
	orders *orders.Service
	pool   *pgxpool.Pool
}

// New builds the service from the installed retail and orders capabilities.
func New(app *server.App) *Service {
	rt, _ := app.Service(retail.Name).(*retail.Service)
	or, _ := app.Service(orders.Name).(*orders.Service)
	return &Service{app: app, core: app.Core, retail: rt, orders: or, pool: app.Pool}
}

func catToDB(c artv1.CraftCategory) string {
	return enumToDB(int32(c), artv1.CraftCategory_name, "other_craft")
}

func cleanTags(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

const workCols = `id, business_id, title, description, image_file_ids, category, techniques, occasions, made_to_order, base_price_cents, currency, production_days, labor_hours::float8,
	options, COALESCE(retail_product_id::text,''), is_active, sort_order, created_at`

func (s *Service) scanWork(ctx context.Context, row pgx.Row, withMaterials bool) (*artv1.Work, error) {
	var w artv1.Work
	var images []string
	var cat, currency string
	var price int64
	var opts []byte
	var created time.Time
	if err := row.Scan(&w.Id, &w.BusinessId, &w.Title, &w.Description, &images, &cat, &w.Techniques, &w.Occasions, &w.MadeToOrder, &price, &currency, &w.ProductionDays,
		&w.LaborHours, &opts, &w.RetailProductId, &w.IsActive, &w.SortOrder, &created); err != nil {
		return nil, mapErr(err)
	}
	w.Category = artv1.CraftCategory(enumFromDB(cat, artv1.CraftCategory_value))
	w.BasePrice = conv.MoneyPB(domain.Money{AmountCents: price, Currency: currency})
	w.CreatedAt = conv.Ts(created)
	_ = json.Unmarshal(opts, &w.Options)
	for _, f := range images {
		w.Images = append(w.Images, conv.ImagePB(s.core.Img(ctx, f)))
	}
	if withMaterials {
		w.Materials, _ = s.materials(ctx, w.BusinessId, w.Id)
	}
	return &w, nil
}

func (s *Service) materials(ctx context.Context, businessID, workID string) ([]*artv1.MaterialLine, error) {
	rows, err := s.pool.Query(ctx, `SELECT product_id::text, quantity::float8 FROM art_work_materials WHERE work_id::text=$1`, workID)
	if err != nil {
		return nil, mapErr(err)
	}
	var lines []*artv1.MaterialLine
	for rows.Next() {
		var l artv1.MaterialLine
		if err := rows.Scan(&l.ProductId, &l.Quantity); err != nil {
			rows.Close()
			return nil, mapErr(err)
		}
		lines = append(lines, &l)
	}
	rows.Close()
	if s.retail != nil {
		for _, l := range lines {
			if p, err := s.retail.GetProduct(ctx, &retailv1.GetProductRequest{BusinessId: businessID, ProductId: l.ProductId}); err == nil {
				l.Name, l.Unit, l.UnitCost = p.Name, p.Unit, p.Cost
			}
		}
	}
	return lines, nil
}

func validateOptions(opts []*artv1.WorkOption) error {
	seen := map[string]bool{}
	for _, o := range opts {
		o.Key = strings.ToLower(strings.TrimSpace(o.Key))
		if o.Key == "" || strings.TrimSpace(o.Label) == "" || seen[o.Key] {
			return invalid("each option needs a unique key and a label")
		}
		seen[o.Key] = true
		if !o.FreeText && len(o.Choices) == 0 {
			return invalid("option %s needs choices or free_text", o.Key)
		}
		for _, c := range o.Choices {
			if c.ExtraPrice != nil && c.ExtraPrice.AmountCents < 0 {
				return invalid("negative extra price in option %s", o.Key)
			}
		}
	}
	return nil
}

func (s *Service) upsertWork(ctx context.Context, req *artv1.UpsertWorkRequest, create bool) (*artv1.Work, error) {
	b, _, id, err := s.core.Authz(ctx, req.BusinessId, "manage")
	if err != nil {
		return nil, err
	}
	if _, err := s.core.RequireWritable(ctx, b); err != nil {
		return nil, err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, invalid("title required")
	}
	if req.ProductionDays < 0 || req.LaborHours < 0 {
		return nil, invalid("production_days and labor_hours must be >= 0")
	}
	if err := validateOptions(req.Options); err != nil {
		return nil, err
	}
	for _, f := range req.ImageFileIds {
		if err := s.core.Clients.Storage.Verify(ctx, f, b.ID, id.UserID); err != nil {
			return nil, fmt.Errorf("%w: image %s", err, f)
		}
	}
	price := conv.MoneyFrom(req.BasePrice, b.Currency)
	if price.AmountCents < 0 {
		return nil, invalid("negative base_price")
	}
	opts, _ := json.Marshal(req.Options)
	if req.Options == nil {
		opts = []byte("[]")
	}
	images := req.ImageFileIds
	if images == nil {
		images = []string{}
	}
	args := []any{b.ID, title, req.Description, images, catToDB(req.Category), cleanTags(req.Techniques), cleanTags(req.Occasions), req.MadeToOrder, price.AmountCents, price.Currency,
		req.ProductionDays, req.LaborHours, opts, nilIfEmpty(req.RetailProductId), req.SortOrder}
	if create {
		w, err := s.scanWork(ctx, s.pool.QueryRow(ctx, `INSERT INTO art_works (business_id, title, description, image_file_ids, category, techniques, occasions, made_to_order, base_price_cents, currency,
			production_days, labor_hours, options, retail_product_id, sort_order) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::uuid,$15) RETURNING `+workCols, args...), true)
		if err == nil && len(req.ImageFileIds) > 0 {
			s.core.Clients.Events.Publish(ctx, "gallery.image_uploaded", map[string]any{"business_id": b.ID, "vertical": b.Vertical, "kind": "art_work", "work_id": w.Id,
				"file_id": req.ImageFileIds[0], "pipeline": "product_shot"})
		}
		return w, err
	}
	return s.scanWork(ctx, s.pool.QueryRow(ctx, `UPDATE art_works SET title=$2, description=$3, image_file_ids=$4, category=$5, techniques=$6, occasions=$7, made_to_order=$8, base_price_cents=$9,
		currency=$10, production_days=$11, labor_hours=$12, options=$13, retail_product_id=$14::uuid, sort_order=$15, is_active=$16, updated_at=NOW()
		WHERE business_id=$1 AND id::text=$17 AND deleted_at IS NULL RETURNING `+workCols, append(args, req.IsActive, req.WorkId)...), true)
}

// CreateWork adds a handmade work with its options, occasions and lead days.
func (s *Service) CreateWork(ctx context.Context, req *artv1.UpsertWorkRequest) (*artv1.Work, error) {
	return s.upsertWork(ctx, req, true)
}

// UpdateWork changes a work; work_id is required.
func (s *Service) UpdateWork(ctx context.Context, req *artv1.UpsertWorkRequest) (*artv1.Work, error) {
	if req.WorkId == "" {
		return nil, invalid("work_id required")
	}
	return s.upsertWork(ctx, req, false)
}

// DeleteWork removes a work; managers only.
func (s *Service) DeleteWork(ctx context.Context, req *artv1.WorkIDRequest) (*corev1.MessageResponse, error) {
	b, _, _, err := s.core.Authz(ctx, req.BusinessId, "manage")
	if err != nil {
		return nil, err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE art_works SET deleted_at=NOW(), is_active=FALSE WHERE id::text=$1 AND business_id=$2 AND deleted_at IS NULL`, req.WorkId, b.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrNotFound
	}
	return conv.Ok("obra eliminada"), nil
}

func (s *Service) getWork(ctx context.Context, businessID, workID string, member bool) (*artv1.Work, error) {
	w, err := s.scanWork(ctx, s.pool.QueryRow(ctx, `SELECT `+workCols+` FROM art_works WHERE id::text=$1 AND business_id::text=$2 AND deleted_at IS NULL`, workID, businessID), member)
	if err != nil {
		return nil, err
	}
	if !member && !w.IsActive {
		return nil, domain.ErrNotFound
	}
	return w, nil
}

func (s *Service) isMember(ctx context.Context, businessID string) bool {
	_, _, _, err := s.core.Authz(ctx, businessID, "")
	return err == nil
}

// GetWork returns a public work with its options and materials.
func (s *Service) GetWork(ctx context.Context, req *artv1.WorkIDRequest) (*artv1.Work, error) {
	b, err := s.core.PublicBusiness(ctx, req.BusinessId)
	if err != nil {
		return nil, err
	}
	return s.getWork(ctx, b.ID, req.WorkId, s.isMember(ctx, b.ID))
}

func (s *Service) listWorks(ctx context.Context, businessID string, req *artv1.ListWorksRequest, member bool) (*artv1.ListWorksResponse, error) {
	where := `business_id=$1 AND deleted_at IS NULL`
	args := []any{businessID}
	if !(req.IncludeInactive && member) {
		where += ` AND is_active`
	}
	if req.Category != artv1.CraftCategory_CRAFT_CATEGORY_UNSPECIFIED {
		args = append(args, catToDB(req.Category))
		where += fmt.Sprintf(` AND category=$%d`, len(args))
	}
	if o := strings.ToLower(strings.TrimSpace(req.Occasion)); o != "" {
		args = append(args, o)
		where += fmt.Sprintf(` AND $%d = ANY(occasions)`, len(args))
	}
	if req.MadeToOrderOnly {
		where += ` AND made_to_order`
	}
	out := &artv1.ListWorksResponse{}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM art_works WHERE `+where, args...).Scan(&out.Total); err != nil {
		return nil, mapErr(err)
	}
	p := pageArgs(req.Page, req.PageSize)
	args = append(args, p.Size, p.Offset())
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT `+workCols+` FROM art_works WHERE `+where+` ORDER BY sort_order, created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		w, err := s.scanWork(ctx, rows, false)
		if err != nil {
			return nil, err
		}
		out.Works = append(out.Works, w)
	}
	return out, rows.Err()
}

// ListWorks returns the public catalog of works.
func (s *Service) ListWorks(ctx context.Context, req *artv1.ListWorksRequest) (*artv1.ListWorksResponse, error) {
	b, err := s.core.PublicBusiness(ctx, req.BusinessId)
	if err != nil {
		return nil, err
	}
	return s.listWorks(ctx, b.ID, req, s.isMember(ctx, b.ID))
}

// SetWorkMaterials sets the retail supplies and quantities one unit of a work consumes; managers
// only.
func (s *Service) SetWorkMaterials(ctx context.Context, req *artv1.SetWorkMaterialsRequest) (*artv1.Work, error) {
	b, _, _, err := s.core.Authz(ctx, req.BusinessId, "manage")
	if err != nil {
		return nil, err
	}
	if _, err := s.getWork(ctx, b.ID, req.WorkId, true); err != nil {
		return nil, err
	}
	if s.retail == nil {
		return nil, invalid("retail capability not installed")
	}
	merged := map[string]float64{}
	var order []string
	for _, m := range req.Materials {
		if m.ProductId == "" || m.Quantity <= 0 {
			return nil, invalid("each material needs product_id and quantity > 0")
		}
		if _, err := s.retail.GetProduct(ctx, &retailv1.GetProductRequest{BusinessId: b.ID, ProductId: m.ProductId}); err != nil {
			return nil, err
		}
		if _, ok := merged[m.ProductId]; !ok {
			order = append(order, m.ProductId)
		}
		merged[m.ProductId] += m.Quantity
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM art_work_materials WHERE work_id::text=$1`, req.WorkId); err != nil {
		return nil, mapErr(err)
	}
	for _, pid := range order {
		if _, err := tx.Exec(ctx, `INSERT INTO art_work_materials (work_id, product_id, quantity) VALUES ($1,$2,$3)`, req.WorkId, pid, merged[pid]); err != nil {
			return nil, mapErr(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.getWork(ctx, b.ID, req.WorkId, true)
}

// Estimate is the cost breakdown of a work in cents and the margin at its base price in percent.
type Estimate struct {
	Materials, Labor, Total, Suggested int64
	MarginAtBase                       float64
}

// EstimateCost adds materials and labor for the quantity and suggests a price with the markup on
// cost, rounded up to whole pesos.
func EstimateCost(lines []*artv1.MaterialLine, laborHours float64, hourlyRate int64, quantity int32, marginPercent int32, basePrice int64) Estimate {
	if quantity <= 0 {
		quantity = 1
	}
	var e Estimate
	for _, l := range lines {
		if l.UnitCost != nil {
			e.Materials += int64(math.Round(float64(l.UnitCost.AmountCents) * l.Quantity * float64(quantity)))
		}
	}
	e.Labor = int64(math.Round(laborHours * float64(hourlyRate) * float64(quantity)))
	e.Total = e.Materials + e.Labor
	e.Suggested = int64(math.Ceil(float64(e.Total)*(1+float64(marginPercent)/100)/100) * 100)
	if basePrice > 0 {
		revenue := float64(basePrice) * float64(quantity)
		e.MarginAtBase = math.Round((revenue-float64(e.Total))/revenue*10000) / 100
	}
	return e
}

// EstimateWorkCost prices a work's materials at current retail costs plus labor, for pricing
// decisions.
func (s *Service) EstimateWorkCost(ctx context.Context, req *artv1.EstimateWorkCostRequest) (*artv1.CostEstimate, error) {
	b, _, _, err := s.core.Authz(ctx, req.BusinessId, "")
	if err != nil {
		return nil, err
	}
	w, err := s.getWork(ctx, b.ID, req.WorkId, true)
	if err != nil {
		return nil, err
	}
	margin := req.MarginPercent
	if margin <= 0 {
		margin = 40
	}
	var rate int64
	if req.HourlyRate != nil {
		rate = req.HourlyRate.AmountCents
	}
	qty := req.Quantity
	if qty <= 0 {
		qty = 1
	}
	e := EstimateCost(w.Materials, w.LaborHours, rate, qty, margin, w.BasePrice.AmountCents)
	m := func(v int64) *corev1.Money { return conv.MoneyPB(domain.Money{AmountCents: v, Currency: b.Currency}) }
	out := &artv1.CostEstimate{Materials: m(e.Materials), Labor: m(e.Labor), TotalCost: m(e.Total), SuggestedPrice: m(e.Suggested), MarginPercentAtBase: e.MarginAtBase, Lines: w.Materials}
	if s.retail != nil {
		for _, l := range w.Materials {
			if p, err := s.retail.GetProduct(ctx, &retailv1.GetProductRequest{BusinessId: b.ID, ProductId: l.ProductId}); err == nil && p.TrackStock && p.StockQty < l.Quantity*float64(qty) {
				out.MissingStock = append(out.MissingStock, fmt.Sprintf("%s: hay %.2f, se necesitan %.2f", p.Name, p.StockQty, l.Quantity*float64(qty)))
			}
		}
	}
	return out, nil
}

// GetPublicProfile returns the public page data of an artisan by id or slug, with works for sale.
func (s *Service) GetPublicProfile(ctx context.Context, req *artv1.GetPublicProfileRequest) (*artv1.PublicProfile, error) {
	var biz *corev1.Business
	var err error
	if req.Slug != "" {
		biz, err = s.core.GetBusinessBySlug(ctx, &corev1.GetBusinessBySlugRequest{Slug: req.Slug})
	} else {
		biz, err = s.core.GetBusiness(ctx, &corev1.GetBusinessRequest{BusinessId: req.BusinessId})
	}
	if err != nil {
		return nil, err
	}
	out := &artv1.PublicProfile{Business: biz}
	if ws, err := s.listWorks(ctx, biz.Id, &artv1.ListWorksRequest{PageSize: 48}, false); err == nil {
		out.Works = ws.Works
		seen := map[string]bool{}
		for _, w := range ws.Works {
			for _, o := range w.Occasions {
				if !seen[o] {
					seen[o] = true
					out.Occasions = append(out.Occasions, o)
				}
			}
		}
	}
	if s.retail != nil {
		if pr, err := s.retail.ListProducts(ctx, &retailv1.ListProductsRequest{BusinessId: biz.Id, OnlyForSale: true, PageSize: 60}); err == nil {
			out.Products = pr.Products
		}
	}
	return out, nil
}
