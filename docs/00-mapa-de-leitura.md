# 00 — Mapa de leitura e localização dos arquivos

Use este arquivo como um roteiro com duas direções: localizar o código a partir de uma dúvida e voltar à explicação correspondente.

## Primeiro, veja o desenho geral

```text
cmd/api/main.go
  cria pool → cria Catalog → cria Repo → cria Server
                                           |
                                   requisição HTTP
                                           |
                             internal/httpapi/server.go
                                           |
                          internal/analytics/repository.go
                             |                     |
                         Catalog.Get          compileFilter
                             |                     |
                         Snapshot              fieldExpr
                                                   |
                                         SQL + argumentos
                                                   |
                                              PostgreSQL
```

O repository usa o catálogo para interpretar campos, mas continua consultando os dados dos funcionários no banco. Um acerto no cache não elimina todas as consultas da requisição.

## Arquivos do código, um por vez

| Arquivo | Procure estes símbolos | Responsabilidade | Capítulo |
|---|---|---|---|
| [go.mod](../go.mod) | `module`, `go`, `require` | Nome interno do módulo, versão Go e dependência do driver PostgreSQL. | [Fundamentos](01-fundamentos-e-inicializacao.md) |
| [main.go](../cmd/api/main.go) | `main`, `NewCatalog`, `NewRepo`, `NewServer` | Montar os objetos e iniciar o servidor. | [Inicialização](01-fundamentos-e-inicializacao.md) |
| [catalog.go](../internal/analytics/catalog.go) | `AttributeDef`, `RiskFactor`, `Snapshot`, `NewSnapshot` | Representar e indexar as definições do catálogo. | [Catálogo](02-catalogo-snapshot-e-cache.md) |
| [catalog.go](../internal/analytics/catalog.go) | `Catalog`, `Get`, `Invalidate`, `loadSnapshot` | Guardar a versão atual e decidir quando recarregar. | [Cache](02-catalogo-snapshot-e-cache.md) |
| [fields.go](../internal/analytics/fields.go) | `args.add`, `viewColumns`, `fieldExpr`, `sqlType` | Traduzir campo conhecido para expressão SQL e acumular parâmetros. | [Campos e SQL](03-campos-dinamicos-e-sql.md) |
| [filter.go](../internal/analytics/filter.go) | `FilterNode`, `operatorsByType`, `compileFilter`, `condition` | Validar a árvore de filtros e gerar condições. | [Filtros](04-filtros-passo-a-passo.md) |
| [filter.go](../internal/analytics/filter.go) | `decodeScalar`, `decodeStringList`, `looseString`, `escapeLike` | Interpretar valores e preparar buscas textuais. | [Filtros](04-filtros-passo-a-passo.md) |
| [repository.go](../internal/analytics/repository.go) | `scope`, `ResolveMonth`, `SearchEmployees`, `scanEmployee` | Aplicar o recorte e executar a listagem. | [Repository](05-repository-e-consultas.md) |
| [repository.go](../internal/analytics/repository.go) | `Overview`, `Breakdown`, `FactorsSummary`, `FactorScatter`, `EmployeeDetail` | Reutilizar as peças em outras consultas. | [Outras consultas](05-repository-e-consultas.md) |
| [server.go](../internal/httpapi/server.go) | `Routes`, `withViewer`, `decodeScope`, `searchEmployees`, `fail` | Receber HTTP, decodificar JSON e enviar respostas. | [HTTP](06-http-e-fluxo-completo.md) |
| [filter_test.go](../internal/analytics/filter_test.go) | `testCatalog`, `TestCompileNestedFilter`, `TestRejectsInvalidFilters` | Exercitar a compilação sem banco. | [Testes](07-testes-e-exercicios.md) |
| [repository_integration_test.go](../internal/analytics/repository_integration_test.go) | `integrationRepo`, funções `TestIntegration...` | Conferir consultas com banco e dados preparados. | [Testes](07-testes-e-exercicios.md) |

O nome interno de módulo que aparece nos imports foi preservado do exemplo original. Para navegar, use os caminhos relativos acima; eles continuam válidos mesmo se a pasta ou o repositório tiver outro nome.

## Em qual ordem ler dentro de cada arquivo

### Catálogo

`AttributeDef` e `RiskFactor` → `Snapshot` → `NewSnapshot` → `Attribute` e `Factor` → `Catalog` → `NewCatalog` → `Get` → `loadSnapshot` → `Invalidate`.

Primeiro entenda o objeto guardado; depois entenda quem controla sua validade.

### Campos e filtros

`args` → `viewColumns` → `fieldExpr` → `FilterNode` → `compileFilter` → `node` → `condition` → funções de decodificação.

Não tente entender a recursão e o cast de JSONB ao mesmo tempo. São peças diferentes que se encontram em `condition`.

### Consulta de listagem

`SearchParams` e `EmployeeRow` → `scope` → `SearchEmployees` → `scanEmployee`.

Anote separadamente a string SQL e o conteúdo de `a.vals`.

### HTTP

`Routes` → `withViewer` → `searchEmployees` → `decodeScope` → chamada ao repository → `writeJSON` ou `fail`.

O handler de busca é suficiente para reconhecer o padrão usado nos demais handlers.

## Se a dúvida for específica

| Dúvida | Abra |
|---|---|
| Por que há lista e mapa com as mesmas definições? | [Catálogo e snapshot](02-catalogo-snapshot-e-cache.md) |
| Quem segura o objeto na memória? | [Catálogo e ponteiros](02-catalogo-snapshot-e-cache.md) |
| O TTL apaga alguma coisa? | [Validade e recarga](02-catalogo-snapshot-e-cache.md) |
| A API consulta o banco em toda busca? | [Cache](02-catalogo-snapshot-e-cache.md) e [repository](05-repository-e-consultas.md) |
| De onde vem a chave de um atributo? | [Campos dinâmicos](03-campos-dinamicos-e-sql.md) |
| Por que aparece `$3` no SQL? | [Parâmetros SQL](03-campos-dinamicos-e-sql.md) |
| Como `AND` e `OR` viram parênteses? | [Filtros](04-filtros-passo-a-passo.md) |
| Como a identidade limita os registros? | [Repository](05-repository-e-consultas.md) e [HTTP](06-http-e-fluxo-completo.md) |
| Onde olhar quando recebo 400, 404 ou 500? | [Erros HTTP](06-http-e-fluxo-completo.md) |
| Como estudar sem ter um PostgreSQL preparado? | [Testes e exercícios](07-testes-e-exercicios.md) |

[Índice](README.md) · [Próximo: fundamentos e inicialização](01-fundamentos-e-inicializacao.md)
