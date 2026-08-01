# luatdo

[![CI](https://github.com/tamnd/luatdo/actions/workflows/ci.yml/badge.svg)](https://github.com/tamnd/luatdo/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/tamnd/luatdo.svg)](https://pkg.go.dev/github.com/tamnd/luatdo)
[![Go Report Card](https://goreportcard.com/badge/github.com/tamnd/luatdo)](https://goreportcard.com/report/github.com/tamnd/luatdo)
[![License](https://img.shields.io/github/license/tamnd/luatdo)](LICENSE)

`luatdo` turns Vietnamese legal documents into a versioned, evidence-grounded knowledge graph and exports it to Neo4j.
The name is luật đồ, a law graph.

The pipeline is deterministic first and generative second.
Document structure, stable identifiers, citations, and amendment links come from parsing and official metadata, with no model involved.
LLM extraction only adds semantic layers on top, and every extracted statement must carry an exact evidence span and pass an entailment check against its source text before it enters the trusted graph.
A statement without evidence does not exist.

## Why

Vietnamese legal text is abundant but unusable as a graph.
Laws are HTML and Markdown blobs where articles, clauses, and points are implicit in formatting conventions.
The same authority or concept appears under a dozen surface forms.
Amendments, repeals, and effective dates make any static graph legally wrong.
And the naive approach, documents into an LLM into arbitrary triples into a database, produces duplicate entities, invented predicates, and legally misleading edges.

`luatdo` is built around three ideas:

1. The document and citation graph is buildable with zero LLM calls and is already useful on its own.
2. Extraction runs under a closed, versioned ontology, so the model can only emit known classes and predicates, and everything else becomes a review candidate instead of a graph fact.
3. Neo4j is a projection, not the source of truth. The trusted store lives on disk as plain files, and the database can be rebuilt from zero at any time.

## Data

| Source | Role |
| --- | --- |
| [UTS_VLC](https://huggingface.co/datasets/undertheseanlp/UTS_VLC) | 306 verified, currently effective laws and codes, cleaned to Markdown. The trusted seed corpus. |
| [th1nhng0/vietnamese-legal-documents](https://huggingface.co/datasets/th1nhng0/vietnamese-legal-documents) | About 171,000 documents from vbpl.vn with official amendment and citation metadata. The breadth corpus. |

Dataset revisions are pinned and raw bytes are immutable once fetched, so every document keeps its provenance.

## Quick start

```sh
go install github.com/tamnd/luatdo/cmd/luatdo@latest

docker run -d --name luatdo-neo4j -p 7474:7474 -p 7687:7687 \
  -e NEO4J_AUTH=neo4j/luatdo-dev neo4j:5

luatdo fetch uts_vlc
luatdo parse
luatdo cite
luatdo export neo4j
```

Then open http://localhost:7474 and ask the graph something:

```cypher
// Amendment history of the 2019 Labor Code
MATCH path = (d:Document {id:'vn:law:2019:45-2019-qh14'})
             <-[:AMENDS|REPLACES*0..]-(later:Document)
RETURN path
```

## Commands

| Command | What it does |
| --- | --- |
| `luatdo fetch` | Download a pinned dataset revision into the immutable raw store |
| `luatdo parse` | Parse raw documents into the canonical model with stable structural IDs |
| `luatdo cite` | Resolve citations and amendment links from official metadata and in-text patterns |
| `luatdo extract` | Schema-constrained LLM extraction of entities and norms |
| `luatdo verify` | Entailment judge over extracted statements |
| `luatdo build` | Assemble verified statements into the trusted store |
| `luatdo export neo4j` | Project the trusted store into Neo4j, full import or incremental merge |
| `luatdo coverage` | Report what is parsed, extracted, verified, and exported, recomputed from disk |
| `luatdo run` | The campaign: work the coverage queue until it is empty |
| `luatdo doctor` | Probe model routes and report which are alive |

Identifiers are structural and reproducible, never generated:

```text
vn:law:2019:45-2019-qh14
vn:law:2019:45-2019-qh14:article-94
vn:law:2019:45-2019-qh14:article-94:clause-1
```

## Status

Early.
The milestone plan is tracked in the issues, one issue per milestone.
M1 is the document and citation graph, M2 adds definitions and entities, M3 adds norms with verification, M4 scales the campaign to the full corpus.

## Development

```sh
make test
make lint
make build
```

Tests use local HTTP servers and scripted model responses.
They need no credentials and no network.

Linux, macOS, and Windows are all first class.
CI runs the test suite on Linux and Windows, releases ship binaries for all three, and the export directory carries both `import.sh` and `import.cmd`.

## License

MIT
