package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Viewer é quem está logado: a parte do e-mail antes do @, que é o
// valor gravado em employee_snapshots.visible_to.
type Viewer struct{ Login string }

type Repo struct {
	db  *pgxpool.Pool
	cat *Catalog
}

func NewRepo(db *pgxpool.Pool, cat *Catalog) *Repo { return &Repo{db: db, cat: cat} }

// scope monta o WHERE comum a todas as telas: mês + visibilidade + filtro.
// O filtro é compilado contra o catálogo, então o mesmo JSON serve para
// listagem, dashboards e gráficos de fatores.
func (r *Repo) scope(ctx context.Context, v Viewer, month time.Time, filter *FilterNode) (*Snapshot, *args, string, error) {
	if v.Login == "" {
		return nil, nil, "", fmt.Errorf("%w: usuário não identificado", ErrInvalidParam)
	}
	cat, err := r.cat.Get(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	a := &args{}
	where := fmt.Sprintf("em.reference_month = %s::date AND em.visible_to @> ARRAY[%s::text]",
		a.add(month), a.add(v.Login))
	f, err := compileFilter(cat, filter, a)
	if err != nil {
		return nil, nil, "", err
	}
	return cat, a, where + " AND " + f, nil
}

// ResolveMonth aceita "2026-03" ou "2026-03-01"; vazio = último mês processado.
func (r *Repo) ResolveMonth(ctx context.Context, s string) (time.Time, error) {
	if s == "" {
		var m *time.Time
		err := r.db.QueryRow(ctx, `SELECT max(reference_month) FROM competencies WHERE status = 'done'`).Scan(&m)
		if err != nil {
			return time.Time{}, err
		}
		if m == nil {
			return time.Time{}, fmt.Errorf("%w: nenhuma competência processada", ErrNotFound)
		}
		return *m, nil
	}
	for _, layout := range []string{"2006-01", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: mês %q (use AAAA-MM)", ErrInvalidParam, s)
}

// ---------------------------------------------------------------- catálogo

func (r *Repo) Catalog(ctx context.Context) (*Snapshot, error) { return r.cat.Get(ctx) }

// AttributeOptions devolve os valores existentes de um campo no mês,
// para preencher o multiselect do filtro.
func (r *Repo) AttributeOptions(ctx context.Context, v Viewer, month time.Time, key string) ([]string, error) {
	cat, a, where, err := r.scope(ctx, v, month, nil)
	if err != nil {
		return nil, err
	}
	def, ok := cat.Attribute(key)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	if def.DataType == TypeNumber || def.DataType == TypeDate {
		return nil, fmt.Errorf("%w: %q é %s; opções só existem para categorias", ErrInvalidParam, key, def.DataType)
	}
	expr, err := fieldExpr(def, a)
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf(`SELECT DISTINCT (%s)::text FROM employee_month em
		WHERE %s AND %s IS NOT NULL ORDER BY 1 LIMIT 1000`, expr, where, expr)
	rows, err := r.db.Query(ctx, sql, a.vals...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// ---------------------------------------------------------------- listagem

type SearchParams struct {
	Month    time.Time
	Filter   *FilterNode
	SortBy   string
	SortDesc bool
	Limit    int
	Offset   int
}

type EmployeeRow struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Gender       *string         `json:"gender"`
	Department   *string         `json:"department"`
	JobTitle     *string         `json:"jobTitle"`
	Region       *string         `json:"region"`
	Age          *int            `json:"age"`
	TenureMonths *int            `json:"tenureMonths"`
	RiskScore    float64         `json:"riskScore"`
	RiskLevel    string          `json:"riskLevel"`
	NetBalance   *float64        `json:"netBalance"`
	Attributes   json.RawMessage `json:"attributes"`
}

type Page[T any] struct {
	Total int `json:"total"`
	Items []T `json:"items"`
}

const employeeColumns = `em.employee_id, em.name, em.gender, em.department, em.job_title, em.region,
	em.age, em.tenure_months, em.risk_score::float8, em.risk_level, em.net_balance::float8, em.attributes`

func scanEmployee(row pgx.Row, extra ...any) (EmployeeRow, error) {
	var e EmployeeRow
	dest := append([]any{&e.ID, &e.Name, &e.Gender, &e.Department, &e.JobTitle, &e.Region,
		&e.Age, &e.TenureMonths, &e.RiskScore, &e.RiskLevel, &e.NetBalance, &e.Attributes}, extra...)
	err := row.Scan(dest...)
	return e, err
}

func (r *Repo) SearchEmployees(ctx context.Context, v Viewer, p SearchParams) (Page[EmployeeRow], error) {
	cat, a, where, err := r.scope(ctx, v, p.Month, p.Filter)
	if err != nil {
		return Page[EmployeeRow]{}, err
	}

	order := "em.risk_score"
	dir := "DESC"
	if p.SortBy != "" {
		def, ok := cat.Attribute(p.SortBy)
		if !ok {
			return Page[EmployeeRow]{}, fmt.Errorf("%w: ordenação por %q", ErrInvalidParam, p.SortBy)
		}
		if order, err = fieldExpr(def, a); err != nil {
			return Page[EmployeeRow]{}, err
		}
		dir = "ASC"
		if p.SortDesc {
			dir = "DESC"
		}
	}
	if p.Limit <= 0 || p.Limit > 500 {
		p.Limit = 50
	}
	if p.Offset < 0 {
		p.Offset = 0
	}

	sql := fmt.Sprintf(`SELECT %s, count(*) OVER()
		FROM employee_month em
		WHERE %s
		ORDER BY %s %s NULLS LAST, em.employee_id
		LIMIT %s OFFSET %s`,
		employeeColumns, where, order, dir, a.add(p.Limit), a.add(p.Offset))

	rows, err := r.db.Query(ctx, sql, a.vals...)
	if err != nil {
		return Page[EmployeeRow]{}, err
	}
	defer rows.Close()

	page := Page[EmployeeRow]{Items: []EmployeeRow{}}
	for rows.Next() {
		var total int64
		e, err := scanEmployee(rows, &total)
		if err != nil {
			return Page[EmployeeRow]{}, err
		}
		page.Total = int(total)
		page.Items = append(page.Items, e)
	}
	return page, rows.Err()
}

// ---------------------------------------------------------------- visão geral

type RiskOverview struct {
	Total         int      `json:"total"`
	Low           int      `json:"low"`
	Medium        int      `json:"medium"`
	High          int      `json:"high"`
	AvgRiskScore  *float64 `json:"avgRiskScore"`
	AvgNetBalance *float64 `json:"avgNetBalance"`
}

func (r *Repo) Overview(ctx context.Context, v Viewer, month time.Time, filter *FilterNode) (RiskOverview, error) {
	_, a, where, err := r.scope(ctx, v, month, filter)
	if err != nil {
		return RiskOverview{}, err
	}
	var o RiskOverview
	err = r.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*)::int,
		       count(*) FILTER (WHERE em.risk_level = 'low')::int,
		       count(*) FILTER (WHERE em.risk_level = 'medium')::int,
		       count(*) FILTER (WHERE em.risk_level = 'high')::int,
		       avg(em.risk_score)::float8,
		       avg(em.net_balance)::float8
		FROM employee_month em WHERE %s`, where), a.vals...).
		Scan(&o.Total, &o.Low, &o.Medium, &o.High, &o.AvgRiskScore, &o.AvgNetBalance)
	return o, err
}

// ---------------------------------------------------------------- quebra por campo

// BreakdownParams: o gráfico "por departamento", "por tempo de casa",
// "por filial"... é sempre o mesmo, só muda o campo.
// Para campos numéricos, Buckets define as faixas (ex.: tempo de casa
// [12, 36, 60, 120] gera <12, 12–36, 36–60, 60–120, ≥120 meses).
type BreakdownParams struct {
	Month   time.Time
	Filter  *FilterNode
	By      string
	Buckets []float64
}

type BreakdownRow struct {
	Group      *string  `json:"group"`      // categorias; null = não informado
	BucketFrom *float64 `json:"bucketFrom"` // faixas numéricas; null = sem limite inferior
	BucketTo   *float64 `json:"bucketTo"`   // null = sem limite superior
	Total      int      `json:"total"`
	Low        int      `json:"low"`
	Medium     int      `json:"medium"`
	High       int      `json:"high"`
}

func (r *Repo) Breakdown(ctx context.Context, v Viewer, p BreakdownParams) ([]BreakdownRow, error) {
	cat, a, where, err := r.scope(ctx, v, p.Month, p.Filter)
	if err != nil {
		return nil, err
	}
	def, ok := cat.Attribute(p.By)
	if !ok {
		return nil, fmt.Errorf("%w: agrupar por %q", ErrInvalidParam, p.By)
	}
	expr, err := fieldExpr(def, a)
	if err != nil {
		return nil, err
	}

	numeric := def.DataType == TypeNumber
	var groupExpr, orderBy string
	switch {
	case numeric:
		if len(p.Buckets) == 0 || len(p.Buckets) > 20 || !sort.Float64sAreSorted(p.Buckets) {
			return nil, fmt.Errorf("%w: %q é numérico; informe de 1 a 20 limites de faixa em ordem crescente", ErrInvalidParam, p.By)
		}
		groupExpr = fmt.Sprintf("width_bucket(%s, %s::numeric[])::text", expr, a.add(p.Buckets))
		orderBy = "g::int NULLS LAST"
	case def.DataType == TypeDate:
		groupExpr = fmt.Sprintf("extract(year FROM %s)::int::text", expr)
		orderBy = "g NULLS LAST"
	default:
		groupExpr = fmt.Sprintf("(%s)::text", expr)
		orderBy = "total DESC, g NULLS LAST"
	}

	sql := fmt.Sprintf(`
		SELECT g, count(*)::int AS total,
		       count(*) FILTER (WHERE risk_level = 'low')::int,
		       count(*) FILTER (WHERE risk_level = 'medium')::int,
		       count(*) FILTER (WHERE risk_level = 'high')::int
		FROM (SELECT %s AS g, em.risk_level FROM employee_month em WHERE %s) t
		GROUP BY g ORDER BY %s`, groupExpr, where, orderBy)

	rows, err := r.db.Query(ctx, sql, a.vals...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (BreakdownRow, error) {
		var b BreakdownRow
		err := row.Scan(&b.Group, &b.Total, &b.Low, &b.Medium, &b.High)
		return b, err
	})
	if err != nil || !numeric {
		return out, err
	}

	// width_bucket: 0 = abaixo do 1º limite, i = [limite i-1, limite i), n = acima do último
	for i := range out {
		if out[i].Group == nil {
			continue
		}
		var idx int
		fmt.Sscan(*out[i].Group, &idx)
		if idx > 0 {
			out[i].BucketFrom = &p.Buckets[idx-1]
		}
		if idx < len(p.Buckets) {
			out[i].BucketTo = &p.Buckets[idx]
		}
		out[i].Group = nil
	}
	return out, nil
}

// ---------------------------------------------------------------- fatores

type FactorSummary struct {
	Key             string  `json:"key"`
	ValueType       string  `json:"valueType"`
	Unit            *string `json:"unit"`
	AvgContribution float64 `json:"avgContribution"`
	Employees       int     `json:"employees"`
	RiskCount       int     `json:"riskCount"`
	RetentionCount  int     `json:"retentionCount"`
}

type FactorTotals struct {
	Employees         int      `json:"employees"`
	AvgTotalRisk      *float64 `json:"avgTotalRisk"`
	AvgTotalRetention *float64 `json:"avgTotalRetention"`
	AvgNetBalance     *float64 `json:"avgNetBalance"`
}

type FactorsOverview struct {
	Totals  FactorTotals    `json:"totals"`
	Factors []FactorSummary `json:"factors"`
}

// FactorsSummary: média de cada fator entre os funcionários do recorte,
// quantos estão do lado do risco/retenção, e a soma de todos os fatores
// (média de total_risk, total_retention e net_balance).
func (r *Repo) FactorsSummary(ctx context.Context, v Viewer, month time.Time, filter *FilterNode) (FactorsOverview, error) {
	cat, a, where, err := r.scope(ctx, v, month, filter)
	if err != nil {
		return FactorsOverview{}, err
	}

	var out FactorsOverview
	err = r.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*)::int, avg(em.total_risk)::float8, avg(em.total_retention)::float8, avg(em.net_balance)::float8
		FROM employee_month em WHERE %s`, where), a.vals...).
		Scan(&out.Totals.Employees, &out.Totals.AvgTotalRisk, &out.Totals.AvgTotalRetention, &out.Totals.AvgNetBalance)
	if err != nil {
		return FactorsOverview{}, err
	}

	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT f.factor_key,
		       avg(f.contribution)::float8,
		       count(*)::int,
		       count(*) FILTER (WHERE f.direction = 'risk')::int,
		       count(*) FILTER (WHERE f.direction = 'retention')::int
		FROM prediction_factors f
		JOIN employee_month em ON em.reference_month = f.reference_month AND em.employee_id = f.employee_id
		WHERE %s
		GROUP BY f.factor_key`, where), a.vals...)
	if err != nil {
		return FactorsOverview{}, err
	}
	stats, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FactorSummary, error) {
		var s FactorSummary
		err := row.Scan(&s.Key, &s.AvgContribution, &s.Employees, &s.RiskCount, &s.RetentionCount)
		return s, err
	})
	if err != nil {
		return FactorsOverview{}, err
	}

	byKey := make(map[string]FactorSummary, len(stats))
	for _, s := range stats {
		byKey[s.Key] = s
	}
	out.Factors = []FactorSummary{}
	for _, f := range cat.Factors { // ordem e visibilidade vêm do catálogo
		s, ok := byKey[f.Key]
		if !f.Visible || !ok {
			continue
		}
		s.ValueType, s.Unit = f.ValueType, f.Unit
		out.Factors = append(out.Factors, s)
	}
	return out, nil
}

type ScatterPoint struct {
	EmployeeID   string   `json:"employeeId"`
	Name         string   `json:"name"`
	RiskLevel    string   `json:"riskLevel"`
	FeatureValue *float64 `json:"featureValue"` // eixo X
	FeatureLabel *string  `json:"featureLabel"`
	Contribution float64  `json:"contribution"` // eixo Y
	Direction    string   `json:"direction"`
}

type FactorScatter struct {
	Factor RiskFactor     `json:"factor"`
	Points []ScatterPoint `json:"points"`
}

// FactorScatter alimenta o gráfico de dispersão de um fator e também a
// lista "funcionários dentro do fator" (já vem ordenada do maior risco
// para a maior retenção).
func (r *Repo) FactorScatter(ctx context.Context, v Viewer, month time.Time, key string, filter *FilterNode) (FactorScatter, error) {
	cat, a, where, err := r.scope(ctx, v, month, filter)
	if err != nil {
		return FactorScatter{}, err
	}
	f, ok := cat.Factor(key)
	if !ok {
		return FactorScatter{}, fmt.Errorf("%w: fator %q", ErrNotFound, key)
	}
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT em.employee_id, em.name, em.risk_level,
		       f.feature_value::float8, f.feature_label, f.contribution::float8, f.direction
		FROM prediction_factors f
		JOIN employee_month em ON em.reference_month = f.reference_month AND em.employee_id = f.employee_id
		WHERE %s AND f.factor_key = %s
		ORDER BY f.contribution DESC`, where, a.add(key)), a.vals...)
	if err != nil {
		return FactorScatter{}, err
	}
	pts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ScatterPoint, error) {
		var p ScatterPoint
		err := row.Scan(&p.EmployeeID, &p.Name, &p.RiskLevel, &p.FeatureValue, &p.FeatureLabel, &p.Contribution, &p.Direction)
		return p, err
	})
	return FactorScatter{Factor: f, Points: pts}, err
}

// ---------------------------------------------------------------- detalhe do funcionário

type EmployeeFactor struct {
	Key          string   `json:"key"`
	ValueType    string   `json:"valueType"`
	Unit         *string  `json:"unit"`
	FeatureValue *float64 `json:"featureValue"`
	FeatureLabel *string  `json:"featureLabel"`
	Contribution float64  `json:"contribution"`
	Direction    string   `json:"direction"`
}

type RiskPoint struct {
	Month      string   `json:"month"`
	RiskScore  float64  `json:"riskScore"`
	RiskLevel  string   `json:"riskLevel"`
	NetBalance *float64 `json:"netBalance"`
}

type Event struct {
	ID      int64           `json:"id"`
	Type    string          `json:"type"`
	Date    string          `json:"date"`
	Title   *string         `json:"title"`
	Details json.RawMessage `json:"details"`
	Source  string          `json:"source"`
}

type Suggestion struct {
	ID            int64     `json:"id"`
	Content       string    `json:"content"`
	RequestedBy   string    `json:"requestedBy"`
	CreatedAt     time.Time `json:"createdAt"`
	PromptVersion int       `json:"promptVersion"`
}

type EmployeeDetail struct {
	EmployeeRow
	Month          string           `json:"month"`
	BirthDate      *string          `json:"birthDate"`
	AdmissionDate  *string          `json:"admissionDate"`
	TotalRisk      *float64         `json:"totalRisk"`
	TotalRetention *float64         `json:"totalRetention"`
	Factors        []EmployeeFactor `json:"factors"`
	History        []RiskPoint      `json:"history"`
	Events         []Event          `json:"events"`
	Suggestions    []Suggestion     `json:"suggestions"`
}

func (r *Repo) EmployeeDetail(ctx context.Context, v Viewer, id string, month time.Time) (EmployeeDetail, error) {
	cat, a, where, err := r.scope(ctx, v, month, nil)
	if err != nil {
		return EmployeeDetail{}, err
	}

	var d EmployeeDetail
	row := r.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s, to_char(em.reference_month, 'YYYY-MM-DD'),
		       to_char(em.birth_date, 'YYYY-MM-DD'), to_char(em.admission_date, 'YYYY-MM-DD'),
		       em.total_risk::float8, em.total_retention::float8
		FROM employee_month em
		WHERE %s AND em.employee_id = %s`, employeeColumns, where, a.add(id)), a.vals...)
	d.EmployeeRow, err = scanEmployee(row, &d.Month, &d.BirthDate, &d.AdmissionDate, &d.TotalRisk, &d.TotalRetention)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmployeeDetail{}, fmt.Errorf("%w: funcionário %s em %s", ErrNotFound, id, month.Format("2006-01"))
	}
	if err != nil {
		return EmployeeDetail{}, err
	}

	// fatores do mês, do mais influente para o menos (risco ou retenção)
	rows, err := r.db.Query(ctx, `
		SELECT factor_key, feature_value::float8, feature_label, contribution::float8, direction
		FROM prediction_factors
		WHERE reference_month = $1 AND employee_id = $2
		ORDER BY abs(contribution) DESC`, month, id)
	if err != nil {
		return EmployeeDetail{}, err
	}
	all, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (EmployeeFactor, error) {
		var f EmployeeFactor
		err := row.Scan(&f.Key, &f.FeatureValue, &f.FeatureLabel, &f.Contribution, &f.Direction)
		return f, err
	})
	if err != nil {
		return EmployeeDetail{}, err
	}
	d.Factors = []EmployeeFactor{}
	for _, f := range all {
		if rf, ok := cat.Factor(f.Key); ok && rf.Visible {
			f.ValueType, f.Unit = rf.ValueType, rf.Unit
			d.Factors = append(d.Factors, f)
		}
	}

	// evolução do risco em todos os meses visíveis para o usuário
	rows, err = r.db.Query(ctx, `
		SELECT to_char(reference_month, 'YYYY-MM-DD'), risk_score::float8, risk_level, net_balance::float8
		FROM employee_snapshots
		WHERE employee_id = $1 AND visible_to @> ARRAY[$2::text]
		ORDER BY reference_month`, id, v.Login)
	if err != nil {
		return EmployeeDetail{}, err
	}
	if d.History, err = pgx.CollectRows(rows, pgx.RowToStructByPos[RiskPoint]); err != nil {
		return EmployeeDetail{}, err
	}

	rows, err = r.db.Query(ctx, `
		SELECT id, event_type, to_char(event_date, 'YYYY-MM-DD'), title, details, source
		FROM employee_events
		WHERE employee_id = $1
		ORDER BY event_date DESC, id DESC
		LIMIT 200`, id)
	if err != nil {
		return EmployeeDetail{}, err
	}
	if d.Events, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Event]); err != nil {
		return EmployeeDetail{}, err
	}

	rows, err = r.db.Query(ctx, `
		SELECT s.id, s.content, s.requested_by, s.created_at, p.version
		FROM suggestions s JOIN prompts p ON p.id = s.prompt_id
		WHERE s.employee_id = $1
		ORDER BY s.created_at DESC`, id)
	if err != nil {
		return EmployeeDetail{}, err
	}
	if d.Suggestions, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Suggestion]); err != nil {
		return EmployeeDetail{}, err
	}
	return d, nil
}

// ---------------------------------------------------------------- turnover

type TurnoverMonth struct {
	Month             string   `json:"month"`
	PreviousHeadcount *int     `json:"previousHeadcount"`
	Headcount         int      `json:"headcount"`
	Hires             *int     `json:"hires"`
	Exits             *int     `json:"exits"`
	AverageHeadcount  *float64 `json:"averageHeadcount"`
	TurnoverPct       *float64 `json:"turnoverPct"`
}

type AnnualTurnover struct {
	Year             int             `json:"year"`
	Competencies     int             `json:"competencies"`
	LastMonth        int             `json:"lastMonth"`
	TotalExits       int             `json:"totalExits"`
	AverageHeadcount float64         `json:"averageHeadcount"`
	AccumulatedPct   float64         `json:"accumulatedPct"`
	AnnualizedPct    float64         `json:"annualizedPct"`
	Months           []TurnoverMonth `json:"months"`
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// MonthlyTurnover aplica a fórmula mensal:
//
//	média = (headcount anterior + atual) / 2
//	turnover % = saídas aparentes / média × 100
//
// É uma métrica da empresa inteira (não aplica visible_to).
func (r *Repo) MonthlyTurnover(ctx context.Context, from, to time.Time) ([]TurnoverMonth, error) {
	rows, err := r.db.Query(ctx, `
		SELECT to_char(reference_month, 'YYYY-MM-DD'), previous_headcount::int, headcount::int, hires::int, exits::int
		FROM monthly_turnover
		WHERE reference_month BETWEEN $1 AND $2
		ORDER BY reference_month`, from, to)
	if err != nil {
		return nil, err
	}
	months, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TurnoverMonth, error) {
		var m TurnoverMonth
		err := row.Scan(&m.Month, &m.PreviousHeadcount, &m.Headcount, &m.Hires, &m.Exits)
		return m, err
	})
	if err != nil {
		return nil, err
	}
	for i := range months {
		m := &months[i]
		if m.PreviousHeadcount == nil || m.Exits == nil {
			continue // primeiro mês da série: sem base de comparação
		}
		avg := float64(*m.PreviousHeadcount+m.Headcount) / 2
		m.AverageHeadcount = &avg
		if avg > 0 {
			pct := round2(float64(*m.Exits) / avg * 100)
			m.TurnoverPct = &pct
		}
	}
	return months, nil
}

// AnnualTurnover aplica a fórmula anual:
//
//	média do período = soma dos headcounts das competências / quantidade
//	taxa acumulada % = total de saídas / média × 100
//	anualizado %     = acumulada × (12 / número do mês final)
func (r *Repo) AnnualTurnover(ctx context.Context, year int) (AnnualTurnover, error) {
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	months, err := r.MonthlyTurnover(ctx, from, from.AddDate(0, 11, 0))
	if err != nil {
		return AnnualTurnover{}, err
	}
	out := AnnualTurnover{Year: year, Months: months, Competencies: len(months)}
	if len(months) == 0 {
		return out, nil
	}
	sumHC := 0
	for _, m := range months {
		sumHC += m.Headcount
		if m.Exits != nil {
			out.TotalExits += *m.Exits
		}
	}
	last, _ := time.Parse("2006-01-02", months[len(months)-1].Month)
	out.LastMonth = int(last.Month())
	out.AverageHeadcount = round2(float64(sumHC) / float64(len(months)))
	if out.AverageHeadcount > 0 {
		acc := float64(out.TotalExits) / (float64(sumHC) / float64(len(months))) * 100
		out.AccumulatedPct = round2(acc)
		out.AnnualizedPct = round2(acc * 12 / float64(out.LastMonth))
	}
	return out, nil
}

// IsClientError diz se o erro é culpa do request (400/404) e não do servidor.
func IsClientError(err error) (status int, ok bool) {
	switch {
	case errors.Is(err, ErrNotFound):
		return 404, true
	case errors.Is(err, ErrInvalidFilter), errors.Is(err, ErrInvalidField), errors.Is(err, ErrInvalidParam):
		return 400, true
	}
	return 0, false
}
