package analytics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DataType é o tipo de um atributo, vindo de attribute_definitions.data_type.
// Define o cast no SQL e quais operadores o filtro aceita.
type DataType string

const (
	TypeText     DataType = "text"
	TypeCategory DataType = "category"
	TypeNumber   DataType = "number"
	TypeDate     DataType = "date"
	TypeBoolean  DataType = "boolean"
)

// AttributeDef espelha uma linha de attribute_definitions.
type AttributeDef struct {
	Key          string   `json:"key"`
	DataType     DataType `json:"dataType"`
	Storage      string   `json:"storage"` // column | attributes | derived
	Unit         *string  `json:"unit"`
	Filterable   bool     `json:"filterable"`
	SimpleFilter bool     `json:"simpleFilter"`
	DisplayOrder int      `json:"displayOrder"`
}

// RiskFactor espelha uma linha de risk_factors.
type RiskFactor struct {
	Key          string  `json:"key"`
	ValueType    string  `json:"valueType"` // number | category | boolean
	Unit         *string `json:"unit"`
	Visible      bool    `json:"visible"`
	DisplayOrder int     `json:"displayOrder"`
}

// Snapshot é uma cópia imutável do catálogo. Cada requisição pega um
// snapshot e usa ele do início ao fim, sem lock.
type Snapshot struct {
	Attributes []AttributeDef `json:"attributes"`
	Factors    []RiskFactor   `json:"factors"`

	attrByKey   map[string]AttributeDef
	factorByKey map[string]RiskFactor
}

func NewSnapshot(attrs []AttributeDef, factors []RiskFactor) *Snapshot {
	s := &Snapshot{
		Attributes:  attrs,
		Factors:     factors,
		attrByKey:   make(map[string]AttributeDef, len(attrs)),
		factorByKey: make(map[string]RiskFactor, len(factors)),
	}
	for _, a := range attrs {
		s.attrByKey[a.Key] = a
	}
	for _, f := range factors {
		s.factorByKey[f.Key] = f
	}
	return s
}

func (s *Snapshot) Attribute(key string) (AttributeDef, bool) {
	a, ok := s.attrByKey[key]
	return a, ok
}

func (s *Snapshot) Factor(key string) (RiskFactor, bool) {
	f, ok := s.factorByKey[key]
	return f, ok
}

// Catalog mantém attribute_definitions e risk_factors em memória.
// São poucas linhas e só mudam quando a IA processa um mês, então um
// cache com TTL curto mais Invalidate() ao fim da análise basta.
type Catalog struct {
	db  *pgxpool.Pool
	ttl time.Duration

	mu       sync.Mutex
	snap     *Snapshot
	loadedAt time.Time
}

func NewCatalog(db *pgxpool.Pool, ttl time.Duration) *Catalog {
	return &Catalog{db: db, ttl: ttl}
}

// NewStaticCatalog cria um catálogo fixo, sem banco (útil em testes).
func NewStaticCatalog(s *Snapshot) *Catalog {
	return &Catalog{snap: s, loadedAt: time.Now(), ttl: 1<<63 - 1}
}

func (c *Catalog) Get(ctx context.Context) (*Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.snap != nil && time.Since(c.loadedAt) < c.ttl {
		return c.snap, nil
	}
	
	snap, err := loadSnapshot(ctx, c.db)
	if err != nil {
		if c.snap != nil {
			return c.snap, nil // banco indisponível: segue com o último catálogo bom
		}
		return nil, err
	}
	c.snap, c.loadedAt = snap, time.Now()
	return snap, nil
}

// Invalidate força a recarga na próxima requisição. Chame quando a
// análise de um mês terminar.
func (c *Catalog) Invalidate() {
	c.mu.Lock()
	c.loadedAt = time.Time{}
	c.mu.Unlock()
}

func loadSnapshot(ctx context.Context, db *pgxpool.Pool) (*Snapshot, error) {
	rows, err := db.Query(ctx, `
		SELECT key, data_type, storage, unit, filterable, simple_filter, display_order
		FROM attribute_definitions
		ORDER BY display_order, key`)
	if err != nil {
		return nil, fmt.Errorf("carregando attribute_definitions: %w", err)
	}
	attrs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AttributeDef, error) {
		var a AttributeDef
		var dt string
		err := r.Scan(&a.Key, &dt, &a.Storage, &a.Unit, &a.Filterable, &a.SimpleFilter, &a.DisplayOrder)
		a.DataType = DataType(dt)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("lendo attribute_definitions: %w", err)
	}

	rows, err = db.Query(ctx, `
		SELECT key, value_type, unit, visible, display_order
		FROM risk_factors
		ORDER BY display_order, key`)
	if err != nil {
		return nil, fmt.Errorf("carregando risk_factors: %w", err)
	}
	factors, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (RiskFactor, error) {
		var f RiskFactor
		err := r.Scan(&f.Key, &f.ValueType, &f.Unit, &f.Visible, &f.DisplayOrder)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("lendo risk_factors: %w", err)
	}

	return NewSnapshot(attrs, factors), nil
}
