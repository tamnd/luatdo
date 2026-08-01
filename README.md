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
luatdo subjects
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
| `luatdo subjects` | File every document under the subject vocabulary, no model involved |
| `luatdo sample` | Draw a reproducible sample stratified over subject and instrument type |
| `luatdo terms` | Extract defined terms from interpretation articles, no model involved |
| `luatdo concepts` | Read definitions into term uses, cluster them, merge by human decision |
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

## A term is not a concept

The phrase `người lao động` appears in the Labour Code, in the Social Insurance Law and in several hundred provincial decisions, and they do not all mean the same thing by it.
So the concept layer has two node types and the difference between them is the whole design.
A `TermUse` is a term as one instrument defines and uses it, its identity is the scope plus the term, and it is the only thing a reading pass is allowed to create.
A `Concept` is corpus wide and comes into existence only because a person decided that two term uses are the same thing and wrote down why.

```text
vn:term:vn:law:2019:45-2019-qh14:nguoi-lao-dong
vn:concept:nguoi-lao-dong
```

The merge is three steps and each is done by whoever is good at it.
Code proposes: same slug, shared aliases, overlapping genus, clustered star wise around the lexicographically first member so k readings cost k-1 questions instead of k squared.
A model compares the two definitions and says what it thinks, as advice, recorded on the question.
A person answers `same`, `broader`, `narrower`, `differs` or `defer`, and the answer is refused without a written rationale.
`INSTANCE_OF` carries the decider, the timestamp and the reason, so a merge made a year ago can be argued with.

`DIFFERS_FROM` is a first class edge, not an error state.
Two instruments using one phrase for two things is one of the most useful facts this graph can hold, and a pipeline that merges by string match destroys it silently.
A difference carries the specific differentiae the two readings disagree on, because a difference with no stated basis is an opinion.

The reading itself is genus and differentiae, kind, aliases, and three things that are usually got wrong.
A term naming a position rather than an organisation is asked as a direct question while the clause is in view, never inferred from the label afterwards, because `cơ quan có thẩm quyền` resolves per document and never to one ministry.
A definition that points at another instrument is stored as a pointer with the quote that makes it one, and the definition field stays empty, because paraphrasing a document nobody was shown is an unfalsifiable claim.
A definition that lists its subtypes keeps the list, because that is the definition.
Every quote is checked byte for byte at the offsets it claims, and a reading that fails is rejected rather than logged.

The kind enum is closed and it started at eight.
Annotating two hundred real definition clauses by hand, before running anything over them, showed that most of the corpus does not define subjects and acts.
It defines chemicals, cables, vehicles, software, forests and water, it defines periods and deadlines, and it defines standards and methods, so `thing`, `time` and `rule` were added and `other` was added with them.
`other` is a residual and its share is a measurement: it is 5 percent of the gold set, and if it grows the enum is wrong again.

The gold set is 200 clauses drawn by seed and stratified over document type, annotated by hand before the pipeline existed to have an opinion about them.
That order is the point, since an annotation written next to a prediction measures agreement with the prediction rather than accuracy.
They yield 198 defined terms, 5 clauses that define nothing at all, 26 role terms, 2 definitions by reference, 15 enumerations, and 8 hand decided merge pairs of which 4 are differences.
The gold set is itself checked before it is used to score anything, because a typo in a kind name there would score every correct reading as wrong and make the pipeline look broken instead of the ruler.

```sh
luatdo concepts sample -n 200 -seed m7   # draw, then annotate by hand
luatdo concepts read                     # the model reads, code checks every quote
luatdo concepts cluster --compare        # code proposes, the model advises
luatdo concepts queue                    # what is waiting on a person
luatdo concepts answer -by tamnd <a> <b> same "same definiens, both scoped to the 2019 code"
luatdo concepts build                    # invariants are a build failure, not a warning
luatdo concepts score                    # against the gold set
```

## An article and what it says

An article of a code outlives the words in it.
Article 94 of the Labour Code is still article 94 after an amending law rewrites it, and a norm read out of the 2021 wording was never stated by the 2019 wording, so the two facts cannot live on one node.
The structural node is a `Component` and the words are a `TextVersion`, one per wording, dated from the day it took effect.

The rule for which one to point at is short.
Citations point at the component, because a law that amends article 94 amends the article rather than one of its wordings.
Norms, defined terms and extracted relations point at the text, because they are readings of particular words.
A component with no text of its own, a chapter for instance, has no version at all rather than a version saying nothing.

The projection carries `Component` and `Provision` as labels on the same node, so a query written against the earlier shape still runs.
The alias ships through v0.1.0 and is dropped after it, and the drift check counts both labels and fails if they ever disagree.

```cypher
// What article 94 says today, and what it said before
MATCH (c:Component {id:'vn:law:2019:45-2019-qh14:article-94'})-[:HAS_VERSION]->(v:TextVersion)
RETURN v.from_date, v.to_date, v.text ORDER BY v.from_date
```

## What a document is about

There is a subject vocabulary of 24 domains and 144 subdomains, hand written once against Vietnamese practice and shaped after EuroVoc.
It is not an ontology and nothing downstream reasons over it.
It has two uses: getting around a corpus of a hundred and twenty eight thousand documents, and drawing samples that are not all provincial land decisions.

`luatdo subjects` files every document by matching cue phrases against its title, type and issuing body, and records the method it used on each assignment.
A document lands in at most three subdomains and carries the domain of each up with it.
The design calls for a distilled classifier trained on model labelled seed data, which is the standard way to get a cheap multi-label classifier over a large corpus, and that is not what ships here: this pass is lexical, and the assignments say `lexical` so nobody has to guess later.
Measured against 65 corpus documents drawn at random and filed by hand, it recalls 0.91, and the six it misses are in the test table rather than relabelled out of it.

Of 112,982 documents that are not quarantined it files 96,368 and leaves 16,614 under nothing, and it uses every domain and every subdomain in the vocabulary.
The 16,614 are not a failure to route around: a title like `Quyết định về việc bãi bỏ Quyết định số 12/2004/QĐ-UB` says nothing about any subject, and a classifier that guessed one would be inventing navigation.
They get a stratum of their own in the sample, because the documents nothing could read are the ones worth reading.

`luatdo sample` draws from those assignments, stratified over subject crossed with instrument type.
There is no random source in it.
Each document is ranked inside its stratum by a hash of the seed and its identifier, so two machines agree on what a seed produces, and a corpus that gained a document keeps almost every pick it had.
The corpus falls into 966 strata, so a draw smaller than that reaches only the largest ones, and the command says so rather than letting the number look like coverage.

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
