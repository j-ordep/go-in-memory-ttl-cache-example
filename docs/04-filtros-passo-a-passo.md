# 04 — Filtros passo a passo

Código: [filter.go](../internal/analytics/filter.go). Para um exemplo verificado pelo próprio projeto, leia [filter_test.go](../internal/analytics/filter_test.go).

## 1. Por que receber JSON em vez de SQL

O cliente informa o que deseja filtrar. O backend decide quais campos, operadores e tipos são aceitos e como representar isso no banco.

Um filtro simples:

```json
{
  "field": "department",
  "operator": "eq",
  "value": "Comercial"
}
```

Um filtro composto:

```json
{
  "op": "and",
  "conditions": [
    { "field": "department", "operator": "eq", "value": "Comercial" },
    {
      "op": "or",
      "conditions": [
        { "field": "tenure_months", "operator": "gt", "value": 12 },
        { "field": "risk_level", "operator": "eq", "value": "high" }
      ]
    }
  ]
}
```

Leia como: departamento Comercial **e** (tempo de casa maior que 12 **ou** nível de risco alto).

## 2. FilterNode representa uma árvore

```go
type FilterNode struct {
    Op         string
    Conditions []FilterNode
    Field      string
    Operator   string
    Value      json.RawMessage
}
```

Um nó pode representar um grupo ou uma condição. Um grupo tem filhos em `Conditions`; esses filhos também são `FilterNode`. Essa repetição permite aninhar grupos.

`json.RawMessage` guarda o valor como JSON ainda não interpretado em um tipo específico. O compilador espera descobrir o tipo no catálogo antes de decidir se precisa de número, texto, booleano ou data.

O nome “árvore” descreve as relações entre os nós, não uma tabela adicional no banco.

## 3. compileFilter prepara o trabalho

Se o filtro for `nil`, a função devolve `TRUE`. Assim o repository consegue escrever sempre “recorte obrigatório AND filtro” sem acrescentar uma restrição quando nenhum filtro foi enviado.

Caso exista filtro, cria:

```go
c := &filterCompiler{cat: cat, a: a}
return c.node(n, 0)
```

A struct do compilador guarda:

- `cat`: a versão do catálogo usada na operação;
- `a`: os argumentos compartilhados com a consulta;
- `count`: quantas condições já foram visitadas.

Esse objeto é criado por compilação. Não é um contador global entre requisições.

## 4. node percorre os grupos por recursão

Recursão significa uma função chamar a si mesma para resolver uma parte menor do problema.

Em `node`:

1. Confere a profundidade.
2. Se `Op` estiver preenchido, trata o nó como grupo.
3. Rejeita grupo que também informe `Field`.
4. Aceita apenas `and` ou `or`.
5. Chama `node` para cada filho.
6. Une as partes e acrescenta parênteses.
7. Sem `Op`, encaminha para `condition`.

Para o exemplo do início, o resultado tem esta forma:

```sql
(em.department = $1::text
 AND (em.tenure_months > $2::numeric
      OR em.risk_level = $3::text))
```

Esse exemplo mostra a compilação isolada, começando em `$1`. Dentro da consulta real, mês e identidade já ocupam posições anteriores.

Os parênteses preservam o significado dos grupos. Sem eles, a combinação de `AND` e `OR` poderia produzir outro resultado.

Há limite de profundidade configurado como 5, com a chamada inicial na profundidade 0, e limite de 50 condições. Um grupo vazio retorna `TRUE` neste código, inclusive se o operador for `or`; não presuma outra regra sem olhar a implementação.

## 5. condition valida antes de montar a expressão

O caminho de uma condição é:

```text
Conta a condição
  → procura definição no snapshot
  → confere Filterable
  → confere operador permitido para o tipo
  → obtém fieldExpr
  → interpreta o valor
  → gera expressão SQL e adiciona argumentos
```

Um campo inexistente e um campo conhecido com `Filterable == false` são rejeitados. O código não tenta descobrir o tipo pelo conteúdo do valor recebido.

## 6. A tabela de operadores

| Tipo | Operadores disponíveis no exemplo |
|---|---|
| category | `eq`, `neq`, `in`, `not_in`, `is_null`, `not_null` |
| text | Os de category e `contains`. |
| number e date | `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `between`, `is_null`, `not_null` |
| boolean | `eq`, `is_null`, `not_null` |

`operatorsByType` é um mapa de mapas. O primeiro acesso seleciona o tipo; o segundo informa se um operador está permitido.

`comparison` traduz nomes conhecidos como `gt` para `>`. O operador recebido não é concatenado livremente como SQL.

## 7. Como o valor é interpretado

`decodeScalar` usa a definição:

| Tipo da definição | Valor Go produzido em casos válidos |
|---|---|
| number | `float64` |
| boolean | `bool` |
| date | `time.Time`, após interpretar `AAAA-MM-DD` |
| text ou category | `string`; códigos numéricos também podem ser representados como texto por `looseString` |

Um texto como `"muito"` não passa como número. Uma data em formato `"01/02/2024"` também não passa no formato esperado.

`decodeStringList` prepara listas para operadores de categoria e texto. Ela usa `looseString` para aceitar, por exemplo, tanto `["A", "B"]` quanto códigos numéricos.

## 8. Os caminhos de cada operador

### Comparações

`eq`, `gt` e outros usam expressão, operador conhecido e valor parametrizado. `neq` usa `IS DISTINCT FROM`, cuja comparação considera a diferença envolvendo SQL `NULL`.

### Ausência

`is_null` e `not_null` geram `IS NULL` e `IS NOT NULL`. Eles não precisam de um valor de comparação.

### Listas

`in` usa `= ANY(...::text[])`. A lista é enviada como um argumento.

`not_in` usa `NOT COALESCE(..., false)`. O `COALESCE` define o comportamento de valores ausentes; nesse exemplo, esses registros continuam no resultado da exclusão.

### Intervalos

`between` exige um array com exatamente dois itens. Cada um passa pela interpretação de tipo. O SQL usa `BETWEEN`, que inclui os dois extremos.

### Texto contido

`contains` exige texto não vazio e usa `ILIKE` com curingas ao redor do argumento. `escapeLike` protege os caracteres que teriam significado especial na busca: porcentagem, sublinhado e barra invertida.

Isso é diferente de parametrizar a consulta. Parametrização separa dados de SQL; escapar o padrão impede que um caractere de busca seja interpretado como curinga.

## 9. ValidateFilter reutiliza o compilador

```go
_, err := compileFilter(cat, n, &args{})
return err
```

A função valida tentando produzir uma expressão. Ela não executa a consulta de funcionários nem salva o filtro.

A rota de validação precisa obter o catálogo antes. Se o cache estiver vazio ou vencido, essa obtenção pode consultar as tabelas de definições. Portanto “validar sem consultar funcionários” não significa “a requisição inteira nunca acessa o banco”.

Não há uma implementação de gravação em `saved_filters` neste projeto; os comentários indicam um possível consumidor.

## 10. Um limite real: null no valor do filtro

Há uma diferença entre testar ausência de um campo e enviar `"value": null` numa comparação.

No código atual, `json.Unmarshal` em variáveis escalares não ponteiro pode aceitar `null` e deixar o valor zero. Assim:

- número com `null` pode virar `0`;
- booleano com `null` pode virar `false`;
- texto com `null` pode virar string vazia.

Isso é uma limitação da validação deste exemplo, não uma regra de negócio recomendada. Para expressar ausência no fluxo ensinado aqui, use os operadores dedicados. Os testes existentes não demonstram uma rejeição geral desse caso.

## Confira se entendeu

1. Por que `Value` começa como JSON bruto? Porque o tipo vem da definição.
2. Onde a busca rápida por chave aparece? Em `c.cat.Attribute(n.Field)`.
3. O cliente pode escolher qualquer operador SQL? Não; há uma lista por tipo.
4. Validar um filtro já salva alguma coisa? Não.
5. Um filtro enviado pelo cliente controla o mês e a visibilidade? Não; esses recortes são montados separadamente pelo repository.

[Anterior: campos e SQL](03-campos-dinamicos-e-sql.md) · [Índice](README.md) · [Próximo: repository](05-repository-e-consultas.md)
