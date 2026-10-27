# 06 — HTTP e um fluxo completo

Código: [server.go](../internal/httpapi/server.go). A consulta de busca está em [repository.go](../internal/analytics/repository.go).

## 1. O que é um handler

Um handler é uma função que recebe uma requisição HTTP e escreve uma resposta. Ela tem acesso ao endereço, aos headers, ao corpo e ao contexto da requisição.

O `Server` guarda um repository e o catálogo. `Routes` cria um `http.ServeMux`, associa método e caminho a handlers e devolve o conjunto envolvido por `withViewer`.

Por exemplo:

```go
mux.HandleFunc("POST /api/employees/search", s.searchEmployees)
```

O handler cuida da comunicação. O repository cuida da consulta.

## 2. Middleware: uma etapa antes do handler

`withViewer` envolve o roteador. Para cada requisição:

1. Lê o header `X-User-Login`.
2. Cria um `analytics.Viewer`.
3. Coloca esse valor em um novo contexto derivado da requisição.
4. Encaminha a requisição para o próximo handler.

A função `viewer(r)` recupera o valor do contexto. O tipo privado `viewerKey` é a chave usada para guardá-lo.

Esse exemplo apenas lê uma identidade declarada no header. Ele não verifica senha, token ou sessão e não rejeita globalmente um header vazio. A exigência de login nas consultas que usam `scope` ocorre depois, no repository.

## 3. decodeScope interpreta o corpo comum

Vários endpoints recebem `month` e `filter`. A struct `scopeRequest` reúne esses campos.

`decodeScope`:

1. Limita o corpo lido a 1 MiB com `http.MaxBytesReader`.
2. Decodifica o JSON na struct recebida.
3. Resolve a competência com `ResolveMonth`.
4. Devolve o mês e um booleano indicando sucesso.

Em falha, já escreve a resposta adequada e o handler encerra com `return`. Isso evita continuar executando uma consulta depois de responder erro.

O decodificador não está configurado com `DisallowUnknownFields`. Portanto não é uma validação que rejeita automaticamente qualquer chave extra do JSON.

## 4. Por que searchEmployees usa uma struct anônima

A requisição de busca combina os campos comuns com ordenação e paginação:

```go
var req struct {
    scopeRequest
    SortBy   string
    SortDesc bool
    Limit    int
    Offset   int
}
```

O arquivo original contém as tags JSON. O campo embutido `scopeRequest` permite que `month` e `filter` fiquem no mesmo nível dos outros campos do corpo.

Depois de decodificar, o handler cria um `analytics.SearchParams` e chama o repository. O tipo de entrada HTTP e o tipo de parâmetros da consulta cumprem papéis diferentes, mesmo compartilhando informações.

## 5. Acompanhe uma busca do começo ao fim

Imagine este pedido, com um catálogo que contém as definições citadas:

```http
POST /api/employees/search
Content-Type: application/json
X-User-Login: ana
```

```json
{
  "month": "2026-03",
  "filter": {
    "op": "and",
    "conditions": [
      { "field": "department", "operator": "eq", "value": "Comercial" },
      { "field": "tenure_months", "operator": "gt", "value": 12 }
    ]
  },
  "sortBy": "salario_atual",
  "sortDesc": true,
  "limit": 20,
  "offset": 0
}
```

### Passo A — Entrar no servidor

O roteador reconhece o método e o caminho. `withViewer` já colocou o login `ana` no contexto.

### Passo B — Interpretar a entrada

`searchEmployees` chama `decodeScope`. O mês explícito é convertido para o primeiro dia de março. Esse caminho não precisa procurar o último mês no banco.

### Passo C — Preparar a consulta

`SearchEmployees` chama `scope`. Ele exige login e pede o catálogo com `Get`.

Se o snapshot estiver válido, recebe a versão atual em memória. Se precisar recarregar, o catálogo lê as duas tabelas de definições.

### Passo D — Compilar os filtros e a ordenação

As duas condições consultam o índice de atributos. Seus campos fixos viram expressões permitidas.

A ordenação por `salario_atual` passa por `fieldExpr`, que produz a expressão numérica para a chave JSON.

Os argumentos ficam assim:

| Posição | Valor ilustrativo |
|---|---|
| `$1` | Março de 2026 normalizado como `time.Time`. |
| `$2` | `"ana"` |
| `$3` | `"Comercial"` |
| `$4` | `12`, interpretado como número. |
| `$5` | `"salario_atual"`, usado na expressão de ordenação. |
| `$6` | `20`, limite. |
| `$7` | `0`, offset. |

### Passo E — Consultar os dados

O repository executa a consulta em `employee_month`, lê as linhas e monta `Page[EmployeeRow]`.

Mesmo com cache válido, esta consulta de dados acontece. O cache economizou as leituras das definições.

### Passo F — Enviar a resposta

O handler recebe a página e chama `writeJSON`. A resposta usa `Content-Type: application/json`; sem outro status explícito no caminho de sucesso, a resposta é HTTP 200.

Os campos exportados e as tags dos tipos determinam os nomes no JSON.

## 6. Quantas consultas acontecem nesse exemplo

Para a busca acima, considerando execução bem-sucedida:

| Estado | Consultas de catálogo | Consultas de funcionários |
|---|---|---|
| Cache já válido | 0 | 1 |
| Cache vazio ou vencido, recarga bem-sucedida | 2 | 1 |

Se o mês estiver vazio, acrescente a consulta de `ResolveMonth`. Essas contagens não se aplicam a endpoints que montam respostas com várias consultas, como o detalhe.

## 7. Os fluxos curtos do catálogo

```text
GET /api/catalog
  → Server.catalog
  → Repo.Catalog
  → Catalog.Get
  → Snapshot
  → JSON com attributes e factors
```

O caminho de invalidação é diferente:

```text
POST /internal/catalog/reload
  → Server.reloadCatalog
  → Catalog.Invalidate
  → HTTP 204 sem corpo
```

A invalidação não passa pelo carregamento do catálogo naquele instante. Também não há neste projeto uma chamada automática dessa rota depois de um processamento externo.

## 8. Como os erros viram HTTP

`fail` pergunta a `analytics.IsClientError` se o erro é conhecido.

| Erro | Status |
|---|---|
| `ErrInvalidFilter`, `ErrInvalidField`, `ErrInvalidParam` | 400 |
| `ErrNotFound` | 404 |
| Demais falhas, como erro inesperado do banco | 500 |

Os erros usam `fmt.Errorf` com `%w`, que preserva a causa. `errors.Is` consegue reconhecê-la mesmo depois de acrescentar contexto.

Para erros internos, o servidor registra detalhes com `slog.Error` e envia uma mensagem genérica. Para erros de cliente, envia a mensagem descritiva retornada.

JSON inválido é tratado diretamente em pontos de decodificação. A rota de validação responde 204 quando o filtro é aceito.

## 9. Como seguir uma dúvida no editor

- Não entendeu um campo do request: comece pelo handler e suas tags.
- O mês veio diferente: siga para `ResolveMonth`.
- A chave não é aceita: confira `Snapshot.Attribute`, `Filterable` e `viewColumns`.
- O SQL usa o tipo errado: confira a definição e `fieldExpr`.
- O resultado tem outro recorte: veja `scope` e a consulta específica.
- O catálogo parece antigo: veja `loadedAt`, TTL, invalidação e fallback em caso de erro.

## Confira se entendeu

Explique o caminho da busca sem usar “o servidor faz tudo”. Cite quem decodifica, quem resolve o mês, quem fornece as definições, quem compila, quem consulta e quem escreve a resposta.

Depois compare com a rota de invalidação: ela precisa executar quais dessas etapas? Apenas localizar o handler, invalidar o catálogo e responder; a carga fica para uma utilização posterior.

[Anterior: repository](05-repository-e-consultas.md) · [Índice](README.md) · [Próximo: testes e exercícios](07-testes-e-exercicios.md)
