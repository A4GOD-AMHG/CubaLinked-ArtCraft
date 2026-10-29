package service

import (
	"testing"
	"time"

	artv1 "github.com/A4GOD-AMHG/CubaLinked-ArtCraftModule/proto/artcraft/v1"
	corev1 "github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/proto/core/v1"
	ordersv1 "github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/proto/orders/v1"
)

func defs() []*artv1.WorkOption {
	return []*artv1.WorkOption{
		{Key: "color", Label: "Color", Required: true, Choices: []*artv1.OptionChoice{{Value: "Rojo"}, {Value: "Dorado", ExtraPrice: &corev1.Money{AmountCents: 50000}}}},
		{Key: "flores", Label: "Cantidad de flores", Choices: []*artv1.OptionChoice{{Value: "6"}, {Value: "12", ExtraPrice: &corev1.Money{AmountCents: 100000}}}},
		{Key: "mensaje", Label: "Mensaje", FreeText: true},
	}
}

func TestPriceWithOptions(t *testing.T) {
	total, opts, err := PriceWithOptions(200000, defs(), []*artv1.ChosenOption{{Key: "color", Value: "dorado"}, {Key: "flores", Value: "12"}, {Key: "mensaje", Value: "Feliz día mamá"}}, 2)
	if err != nil || total != 700000 || len(opts) != 3 || opts[0].Value != "Dorado" {
		t.Fatalf("total %d opts %v err %v", total, opts, err)
	}
	if _, _, err := PriceWithOptions(200000, defs(), nil, 1); err == nil {
		t.Fatal("missing required option must fail")
	}
	if _, _, err := PriceWithOptions(200000, defs(), []*artv1.ChosenOption{{Key: "color", Value: "Azul"}}, 1); err == nil {
		t.Fatal("invalid choice must fail")
	}
	if _, _, err := PriceWithOptions(200000, defs(), []*artv1.ChosenOption{{Key: "color", Value: "Rojo"}, {Key: "talla", Value: "M"}}, 1); err == nil {
		t.Fatal("unknown option must fail")
	}
}

func TestEstimateCost(t *testing.T) {
	lines := []*artv1.MaterialLine{{Quantity: 2, UnitCost: &corev1.Money{AmountCents: 15000}}, {Quantity: 0.5, UnitCost: &corev1.Money{AmountCents: 40000}}}
	e := EstimateCost(lines, 2, 25000, 1, 40, 200000)
	if e.Materials != 50000 || e.Labor != 50000 || e.Total != 100000 || e.Suggested != 140000 || e.MarginAtBase != 50 {
		t.Fatalf("estimate %+v", e)
	}
}

func TestLateAndOptions(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	if !IsLate(&past, ordersv1.OrderStatus_IN_PROGRESS, now) || IsLate(&past, ordersv1.OrderStatus_READY, now) || IsLate(nil, ordersv1.OrderStatus_ACCEPTED, now) {
		t.Fatal("late")
	}
	if err := validateOptions([]*artv1.WorkOption{{Key: "a", Label: "A"}}); err == nil {
		t.Fatal("option without choices must fail")
	}
	if err := validateOptions(defs()); err != nil {
		t.Fatal(err)
	}
	if len(cleanTags([]string{" Boda", "boda", ""})) != 1 || len(PublicMethods()) != 3 {
		t.Fatal("tags or public methods")
	}
}
