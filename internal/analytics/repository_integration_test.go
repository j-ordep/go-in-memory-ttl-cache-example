package analytics

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Testes de integração: rodam contra um banco com o turnover_schema.sql e o
// turnover_mock_data.sql (jan–mar/2026) carregados.
//
//	TEST_DATABASE_URL=postgres://... go test ./internal/analytics -run Integration -v
func integrationRepo(t *testing.T) (*Repo, context.Context) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL não definido")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return NewRepo(db, NewCatalog(db, time.Minute)), ctx
}

var admin = Viewer{Login: "admin"}

func mustMonth(t *testing.T, r *Repo, ctx context.Context, s string) time.Time {
	m, err := r.ResolveMonth(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func filter(t *testing.T, js string) *FilterNode {
	var f FilterNode
	if err := json.Unmarshal([]byte(js), &f); err != nil {
		t.Fatal(err)
	}
	return &f
}

func dump(t *testing.T, label string, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	if len(b) > 1500 {
		b = append(b[:1500], []byte("\n  ...")...)
	}
	t.Logf("%s:\n%s", label, b)
}

func TestIntegrationCatalogAndMonth(t *testing.T) {
	r, ctx := integrationRepo(t)
	cat, err := r.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Attributes) != 33 || len(cat.Factors) != 25 {
		t.Fatalf("catálogo: %d atributos, %d fatores", len(cat.Attributes), len(cat.Factors))
	}
	if m := mustMonth(t, r, ctx, ""); m.Format("2006-01") != "2026-03" {
		t.Fatalf("último mês = %s", m)
	}
}

func TestIntegrationSearchWithAdvancedFilter(t *testing.T) {
	r, ctx := integrationRepo(t)
	m := mustMonth(t, r, ctx, "2026-03")
	f := filter(t, `{"op":"and","conditions":[
		{"field":"department","operator":"in","value":["Logística","Comercial"]},
		{"field":"tenure_months","operator":"gt","value":60}]}`)
	page, err := r.SearchEmployees(ctx, admin, SearchParams{Month: m, Filter: f, SortBy: "salario_atual", SortDesc: true})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total == 0 {
		t.Fatal("nenhum resultado")
	}
	for _, e := range page.Items {
		if (*e.Department != "Logística" && *e.Department != "Comercial") || *e.TenureMonths <= 60 {
			t.Fatalf("resultado fora do filtro: %+v", e)
		}
	}
	dump(t, "busca (1º item)", page.Items[0])

	// gestor só vê o próprio departamento
	page, err = r.SearchEmployees(ctx, Viewer{Login: "vinicius.costa"}, SearchParams{Month: m})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Items {
		if *e.Department != "Logística" {
			t.Fatalf("vinicius.costa viu %s", *e.Department)
		}
	}
	t.Logf("vinicius.costa vê %d funcionários", page.Total)
}

func TestIntegrationDashboards(t *testing.T) {
	r, ctx := integrationRepo(t)
	m := mustMonth(t, r, ctx, "2026-03")

	o, err := r.Overview(ctx, admin, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.Total != 30 || o.Low+o.Medium+o.High != 30 {
		t.Fatalf("overview: %+v", o)
	}
	dump(t, "overview", o)

	dep, err := r.Breakdown(ctx, admin, BreakdownParams{Month: m, By: "department"})
	if err != nil {
		t.Fatal(err)
	}
	dump(t, "por departamento", dep)

	ten, err := r.Breakdown(ctx, admin, BreakdownParams{Month: m, By: "tenure_months", Buckets: []float64{12, 36, 60, 120}})
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, b := range ten {
		sum += b.Total
	}
	if sum != 30 {
		t.Fatalf("faixas de tempo de casa somam %d", sum)
	}
	dump(t, "por tempo de casa", ten)

	fil, err := r.Breakdown(ctx, admin, BreakdownParams{Month: m, By: "filial"})
	if err != nil {
		t.Fatal(err)
	}
	dump(t, "por filial (atributo dinâmico)", fil)

	opts, err := r.AttributeOptions(ctx, admin, m, "idiomas")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("opções de idiomas: %v", opts)
}

func TestIntegrationFactors(t *testing.T) {
	r, ctx := integrationRepo(t)
	m := mustMonth(t, r, ctx, "2026-03")

	sum, err := r.FactorsSummary(ctx, admin, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Factors) != 23 { // 25 menos os 2 invisíveis
		t.Fatalf("%d fatores visíveis", len(sum.Factors))
	}
	for _, f := range sum.Factors {
		if f.RiskCount+f.RetentionCount != f.Employees {
			t.Fatalf("contagens inconsistentes: %+v", f)
		}
	}
	dump(t, "resumo de fatores (totais)", sum.Totals)
	dump(t, "resumo de fatores (1º)", sum.Factors[0])

	sc, err := r.FactorScatter(ctx, admin, m, "salario_vigente",
		filter(t, `{"field":"risk_level","operator":"eq","value":"high"}`))
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range sc.Points {
		if p.RiskLevel != "high" || p.FeatureValue == nil {
			t.Fatalf("ponto inválido: %+v", p)
		}
		if i > 0 && p.Contribution > sc.Points[i-1].Contribution {
			t.Fatal("pontos não estão ordenados por contribuição")
		}
	}
	dump(t, "dispersão salario_vigente (alto risco)", sc)
}

func TestIntegrationEmployeeDetail(t *testing.T) {
	r, ctx := integrationRepo(t)
	m := mustMonth(t, r, ctx, "2026-03")

	d, err := r.EmployeeDetail(ctx, admin, "129068", m)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.History) != 3 || len(d.Factors) != 23 || len(d.Events) == 0 {
		t.Fatalf("detalhe incompleto: hist=%d fatores=%d eventos=%d", len(d.History), len(d.Factors), len(d.Events))
	}
	d.Factors = d.Factors[:3]
	dump(t, "detalhe 129068", d)

	// gestor de outro departamento não enxerga
	if _, err := r.EmployeeDetail(ctx, Viewer{Login: "ana.lima"}, "129068", m); err == nil {
		t.Fatal("ana.lima não deveria ver 129068")
	}
}

func TestIntegrationTurnover(t *testing.T) {
	r, ctx := integrationRepo(t)
	months, err := r.MonthlyTurnover(ctx, mustMonth(t, r, ctx, "2026-01"), mustMonth(t, r, ctx, "2026-03"))
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 3 || *months[1].Exits != 3 || *months[2].Exits != 2 || *months[1].TurnoverPct != 10.17 {
		dump(t, "mensal", months)
		t.Fatal("turnover mensal inesperado")
	}
	y, err := r.AnnualTurnover(ctx, 2026)
	if err != nil {
		t.Fatal(err)
	}
	y.Months = nil
	dump(t, "anual 2026", y)
	if y.TotalExits != 5 || y.LastMonth != 3 {
		t.Fatalf("anual inesperado: %+v", y)
	}
}
