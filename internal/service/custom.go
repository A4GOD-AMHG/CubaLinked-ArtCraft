package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	artv1 "github.com/A4GOD-AMHG/CubaLinked-ArtCraftModule/proto/artcraft/v1"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/conv"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/domain"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/grpcx"
	ordersv1 "github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/proto/orders/v1"
	retailv1 "github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/proto/retail/v1"
)

// PriceWithOptions prices a work from its base price plus the extra price of each chosen option,
// times the quantity. Required options must be chosen, fixed options must match a defined choice
// and unknown options are rejected.
func PriceWithOptions(base int64, defs []*artv1.WorkOption, chosen []*artv1.ChosenOption, quantity int32) (int64, []*artv1.ChosenOption, error) {
	if quantity <= 0 {
		quantity = 1
	}
	picked := map[string]string{}
	for _, c := range chosen {
		picked[strings.ToLower(strings.TrimSpace(c.Key))] = strings.TrimSpace(c.Value)
	}
	unit := base
	var out []*artv1.ChosenOption
	for _, d := range defs {
		v, ok := picked[d.Key]
		if !ok || v == "" {
			if d.Required {
				return 0, nil, invalid("falta elegir %s", d.Label)
			}
			continue
		}
		if !d.FreeText {
			found := false
			for _, c := range d.Choices {
				if strings.EqualFold(c.Value, v) {
					found, v = true, c.Value
					if c.ExtraPrice != nil {
						unit += c.ExtraPrice.AmountCents
					}
					break
				}
			}
			if !found {
				return 0, nil, invalid("opción no válida para %s: %s", d.Label, v)
			}
		}
		out = append(out, &artv1.ChosenOption{Key: d.Key, Value: v})
		delete(picked, d.Key)
	}
	for k := range picked {
		return 0, nil, invalid("opción desconocida: %s", k)
	}
	return unit * int64(quantity), out, nil
}

// IsLate reports whether an order that is not ready, delivered, cancelled or rejected has passed
// the date the customer needs it.
func IsLate(neededBy *time.Time, status ordersv1.OrderStatus, now time.Time) bool {
	if neededBy == nil {
		return false
	}
	switch status {
	case ordersv1.OrderStatus_DELIVERED, ordersv1.OrderStatus_CANCELLED, ordersv1.OrderStatus_REJECTED, ordersv1.OrderStatus_READY:
		return false
	}
	return now.After(*neededBy)
}

type spec struct {
	WorkID, WorkTitle, Currency, Address, Gift string
	Options                                    []*artv1.ChosenOption
	Quantity                                   int32
	Price                                      int64
	Delivery                                   bool
	NeededBy, Consumed                         *time.Time
}

func (s *Service) loadSpec(ctx context.Context, orderID string) (*spec, error) {
	sp := &spec{}
	var opts []byte
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(work_id::text,''), work_title, options, quantity, estimated_price_cents, currency, delivery, delivery_address, gift_message, needed_by, materials_consumed_at
		FROM art_order_specs WHERE order_id::text=$1`, orderID).Scan(&sp.WorkID, &sp.WorkTitle, &opts, &sp.Quantity, &sp.Price, &sp.Currency, &sp.Delivery, &sp.Address, &sp.Gift, &sp.NeededBy, &sp.Consumed)
	if err != nil {
		return nil, mapErr(err)
	}
	_ = json.Unmarshal(opts, &sp.Options)
	return sp, nil
}

func customPB(o *ordersv1.Order, sp *spec, now time.Time) *artv1.CustomOrder {
	return &artv1.CustomOrder{Order: o, WorkId: sp.WorkID, WorkTitle: sp.WorkTitle, Options: sp.Options, Quantity: sp.Quantity,
		EstimatedPrice: conv.MoneyPB(domain.Money{AmountCents: sp.Price, Currency: sp.Currency}), Delivery: sp.Delivery, DeliveryAddress: sp.Address, GiftMessage: sp.Gift,
		MaterialsConsumed: sp.Consumed != nil, Late: IsLate(sp.NeededBy, o.Status, now)}
}

// RequestCustomOrder places a custom order for a catalog work or a free idea with reference photos,
// options, quantity, needed-by date and delivery. Works that are not made to order must be bought
// from the catalog, and the date must respect the work's lead days. It publishes
// artcraft.custom_order_requested.
func (s *Service) RequestCustomOrder(ctx context.Context, req *artv1.RequestCustomOrderRequest) (*artv1.CustomOrder, error) {
	if _, err := grpcx.MustIdentity(ctx); err != nil {
		return nil, err
	}
	if s.orders == nil {
		return nil, invalid("orders capability not installed")
	}
	b, err := s.core.PublicBusiness(ctx, req.BusinessId)
	if err != nil {
		return nil, err
	}
	qty := req.Quantity
	if qty <= 0 {
		qty = 1
	}
	sp := &spec{Quantity: qty, Currency: b.Currency, Delivery: req.Delivery, Address: strings.TrimSpace(req.DeliveryAddress), Gift: strings.TrimSpace(req.GiftMessage), NeededBy: tsTime(req.NeededBy)}
	title := strings.TrimSpace(req.Title)
	production := int32(3)
	if req.WorkId != "" {
		w, err := s.getWork(ctx, b.ID, req.WorkId, false)
		if err != nil {
			return nil, err
		}
		if !w.MadeToOrder {
			return nil, invalid("esta obra no se hace por encargo; cómprala en el catálogo")
		}
		price, opts, err := PriceWithOptions(w.BasePrice.AmountCents, w.Options, req.Options, qty)
		if err != nil {
			return nil, err
		}
		sp.WorkID, sp.WorkTitle, sp.Price, sp.Options = w.Id, w.Title, price, opts
		production = w.ProductionDays
		if title == "" {
			title = w.Title
		}
	} else if title == "" || strings.TrimSpace(req.Notes) == "" {
		return nil, invalid("para un encargo libre indica título y descripción")
	}
	if sp.Delivery && sp.Address == "" {
		return nil, invalid("delivery_address required for delivery")
	}
	earliest := time.Now().AddDate(0, 0, int(production))
	if sp.NeededBy != nil && sp.NeededBy.Before(earliest.Add(-12*time.Hour)) {
		return nil, invalid("este encargo necesita al menos %d días; fecha más temprana %s", production, earliest.Format("2006-01-02"))
	}
	var desc []string
	for _, o := range sp.Options {
		desc = append(desc, o.Key+": "+o.Value)
	}
	if sp.Gift != "" {
		desc = append(desc, "Mensaje: "+sp.Gift)
	}
	if sp.Delivery {
		desc = append(desc, "Entrega a domicilio: "+sp.Address)
	}
	if n := strings.TrimSpace(req.Notes); n != "" {
		desc = append(desc, n)
	}
	o, err := s.orders.CreateOrder(ctx, &ordersv1.CreateOrderRequest{BusinessId: b.ID, Kind: ordersv1.OrderKind_CUSTOM, Title: fmt.Sprintf("%s ×%d", title, qty),
		Description: strings.Join(desc, "\n"), ReferenceImageFileIds: req.ReferenceImageFileIds, RequestedFor: req.NeededBy, Quantity: float64(qty),
		CustomerName: req.CustomerName, CustomerPhone: req.CustomerPhone})
	if err != nil {
		return nil, err
	}
	opts, _ := json.Marshal(sp.Options)
	if sp.Options == nil {
		opts = []byte("[]")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO art_order_specs (order_id, business_id, work_id, work_title, options, quantity, estimated_price_cents, currency, delivery, delivery_address, gift_message, needed_by)
		VALUES ($1,$2,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, o.Id, b.ID, nilIfEmpty(sp.WorkID), sp.WorkTitle, opts, qty, sp.Price, sp.Currency, sp.Delivery, sp.Address, sp.Gift, sp.NeededBy); err != nil {
		return nil, mapErr(err)
	}
	s.core.Clients.Events.Publish(ctx, "artcraft.custom_order_requested", map[string]any{"business_id": b.ID, "vertical": b.Vertical, "order_id": o.Id, "work_id": sp.WorkID,
		"estimated_price_cents": sp.Price, "needed_by": sp.NeededBy})
	return customPB(o, sp, time.Now()), nil
}

// GetCustomOrder returns a custom order with its specification.
func (s *Service) GetCustomOrder(ctx context.Context, req *artv1.OrderRefRequest) (*artv1.CustomOrder, error) {
	if s.orders == nil {
		return nil, invalid("orders capability not installed")
	}
	o, err := s.orders.GetOrder(ctx, &ordersv1.OrderIDRequest{BusinessId: req.BusinessId, OrderId: req.OrderId})
	if err != nil {
		return nil, err
	}
	sp, err := s.loadSpec(ctx, o.Id)
	if err != nil {
		sp = &spec{Quantity: 1, Currency: "CUP"}
	}
	return customPB(o, sp, time.Now()), nil
}

// ConsumeOrderMaterials deducts the work's materials from retail stock once per order and moves the
// order into progress. Free-idea orders must deduct their materials from inventory directly.
func (s *Service) ConsumeOrderMaterials(ctx context.Context, req *artv1.OrderRefRequest) (*artv1.CustomOrder, error) {
	b, _, _, err := s.core.Authz(ctx, req.BusinessId, "")
	if err != nil {
		return nil, err
	}
	if s.retail == nil || s.orders == nil {
		return nil, invalid("retail and orders capabilities required")
	}
	o, err := s.orders.GetOrder(ctx, &ordersv1.OrderIDRequest{BusinessId: b.ID, OrderId: req.OrderId})
	if err != nil {
		return nil, err
	}
	sp, err := s.loadSpec(ctx, o.Id)
	if err != nil {
		return nil, err
	}
	if sp.Consumed != nil {
		return nil, invalid("los materiales de este encargo ya se descontaron")
	}
	if sp.WorkID == "" {
		return nil, invalid("encargo libre: descuenta los materiales desde el inventario")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE art_order_specs SET materials_consumed_at=NOW() WHERE order_id::text=$1 AND materials_consumed_at IS NULL`, o.Id)
	if err != nil {
		return nil, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, invalid("los materiales de este encargo ya se descontaron")
	}
	lines, err := s.materials(ctx, b.ID, sp.WorkID)
	if err != nil {
		return nil, err
	}
	var failed []string
	for _, l := range lines {
		if _, err := s.retail.AdjustStock(ctx, &retailv1.AdjustStockRequest{BusinessId: b.ID, ProductId: l.ProductId, Type: retailv1.StockMovementType_CONSUMPTION,
			Quantity: l.Quantity * float64(sp.Quantity), Note: "Encargo " + o.Title}); err != nil {
			failed = append(failed, l.Name)
		}
	}
	if len(failed) > 0 && len(failed) == len(lines) {
		_, _ = s.pool.Exec(ctx, `UPDATE art_order_specs SET materials_consumed_at=NULL WHERE order_id::text=$1`, o.Id)
		return nil, invalid("no se pudo descontar: %s", strings.Join(failed, ", "))
	}
	if o.Status == ordersv1.OrderStatus_ACCEPTED {
		if no, err := s.orders.UpdateOrderStatus(ctx, &ordersv1.UpdateOrderStatusRequest{BusinessId: b.ID, OrderId: o.Id, Status: ordersv1.OrderStatus_IN_PROGRESS}); err == nil {
			o = no
		}
	}
	now := time.Now()
	sp.Consumed = &now
	return customPB(o, sp, now), nil
}

// ListProductionBoard groups open custom orders by status for the coming days, flagging late ones.
func (s *Service) ListProductionBoard(ctx context.Context, req *artv1.ListProductionBoardRequest) (*artv1.ProductionBoard, error) {
	b, _, _, err := s.core.Authz(ctx, req.BusinessId, "")
	if err != nil {
		return nil, err
	}
	if s.orders == nil {
		return nil, invalid("orders capability not installed")
	}
	res, err := s.orders.ListOrders(ctx, &ordersv1.ListOrdersRequest{BusinessId: b.ID, Kind: ordersv1.OrderKind_CUSTOM, OpenOnly: true, PageSize: 200})
	if err != nil {
		return nil, err
	}
	days := req.DaysAhead
	if days <= 0 {
		days = 7
	}
	now := time.Now()
	horizon := now.AddDate(0, 0, int(days))
	cols := map[ordersv1.OrderStatus]*artv1.ProductionColumn{}
	out := &artv1.ProductionBoard{}
	for _, o := range res.Orders {
		sp, err := s.loadSpec(ctx, o.Id)
		if err != nil {
			sp = &spec{Quantity: 1, Currency: b.Currency}
		}
		co := customPB(o, sp, now)
		if co.Late {
			out.Late++
		}
		if sp.NeededBy != nil && !sp.NeededBy.After(horizon) {
			out.DueThisWeek++
		}
		c, ok := cols[o.Status]
		if !ok {
			c = &artv1.ProductionColumn{Status: o.Status}
			cols[o.Status] = c
		}
		c.Orders = append(c.Orders, co)
	}
	for _, st := range []ordersv1.OrderStatus{ordersv1.OrderStatus_REQUESTED, ordersv1.OrderStatus_QUOTED, ordersv1.OrderStatus_ACCEPTED, ordersv1.OrderStatus_IN_PROGRESS, ordersv1.OrderStatus_READY} {
		if c, ok := cols[st]; ok {
			sort.SliceStable(c.Orders, func(i, j int) bool {
				a, b := c.Orders[i].Order.RequestedFor, c.Orders[j].Order.RequestedFor
				if a == nil || b == nil {
					return a != nil
				}
				return a.AsTime().Before(b.AsTime())
			})
			out.Columns = append(out.Columns, c)
		}
	}
	return out, nil
}
