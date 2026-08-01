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

The breadth corpus is published as three configs, fetched one at a time:

```sh
luatdo fetch --config metadata th1nhng0        # 171k rows, titles, numbers, dates
luatdo fetch --config relationships th1nhng0   # 1M official citation and amendment edges
luatdo fetch --config content th1nhng0         # the full text, 3.5 GB
luatdo parse --dataset th1nhng0
```

Metadata alone is already worth having: every document becomes a node and the official relationship graph gets its edges.
A document whose text has not been downloaded is marked text pending rather than left out, because an edge pointing at a document that is not there is worse than a document that is honestly incomplete.

The 171,556 metadata rows are not 171,556 documents, and `luatdo parse` says what it did with every row it did not take: numbers with no year and so no stable identifier, local numbers with no issuing body, English translations that carry the number of the document they translate, and rows that duplicate a document already taken.
What comes out is about 128,000 documents.

## Quick start

```sh
go install github.com/tamnd/luatdo/cmd/luatdo@latest

docker run -d --name luatdo-neo4j -p 7474:7474 -p 7687:7687 \
  -e NEO4J_AUTH=neo4j/luatdo-dev neo4j:5

luatdo fetch uts_vlc
luatdo parse
luatdo cite
luatdo anchor
luatdo terms
luatdo ontology init
luatdo export neo4j
```

Then open http://localhost:7474 and ask the graph something:

```cypher
// Amendment history of the 2019 Labor Code
MATCH path = (d:Document {id:'vn:law:2019:45-2019-qh14'})
             <-[:AMENDS*0..]-(later:Document)
RETURN path
```

## Commands

| Command | What it does |
| --- | --- |
| `luatdo fetch` | Download a pinned dataset revision into the immutable raw store |
| `luatdo parse` | Parse raw documents into the canonical model with stable structural IDs |
| `luatdo cite` | Resolve citations and amendment links from official metadata and in-text patterns |
| `luatdo anchor` | Locate definitions articles, split them into units, harvest declared aliases |
| `luatdo terms` | Extract defined terms from interpretation articles, no model involved |
| `luatdo ontology` | Manage the versioned class and predicate registry and its candidates queue |
| `luatdo extract` | Schema-constrained LLM extraction of entity mentions under the closed registry |
| `luatdo link` | Resolve mentions against the registry and the defined term table, with scores |
| `luatdo norms` | Extract norm statements and verify them with the entailment judge |
| `luatdo prompt` | Print the exact prompt for a provision without calling a model |
| `luatdo review` | Work the human review queue for gated statements |
| `luatdo build` | Assemble verified statements into the trusted store |
| `luatdo export neo4j` | Project the trusted store into Neo4j, full import or incremental merge |
| `luatdo coverage` | Report what is parsed, extracted, verified, and exported, recomputed from disk |
| `luatdo run` | The campaign: work the coverage queue with parallel workers until it is empty |
| `luatdo doctor` | Probe model routes and report which are alive, and what the store holds |

Identifiers are structural and reproducible, never generated:

```text
vn:law:2019:45-2019-qh14
vn:law:2019:45-2019-qh14:article-94
vn:law:2019:45-2019-qh14:article-94:clause-1
```

A number issued centrally names one instrument and is an identity on its own.
A number issued by a province is not: every province issues its own `01/2024/QĐ-UBND`, so the identifier of a local document carries the body that signed it, and text citing that number without naming a province stays unresolved rather than being pointed at whichever province was loaded last.

```text
vn:law:2024:01-2024-qd-ubnd:ubnd-tinh-long-an
```

## Where definitions live

A Vietnamese law states its vocabulary in one article, usually `Điều 3. Giải thích từ ngữ`, and usually under a sentence saying which instrument that vocabulary belongs to.
`luatdo anchor` finds those articles, splits them into one unit per clause, and hands the units on with exact spans.
It decides nothing about what any of them mean.

The line it will not cross is the split between the defined term and its definition.
That split is the reading, and a substring taken before the connective is a substring rather than a term, so the unit stays whole and the concept pass takes it from there.
What a grammar can get exactly is taken by code and nothing else is: article headings, clause boundaries, scoping formulas and declared short forms are exact, and the phrase a short form abbreviates is not.

Scope is the part that is easy to get wrong.
A provincial decision is three articles of housekeeping with the entire substance travelling underneath it as an annex, and a term defined in a `Quy chế` issued under a decision is scoped to the Quy chế rather than to the decision.
Flattening the two would claim a definition the decision never made, so the annex keeps its own scope and its own article numbering.

Of the 104,674 documents that carry text, 7,207 have a definitions article, and those yield 49,343 definition units under 7,227 scopes, 3,193 of which are annexes.
Alias declarations are harvested corpus wide rather than only inside definitions articles, because a drafter declares one wherever the phrase first appears: 37,431 of them, split about evenly between `sau đây gọi tắt là`, `sau đây gọi chung là` and `sau đây gọi là`.
The 97,467 documents with text and no definitions article are listed by identifier in `<data>/anchor/unanchored.txt`, because a residue described is a residue nobody can check.

## Running a campaign

A campaign is a long job against a metered service, so it is built to be interrupted.
The queue is recomputed from disk on every run, work is committed one provision at a time, and a provision that fails leaves no artifact, which is exactly what puts it back in the queue next time.

Model access is a routes file: named endpoints in rank order, each with its own credential and its own rate card.

```sh
luatdo doctor --suggest-routes > ~/.config/luatdo/routes.json
luatdo doctor
luatdo run --parallel auto
```

Failover is per call and matched to the cause.
A quota error cools that route for five minutes, a transport blip for thirty seconds, and an authentication failure disables it for the process because retrying a bad credential is pointless.
Every call records which endpoint served it, so a corpus assembled from three endpoints can still say where each statement came from.

Each provision reports what it cost:

```text
norms vn:law:2019:45-2019-qh14:article-94:clause-1 route=subscription statements=3 entailed=3 review=1 time=6s tokens=4812 cost=$0.0141
campaign: 412 done, 0 failed, 0 skipped of 412 queued, 1180 statements, 1094 entailed, 143 in review, 2137440 tokens, cost $6.2841, 21m14s
```

A route with no rate card reports its cost as unavailable, and any total that includes it is unavailable too.
An invented zero would quietly understate a campaign, so nothing invents one.

The first interrupt drains: no new provision starts, the ones in flight finish and are written, and the accounting reports what actually ran.
A second interrupt aborts.
Every run writes a summary to `<data>/campaign/`, and `luatdo coverage --missing` prints what is left.

## Deployment

The binary is static and the state is one data directory, so a host is a copy and a timer.
`deploy/` carries a systemd unit and timer for Linux and a scheduled task script for Windows, both guarded by `luatdo doctor` so a campaign never starts when no route is alive.
See [deploy/README.md](deploy/README.md).

## Status

Early.
The milestone plan is tracked in the issues, one issue per milestone.
M1 is the document and citation graph, M2 adds definitions and entities, M3 adds norms with verification, M4 scales the campaign to the full corpus, and M5 onwards build the concept layer on top of it.

## Development

```sh
make test
make lint
make build
```

Tests use local HTTP servers and scripted model responses.
They need no credentials and no network.

Linux, macOS, and Windows are all first class.
CI runs the test suite on Linux, macOS, and Windows, releases ship binaries for all three, and the export directory carries both `import.sh` and `import.cmd`.

## License

MIT
