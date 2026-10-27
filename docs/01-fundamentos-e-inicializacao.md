# 01 — Fundamentos de Go e inicialização

Código principal: [main.go](../cmd/api/main.go). Para reconhecer os tipos, mantenha [catalog.go](../internal/analytics/catalog.go) aberto ao lado.

## 1. O problema que o programa resolve

Uma API recebe uma pergunta, consulta dados e devolve uma resposta. Neste exemplo, algumas propriedades dos registros são fixas e outras ficam em um objeto JSON. O catálogo descreve como interpretar essas propriedades.

As peças são:

- **Server:** entende HTTP e JSON.
- **Repo:** monta e executa consultas.
- **Catalog:** fornece as definições e administra o cache.
- **Snapshot:** contém uma versão das definições.
- **Pool PostgreSQL:** permite que as consultas usem conexões com o banco.

Separar essas responsabilidades permite estudar ou alterar uma delas sem colocar toda a lógica dentro de um handler.

## 2. Struct: agrupar campos relacionados

```go
type AttributeDef struct {
    Key      string
    DataType DataType
    Unit     *string
}
```

Uma struct é um tipo com campos nomeados. Um valor desse tipo representa uma definição, por exemplo: chave `salario_atual`, tipo `number` e unidade `currency`.

`type DataType string` cria um tipo nomeado cujo tipo subjacente é string. As constantes `TypeNumber`, `TypeText` e outras ajudam a escrever a intenção. Isso não transforma qualquer string recebida do banco em um valor automaticamente validado.

## 3. Slice: guardar uma sequência

```go
attrs := []AttributeDef{
    {Key: "department", DataType: TypeCategory},
    {Key: "salario_atual", DataType: TypeNumber},
}
```

`[]AttributeDef` é um slice: uma sequência de valores `AttributeDef`. Você pode percorrê-lo com `for`, acessar posições e acrescentar itens com `append`.

Slices mantêm a ordem dos elementos. Eles são úteis para devolver o catálogo na ordem de exibição recebida da consulta.

## 4. Map: encontrar algo por chave

```go
byKey := make(map[string]AttributeDef)
byKey["salario_atual"] = attrs[1]

def, ok := byKey["salario_atual"]
```

O mapa associa uma chave a um valor. `ok` informa se a chave existe; sem esse booleano, uma chave ausente produziria o valor zero do tipo e poderia ser confundida com uma definição encontrada.

O capítulo do catálogo explica o custo da busca e por que o exemplo mantém slices e mapas ao mesmo tempo.

## 5. Ponteiro: referenciar um objeto existente

```go
snap *Snapshot
```

`*Snapshot` é o tipo de um ponteiro para `Snapshot`. A expressão `&Snapshot{...}` cria um valor e obtém seu endereço.

Em vez de copiar todos os dados do catálogo para cada requisição, o código entrega uma referência à mesma versão. O compilador e o runtime cuidam da alocação e da vida útil; o programador não chama uma função para liberar esse objeto manualmente.

Um ponteiro pode ser `nil`: ainda não aponta para objeto algum. Antes da primeira carga, `Catalog.snap` está assim.

## 6. Construtor: uma função que prepara o objeto

```go
func NewCatalog(db *pgxpool.Pool, ttl time.Duration) *Catalog {
    return &Catalog{db: db, ttl: ttl}
}
```

Go não exige construtores especiais. `NewCatalog` é uma função comum, com um nome que comunica criação e preparação.

Aqui ela guarda a referência ao pool e a duração do TTL. **Ela não consulta as tabelas do catálogo.** Isso fica para `Get`, quando os dados forem necessários.

Em `NewSnapshot`, o trabalho de preparação é diferente: guardar as listas recebidas e preencher os índices por chave.

## 7. Método e receiver

```go
func (c *Catalog) Get(ctx context.Context) (*Snapshot, error)
```

A parte `(c *Catalog)` é o receiver: indica a qual tipo o método pertence. Dentro dele, `c` representa o catálogo usado na chamada `cat.Get(ctx)`.

O retorno `(*Snapshot, error)` permite devolver o objeto ou informar uma falha. O padrão frequente é verificar `err != nil` antes de usar o outro retorno.

## 8. Campos públicos, privados e tags JSON

Nomes iniciados por maiúscula, como `Attributes`, podem ser acessados por outros pacotes. Nomes iniciados por minúscula, como `attrByKey`, ficam restritos ao pacote.

A tag `json:"attributes"` informa o nome usado ao serializar um campo exportado. Ela não renomeia o campo no código Go.

Por isso o JSON do snapshot contém `attributes` e `factors`, enquanto os mapas internos não são enviados pelo serializador. Não é o nome da chave do mapa que define a estrutura da resposta HTTP.

## 9. A montagem em main

O núcleo da inicialização é:

```go
cat := analytics.NewCatalog(db, time.Minute)
repo := analytics.NewRepo(db, cat)
srv := httpapi.NewServer(repo, cat)
```

A ordem importa porque cada objeto precisa receber as dependências que usa.

1. `pgxpool.New` cria o pool a partir de `DATABASE_URL`.
2. `NewCatalog` recebe esse pool e o TTL.
3. `NewRepo` recebe o mesmo pool e o mesmo catálogo.
4. `NewServer` recebe o repository e o catálogo.
5. `Routes` prepara as rotas.
6. `http.ListenAndServe` inicia o atendimento no endereço configurado.

O programa usa `:8080` quando `ADDR` não foi definida. O comando para executar está no [README principal](../README.md).

## 10. Por que passar as dependências

`Repo` precisa consultar o banco e consultar definições. Receber `db` e `cat` torna isso explícito.

`Server` recebe o catálogo porque seu handler de invalidação chama `Invalidate`. O repository recebe esse mesmo objeto; portanto a invalidação afeta a versão usada nas consultas daquele processo.

```text
Catalog criado em main
        |
        +---- Repo.cat
        |
        +---- Server.cat

São referências para a mesma instância.
```

Isso costuma ser chamado de injeção de dependências. Aqui é feito com argumentos de funções; não existe um framework organizando os objetos.

## 11. Pool, context e defer

Um pool administra conexões; ele não é uma cópia do banco nem o cache de catálogo. O campo `Catalog.db` permite executar as consultas quando houver uma recarga.

O `context.Context` acompanha a operação. No atendimento HTTP, `r.Context()` é passado até o driver para que cancelamentos e prazos possam ser observados pelas consultas. Não é nele que o catálogo é armazenado.

`defer db.Close()` agenda o fechamento do pool quando a função retornar normalmente. Já `defer c.mu.Unlock()`, no cache, agenda a liberação do mutex ao sair de `Get`. Não são operações feitas imediatamente na linha do `defer`.

A criação dos objetos não comprova que o banco contém as tabelas esperadas; este exemplo não inclui a preparação do schema. Consulte a [documentação de pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool) para aprofundar o papel do pool.

## Confira se entendeu

1. `NewCatalog` já carrega os registros do banco? Não: guarda as dependências.
2. `Repo.cat` e `Server.cat` são dois caches? Não: apontam para a mesma instância.
3. `*Snapshot` é uma lista? Não: é uma referência a uma struct que contém listas e mapas.
4. Um mapa é obrigatório em toda struct? Não: é uma escolha para um padrão de acesso.

[Anterior: mapa](00-mapa-de-leitura.md) · [Índice](README.md) · [Próximo: catálogo, snapshot e cache](02-catalogo-snapshot-e-cache.md)
