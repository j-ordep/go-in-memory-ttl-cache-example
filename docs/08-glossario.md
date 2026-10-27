# 08 — Glossário para acompanhar a leitura

Use este arquivo quando um termo aparecer antes de você estar confortável com ele. Os exemplos apontam para o significado que o termo tem neste projeto.

| Termo | Significado |
|---|---|
| API | Interface pela qual um programa faz pedidos a outro. Aqui usa HTTP e JSON. |
| Argumento | Valor passado para uma função ou consulta. |
| Atributo dinâmico | Campo cujo nome não precisa existir como coluna fixa do registro; neste exemplo, pode estar dentro de JSONB. |
| Cache | Dados guardados para reutilização. Aqui guarda o catálogo dentro do processo. |
| Catálogo | Conjunto de definições que descreve campos e fatores. A struct `Catalog` também administra sua versão em memória. |
| Cast | Conversão de tipo no SQL, como `::numeric`. |
| Chave | Nome usado para localizar um item. A mesma chave conecta uma definição ao valor correspondente. |
| Competência | Mês de referência dos dados. |
| Concorrência | Operações que podem progredir ao mesmo tempo ou se intercalar, como requisições de usuários diferentes. |
| Construtor | Função que cria e prepara um objeto, como `NewSnapshot`. Em Go, não é uma palavra especial da linguagem. |
| Context | Objeto que acompanha a operação, podendo carregar identidade, cancelamento e prazo. |
| Driver | Biblioteca que permite ao programa se comunicar com o banco. Aqui é usado `pgx`. |
| Encapsular | Oferecer operações conhecidas sem exigir que o consumidor manipule a representação interna. |
| Fallback | Caminho alternativo quando uma operação falha. Aqui pode ser usar o snapshot anterior. |
| Field / campo | Propriedade de uma struct, chave de filtro ou coluna, conforme o contexto. |
| Flag | Campo que expressa uma condição ou opção, frequentemente um booleano como `Filterable`. |
| Função de janela | Operação SQL que calcula informação sobre um conjunto sem reduzir cada resultado a um único grupo. Exemplo: `count(*) OVER()`. |
| Garbage collector / GC | Parte do runtime que pode recuperar a memória de objetos que deixaram de ser alcançáveis. |
| Genérico | Tipo ou função parametrizado por tipo. Exemplo: `Page[EmployeeRow]`. |
| Goroutine | Unidade de execução concorrente administrada pelo runtime Go. |
| Handler | Código que atende uma requisição HTTP e escreve a resposta. |
| Hash | Transformação usada por estruturas como mapas para localizar chaves. Não significa criptografia neste contexto. |
| Imutável | Que não é modificado depois de pronto. No snapshot deste exemplo, é uma disciplina de uso, não uma garantia da linguagem. |
| Índice em memória | Estrutura para encontrar itens rapidamente, como `attrByKey`. Não é um índice criado no PostgreSQL. |
| Injeção de dependências | Passar os objetos necessários como argumentos, em vez de criá-los escondidos dentro de quem os usa. |
| In-process | Que fica dentro do processo da aplicação. Cada processo tem sua própria memória. |
| Invalidação | Marcar um dado guardado como vencido para que seja atualizado numa utilização posterior. |
| JSON | Formato de dados com objetos, listas, textos, números, booleanos e null. |
| JSONB | Tipo PostgreSQL para armazenar e consultar dados JSON. |
| Lazy loading | Carregar quando alguém precisa, em vez de carregar antecipadamente. |
| Lookup | Busca de um item, geralmente por uma chave. |
| Map | Estrutura Go que associa chaves a valores. A ordem de percurso não é garantida. |
| Metadado | Informação sobre outro dado: tipo, origem, unidade ou regras de uso, por exemplo. |
| Middleware | Etapa que envolve o handler para executar alguma ação antes ou depois dele. |
| Mutex | Mecanismo de exclusão mútua: permite uma execução por vez na região protegida. |
| nil | Ausência de referência ou de valor em tipos Go que admitem esse estado. Não é o número zero. |
| NULL | Ausência de valor no SQL. Não se comporta como um número zero ou uma string vazia. |
| Offset | Quantidade de linhas a pular numa consulta paginada. |
| O(n) | Forma de expressar um custo que cresce linearmente com a quantidade de itens. |
| O(1) médio | Custo esperado que não cresce linearmente com a quantidade de itens, como se espera de um lookup em mapa. Não é uma promessa de duração fixa. |
| Parâmetro SQL | Posição como `$1` no SQL, com valor enviado separadamente pelo driver. |
| Ponteiro | Valor que referencia outro objeto. `*Snapshot` é um ponteiro para uma struct Snapshot. |
| Pool | Objeto que administra conexões para acesso ao banco. Não é o cache do catálogo. |
| Receiver | Parâmetro que associa um método a um tipo, como `(c *Catalog)`. |
| Repository | Parte do código responsável por acessar os dados e converter os resultados. |
| Runtime | Parte da execução Go que administra recursos como goroutines e coleta de lixo. |
| Serializar | Transformar valores Go em um formato de resposta, como JSON. |
| Slice | Sequência de elementos Go, como `[]AttributeDef`; sua representação referencia um array subjacente. |
| Snapshot | Neste projeto, uma versão do catálogo com listas e mapas preparada para leitura. |
| Stale | Dado cuja validade terminou ou que pode estar desatualizado. |
| Stale-if-error | Reutilizar uma versão antiga se a tentativa de atualizar falhar. |
| Struct | Tipo Go composto por campos nomeados. |
| Tag JSON | Anotação que define detalhes da representação JSON de um campo exportado. |
| TTL | Tempo de validade contado a partir da carga. No exemplo, não é renovado por cada leitura. |
| View | Consulta nomeada no PostgreSQL usada como origem de dados. O arquivo Go não contém sua definição. |
| Whitelist / lista permitida | Conjunto explícito de nomes ou expressões aceitos, como `viewColumns`. |
| Zero value / valor zero | Estado inicial de um tipo Go: por exemplo, 0, false, string vazia ou nil, conforme o tipo. |

## Três confusões comuns

**Struct não é tabela.** Uma struct representa dados em Go; ela pode ser preenchida a partir de uma linha de tabela, mas também pode agrupar listas e estado, como `Snapshot` e `Catalog`.

**Ponteiro não é cache.** Um ponteiro apenas referencia um objeto. O cache surge das regras que guardam e reutilizam esse objeto.

**Map não é obrigatório.** Ele é apropriado quando a aplicação precisa localizar valores por chave. Usar um mapa tem custo de memória e preparação; um slice pode bastar quando o uso é somente percorrer uma lista.

[Índice](README.md) · [Mapa de localização](00-mapa-de-leitura.md) · [Voltar ao README do projeto](../README.md)
