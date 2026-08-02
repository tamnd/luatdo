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

The two datasets overlap and they do not carry the same fields, so an instrument published in both is merged field by field rather than overwritten by whichever pass ran last.
UTS_VLC is cleaned text with no dates in it at all and th1nhng0 carries the commencement date from vbpl.vn, and a parse that let the later run win threw one of those away depending on the order the datasets were fetched in.
The incoming parse wins every field it fills and the record already on disk supplies every field it leaves empty, so the clean text and the date both survive.

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
| `luatdo discover` | Find concepts no article defines, promote them, tag the corpus, link mentions |
| `luatdo relations` | Read concept to concept relations, fold them across the corpus, answer with them |
| `luatdo ontology` | Manage the versioned class and predicate registry and its candidates queue |
| `luatdo extract` | Schema-constrained LLM extraction of entity mentions under the closed registry |
| `luatdo link` | Resolve mentions against the registry and the defined term table, with scores |
| `luatdo norms` | Extract norm statements and verify them with the entailment judge |
| `luatdo ask` | Answer the norm competency questions over the trusted store |
| `luatdo temporal` | Read amending instructions, build the version graph, and answer questions at a date |
| `luatdo prompt` | Print the exact prompt for a provision without calling a model |
| `luatdo review` | Work the human review queue for gated statements |
| `luatdo build` | Assemble verified statements into the trusted store |
| `luatdo export neo4j` | Project the trusted store into Neo4j, full import or incremental merge |
| `luatdo coverage` | Report what is parsed, extracted, verified, and exported, recomputed from disk |
| `luatdo run` | The campaign: work the coverage queue with parallel workers until it is empty |
| `luatdo campaign` | Scope a named campaign and report what it has covered |
| `luatdo eval` | Run the metric suite, validate the judge, and check the release gates |
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

Of the 104,684 documents that carry text, 7,221 have a definitions article, and those yield 49,175 definition units under 7,241 scopes, 3,192 of which are annexes.
Alias declarations are harvested corpus wide rather than only inside definitions articles, because a drafter declares one wherever the phrase first appears: 37,499 of them, split about evenly between `sau đây gọi tắt là`, `sau đây gọi chung là` and `sau đây gọi là`.
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

## The concepts nobody defined

Everything above starts at a definitions article, and most of the corpus does not have one.
7,221 documents of the 104,684 that carry text do, so a concept layer built only from anchored definitions knows the vocabulary of seven percent of the corpus and is silent about the rest.
The phrases the other ninety three percent are made of are concepts too. Nobody wrote a clause saying so.

This is the one pass with no deterministic anchor, and that is not an oversight in the design.
A definitions article can be found by grammar because a drafter marked it.
An undefined concept is a phrase that is load bearing in a provision and looks exactly like a phrase that is not, and the only thing that separates them is reading the sentence.
So the rule that holds everywhere else, that anything a grammar can get exactly is never asked of a model, is satisfied here by there being nothing exact to get.

The pass reads, aggregates, and only then promotes.
Reading is per provision and produces candidates with a verbatim span, checked byte for byte at the offsets claimed, the same fence as the definition pass.
Nothing is promoted off one sighting: a phrase becomes a concept when it appears across enough documents and enough separate instruments, counted apart because forty provisions of one decree are one drafter's habit and four documents from four bodies are a shared vocabulary.
A phrase that some article does define is refused outright, because that concept already exists and a second one under the same label would be the identity bug this whole layer was built to avoid.

A concept with no definition anywhere still needs something written next to it, so the pass synthesises a working definition out of the usages and marks it as ours.
It is a set of claims, each of which has to cite a provision that was actually in the evidence, and a claim citing anything else is dropped rather than kept with a warning.
It is never presented as what the law says. No law says it.
It goes stale when the usages behind it change, and it says so.

Reading 1.9 million provisions with a model is not affordable, so the model reads a stratified sample and a student learns from what it did.
The student is an averaged perceptron over string features with no dependencies, which makes it byte identical on Linux, macOS and Windows, and it tags the rest of the corpus.
It gets scored twice against two different things: agreement with the teacher says it copied the model faithfully, accuracy on the hand annotated gold set says whether either of them was right.
Whichever set it was trained on is scored on its held out part only, and the model file records what it learned from so this holds when the person running the command has forgotten.

Then mentions get linked back.
Inside the instrument that defines a term, code decides and no model is called.
Across instruments code only emits candidates and scores them on signals that are already on disk: the citation graph first, because a document citing another is the strongest evidence in the corpus that it borrowed that vocabulary, then hierarchy, subject overlap and whether the target was in force at all.
A close call goes to the model with both definitions in view.
A mention left unresolved is correct output and is recorded as such, while a confidently wrong link is a defect, because a wrong edge in a knowledge graph is worse than a missing one.

The measurement that matters is a standoff against the grammar only layer, run over the real corpus:

```text
grammar only   23090 terms from 6196 documents
question 6     concepts nobody defined, used in more than 100 provisions
  grammar only 0 answers from 23090 concepts considered, and it cannot ever be more:
               every concept it holds came out of a definitions article
```

That zero is structural rather than a tuning problem.
The concepts in the answer to that question are exactly the ones a definitions driven layer never creates, so no threshold makes the number move.

```sh
luatdo discover sample -n 500 -seed m8   # stratified over instrument type
luatdo discover prompt <provision-id>    # the exact prompt, no model called
luatdo discover read                     # the model reads, code checks every quote
luatdo discover aggregate                # count first, promote second
luatdo discover define                   # working definitions, marked as ours
luatdo discover train -source teacher    # or -source gold, which is a smaller thing
luatdo discover tag                      # the student over the rest of the corpus
luatdo discover score                    # against the teacher and against the gold set
luatdo discover link                     # citation graph first, model only for close calls
luatdo discover compare                  # the standoff against the grammar only layer
```

## What holds between two concepts

A layer of concepts with no edges between them is a glossary.
The thing that makes it a knowledge graph is being able to ask what a building permit requires and get an answer out of the graph rather than out of a search box.

Nothing in this layer can be derived by a deterministic function of the corpus, and that is worth saying plainly because everything else in this project that can be, is.
Vietnamese legal text has no marker meaning "a prerequisite relation follows".
The relation is in the meaning of the sentence and nowhere in its form, so this pass is model driven end to end, and the engineering problem is not finding relations without a model but keeping a model's relations honest.

The pass reads one provision at a time and is handed the concepts that provision already links to, which earlier passes found.
Asking a model to find the concepts and the relations at once is two tasks and it fails at both, so this one is a comprehension task with a bounded candidate set.
Every edge quotes the provision it came from, checked byte for byte at the offsets claimed, and an edge whose endpoints are nowhere near the text it cites does not build.
That last check costs a substring search and it kills the commonest relation hallucination outright.

The seed vocabulary is twelve relations, small on purpose, because a vocabulary fixed large up front is a vocabulary the model spends the corpus forcing reality into.
A model with something to say that none of the twelve covers invents a name for it, and the invented names are folded, defined and only then compared against what already exists.
The comparison is on definitions and never on names, because `cần có trước` and `là điều kiện để được cấp` are two phrases for one relation, and asking whether two verb phrases mean the same thing in the abstract is a question models are unreliable at.
No match is a first class answer and usually the right one.
Nothing invented is promoted without a person, and nothing invented is dropped either, because the tail is where the interesting law is.

There is no `RELATED_TO` in the seed set and none may be promoted into it.
A model under uncertainty reaches for the vaguest relation available, and an edge saying only that two things are connected cannot be queried for anything.
A model with nothing specific to say is supposed to return nothing, and an empty answer is the correct output rather than a failure to extract.

Nothing read out of one provision is canonical, whatever confidence the model attached to it.
One confident sighting is the cheapest hallucination to produce and the hardest to notice, so an edge becomes canonical when at least two provisions in at least two documents say it, counted apart because forty provisions of one decree are one drafter's habit.
The single exception is the relations the drafter wrote down: a genus or an enumerated subtype out of a definitions clause is a hierarchy edge somebody in the National Assembly typed, and those stand on one provision.

Direction gets its own pass and its own number.
M4 shipped 75,252 amendment edges pointing the wrong way, which is what happens when the thing that read the sentence is also the thing that checks it.
So the second pass is blind: it sees the quote and two labels in a fixed order, and it never sees the relation type, the identifiers, or which way the first pass said the arrow runs.
Direction accuracy is reported next to relation precision and never folded into it, because a graph with 95 percent relation precision and 80 percent direction accuracy is worse than useless for traversal.

Transitivity is a query and never an edge.
A materialised closure hides which link is weak and lets one bad edge poison a whole subtree without leaving a trace of where it entered.
A cycle in the hierarchy is reported as the whole path rather than as one offending edge, because it almost always means two concepts should have been merged and naming one edge would send a reviewer to fix the wrong link.

Edge count per concept per document is reported next to everything else.
The failure it watches for is a graph where every co-occurring pair of concepts acquires an edge, so traversal returns everything and answers nothing.
It looks fine one edge at a time and shows up only in the ratio.

```sh
luatdo relations prompt <provision-id>   # the exact prompt, no model called
luatdo relations extract                 # one file of raw sightings per document
luatdo relations define                  # fold the invented names, define, canonicalize
luatdo relations build                   # fold across the corpus, refuse a layer that fails its invariants
luatdo relations verify                  # the blind direction pass
luatdo relations ask 21 <concept-id>     # what it requires, from the graph alone
```

The competency questions run over the edges alone with no text read, because a question that has to fall back on searching the corpus is a question the graph did not answer.

```text
question 21    what vn:term:...:giay-phep-xay-dung requires, from the graph alone with no text read
               giấy chứng nhận quyền sử dụng đất (40 provisions in 9 documents, corpus, canonical)
                 hồ sơ địa chính (4 provisions in 2 documents, corpus, canonical)
               produced by cấp giấy phép xây dựng (11 provisions in 5 documents, corpus, canonical)
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

## What changed and when

The section above says a component has wordings.
This layer says which wording was in force on which day, and which instrument put it there.

An amendment arrives as a sentence in somebody else's document.
`Sửa đổi, bổ sung Điều 7 như sau:` followed by the whole of the new article 7 is one operation with a target, a kind, a date and the replacement words, and none of those four is a field anybody wrote down.
So a model reads the sentence into a structured operation, and the reading is checked before it is applied.
The quote the model gives has to appear in the provision verbatim, and the reference it names has to resolve to a component that exists.
An operation that fails either check is quarantined with a reason and applied to nothing, because an amendment applied to the wrong component is worse than an amendment not applied at all.

There are ten event kinds and suspend and resume are two of them, because a suspended provision is a third state and reporting it as in force is the mistake this layer exists to prevent.
Intervals are half open, so the day a successor begins belongs to the successor and no date has two answers.
An amendment with no date is excluded from every point in time query and never given a guessed one.

Versions are made by aggregation rather than by copying.
Amending one point makes a new version of that point, of the clause above it and of the article above that, and each of those reuses the version identifiers of every sibling that did not change.
The replacement text is then parsed by the same walk that parsed the article it replaces, so an amended article still has clauses and points rather than one paragraph of prose, and a component the replacement leaves out is closed rather than left standing.

Nine invariants are checked after every build.
Eight of them are consistency: no two versions of one component overlap, every version has an event, every event names a document, and so on.
The ninth is the only one that can prove the layer wrong rather than merely inconsistent.
Where the state publishes a `văn bản hợp nhất`, a consolidated text of an instrument as it currently reads, the computed text at the consolidation date has to match it.

One chain in the corpus is complete enough to run that check end to end today.
Nghị định số 72/2025/NĐ-CP was amended by Nghị định số 278/2026/NĐ-CP and consolidated as Văn bản hợp nhất số 68/2026/VBHN-NĐ-BCT.
Reading the amending decree took 3 model calls over 3 provisions, 3,335 input and 5,811 output tokens, and the cost is unavailable because the route it went to has no rate card.
That yielded 7 operations, and the build made 510 versions of 485 components across 9 events with nothing quarantined and nothing undated.
Compared against the published consolidation, 54 components matched on structure and 51 agreed on text, which is 94.4 percent.

The three that disagree are worth stating exactly, because none of them is the version graph being wrong.
One is a formula that the source of the base decree does not carry and the source of the consolidation does.
One is `Vộ Tài chính` where the consolidation reads `Bộ Tài chính`, a typo in the base decree as published.
One is `Tập đoàn điện lực Việt Nam` against `Tập đoàn Điện lực Việt Nam`, which is two publishers capitalising the same proper noun differently.
The check reports a rate and lists every divergence with both texts rather than asserting a threshold, because a threshold here would turn a number that means something into a boolean that does not.

Of the 100 consolidated texts in the corpus, that one is the only one with an amendment read against it so far, and the report says so on every run rather than letting one instrument look like coverage.

Three questions are answered from the version graph alone, with no text read at query time and a date on every one of them.

```text
$ luatdo temporal ask 18 vn:law:2025:72-2025-nd-cp:article-7
vn:law:2025:72-2025-nd-cp:article-7 has 2 versions
  v1  2025-03-28 to 2026-07-09 in_force     enact        vn:law:2025:72-2025-nd-cp
  v2  2026-07-09 to now        in_force     amend        vn:law:2026:278-2026-nd-cp
      Sửa đổi, bổ sung Điều 7 như sau:
```

Question 16 prints what a component said on two dates and every event between them, and question 17 lists versions that were in force for less than a given number of days before being replaced.
Question 17 currently returns nothing, which is the honest answer for a graph built from a single amending instrument rather than evidence that no provision in Vietnamese law is short lived.

A norm, a defined term and a relation edge are readings of particular words rather than of a component, so each of them inherits the interval of the wording it was read from.
`luatdo temporal stamp` writes that interval to a sidecar, and it says on every one of them where the interval came from, because three quite different answers would otherwise look identical.
A `version_graph` interval has both ends read.
A `commencement` interval starts the day the document took effect and is open, because nothing in the citation graph says the component was ever amended.
A `commencement_amended` interval starts the same way and its end is unknown rather than open, because something did amend that document and nobody has read it yet, and a record with that source answers no when asked whether it is in force on a date.
Saying no there is the awkward answer and it is the right one: this layer would rather report that it does not know than report a wording as current when an unread amendment may have replaced it.

The sidecar is a sidecar rather than a field written back into the norm, term and relation stores, for the same reason Neo4j is a projection.
Those stores are what the extraction passes produced, the interval is derived, and a derived value written into a source file cannot be told apart later from something a model said.

On the store today that is 2,559 intervals over 2,565 readings, 255 of them open and 2,304 with an end nobody has read yet.
The six with no interval at all are readings of the 2013 Constitution, which neither dataset gives a commencement date, and the honest answer there is to leave the field empty rather than type in the date everybody knows.

## What a document is about

There is a subject vocabulary of 24 domains and 144 subdomains, hand written once against Vietnamese practice and shaped after EuroVoc.
It is not an ontology and nothing downstream reasons over it.
It has two uses: getting around a corpus of a hundred and twenty eight thousand documents, and drawing samples that are not all provincial land decisions.

`luatdo subjects` files every document by matching cue phrases against its title, type and issuing body, and records the method it used on each assignment.
A document lands in at most three subdomains and carries the domain of each up with it.
The design calls for a distilled classifier trained on model labelled seed data, which is the standard way to get a cheap multi-label classifier over a large corpus, and that is not what ships here: this pass is lexical, and the assignments say `lexical` so nobody has to guess later.
Measured against 65 corpus documents drawn at random and filed by hand, it recalls 0.91, and the six it misses are in the test table rather than relabelled out of it.

Of 112,990 documents that are not quarantined it files 96,371 and leaves 16,619 under nothing, and it uses every domain and every subdomain in the vocabulary.
The 16,619 are not a failure to route around: a title like `Quyết định về việc bãi bỏ Quyết định số 12/2004/QĐ-UB` says nothing about any subject, and a classifier that guessed one would be inventing navigation.
They get a stratum of their own in the sample, because the documents nothing could read are the ones worth reading.

`luatdo sample` draws from those assignments, stratified over subject crossed with instrument type.
There is no random source in it.
Each document is ranked inside its stratum by a hash of the seed and its identifier, so two machines agree on what a seed produces, and a corpus that gained a document keeps almost every pick it had.
The corpus falls into 966 strata, so a draw smaller than that reaches only the largest ones, and the command says so rather than letting the number look like coverage.

## What a norm says

A statement of law has parts, and the parts are the answer.
Somebody has to do something, for or against somebody else, when certain things hold, unless certain other things hold, by a certain time, or else.
Flatten that to a triple and the conditions go, and a duty whose condition was dropped is not a weaker fact but a false one.

So a norm carries a bearer and a counterparty rather than a subject.
The bearer is the party that must act, may act or is forbidden to act; the counterparty is the other side of the relation.
Every reference that names a party is flagged as one, and a bearer the registry cannot place as a legal actor is a validation failure rather than a note in a log.

Conditions and exceptions are objects with their own verbatim quotes and their own kinds, four of each.
A condition is a precondition, a temporal condition, a threshold or a qualifying condition.
An exception is a carve out, an override, a consented exception or force majeure.

Deadlines are not asked for as numbers.
A model asked for a number returns one for every phrase, including the phrases that have none, and a deadline of five that came from `trong thời hạn hợp lý` is worse than no deadline at all.
The model copies the phrase and a grammar takes it apart, into a length or a fixed date, an anchor, and whether the days are working days or calendar days.
That last distinction is the whole of competency question 12: five working days is seven calendar days, and a query that treats them as the same number answers with the wrong provisions.
The grammar reads numbers written in words as well as in digits, because the older instruments spell them out and a parser that only reads digits goes blind on the 2006 law while working perfectly on the 2019 one.
That is also why the trusted store is rebuilt from the extraction artifacts rather than edited in place: the deadline fields are derived from the phrase, so improving the grammar and running `luatdo build` again is enough, and no model is paid to read the same provisions twice.

Sanctions carry a legal basis or they are not recorded.
The basis is then resolved into a document identifier, which is what makes a prohibition in a law and the penalty for it in a decree two ends of one edge instead of two pieces of unconnected text.
Procedures are grouped after extraction rather than during it, because a procedure's steps are read one provision at a time by calls that cannot see how many there are.

### The word được

`được` is the passive marker, the permission marker, and part of `được quyền`.
`Người lao động được trả lương đúng hạn` is a right of the worker stated in the passive voice, not a permission granted to them, and the duty it implies sits on the employer.
Read it as a permission and the obligation moves off the party that owes it, and every question about worker rights then answers with an empty set.

It gets three defences and not one.
The prompt states the three senses with an example of each.
The gold subset annotates the sense of the word separately from the statements, so it is measured whether or not the extraction found the norm at all, and the draw oversamples clauses that use the word and says by how much.
The score reports a confusion table rather than a rate, so the permission for right swap appears as a number with a name on it instead of hiding inside an accuracy that stays high.

```sh
luatdo norms gold sample -n 120 -duoc 60   # draw, then annotate by hand
luatdo norms gold check                    # the ruler is checked harder than the thing it measures
luatdo norms gold score
```

### Asking it questions

```sh
luatdo ask 9 vn-legal:Employer --doc vn:law:2019:45-2019-qh14
luatdo ask 12 --days 5
luatdo ask 13
luatdo ask 15
```

Question 9 is the duties an instrument places on a kind of actor and which carry a consequence.
Question 10 is the norms with nobody to owe them, split into the provisions that name no actor in their own words and the ones whose actor the extraction dropped, because one of those is fixed by rereading and the other never will be.
Question 11 is a procedure as ordered steps with its deadlines, 12 is every deadline shorter than five working days with the actor who must meet it, 13 is the prohibitions nothing in the corpus punishes, 14 is what has to hold for one duty and what releases its bearer, and 15 is the norms that name an authority no provision ever resolves.

Every answer says what it left out.
Question 12 reports the deadlines it could not take apart, question 9 reports the duties with no consequence as a count and not just as rows, and question 13 reports the total it counted against.
An answer that returns a clean list and says nothing about what it skipped reads as coverage, and it is not.

## Running a campaign

A campaign is a long job against a metered service, so it is built to be interrupted.
The queue is recomputed from disk on every run, work is committed one provision at a time, and a provision that fails leaves no artifact, which is exactly what puts it back in the queue next time.

Model access is a routes file: named endpoints in rank order, each with its own credential and its own rate card. See [where the models come from](#where-the-models-come-from).

```sh
luatdo doctor --write-routes
luatdo doctor
luatdo run --parallel auto
```

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

### A named campaign

A pass over all 128,097 documents measures the throughput of a queue and nothing else.
The competency questions need one area of law end to end: question 13 asks which prohibitions nothing punishes, and that is meaningless unless the instruments that do the punishing are in the same pass as the ones that do the forbidding.

So a campaign is a named slice, kept as data so a report written months later selects the same documents the run did.

```sh
luatdo campaign list
luatdo campaign scope labour-2025
luatdo run --campaign labour-2025 --parallel 6
luatdo campaign report labour-2025
```

`labour-2025` is the labour code and every national instrument under it, in force through 2025: 1,239 documents and 36,371 extractable provisions, out of 4,563 documents in the labour subject.
The cut is on the issuing body rather than the instrument type, because a decision is central when a ministry signs it and local when a province does, and the type says nothing either way.
The 2,119 provincial documents it drops are two thirds of the subject by count and almost none of it by normative content.
That is a statement about what the pass is for, and it is in the scope definition where a reader can argue with it.

The report leads with what the campaign has not reached, because a report that opens with the number of statements extracted invites the reader to take that as the number of statements in the corpus.

It ends with a known defects section, and that section is derived from the report's own counts rather than typed in by whoever is writing the summary.
A defect somebody has to remember to record is a defect that stops being recorded around the third campaign.

```text
statements     597 proposed, 359 verified by the judge, 0 kept by a person, 110 rejected, 128 never valid enough to judge
concepts       0 of 1798 references resolved to a concept
deadlines      49 phrases, 31 taken apart, 0 shorter than five working days
known defects  5
               unparsed-deadlines
                 18 of 49 deadline phrases carry no length or date this grammar can read, so question 12 answers over 31 of them
               no-concept-layer
                 none of the 1798 references resolved to a concept, so every question that turns on a concept is answered from surface strings
               judge-below-the-gate
                 the judge agrees with a person at kappa -0.120 over 50 labelled items against a floor of 0.400 over 50, so the verdicts above are not evidence of precision
```

The report counts every statement the pass proposed, not the ones that survived it.
Compiled from the trusted store alone it would report that a hundred percent of statements were accepted, which is the most flattering number this project can print and the least true.

## Measuring it

Everything above this line is a claim about a corpus, and a claim about a corpus needs an instrument.
The instrument here is a language model judging whether a statement follows from a provision, so the first thing worth measuring is the judge.

```sh
luatdo eval suite                # one table per layer, every number with its sample
luatdo eval judge sample         # draw a blind sample, half of it from the gate's rejections
luatdo eval judge score          # score the labels a person filled in
luatdo eval gates labour-2025    # the release gates, which the export path also runs
luatdo eval baselines            # the 23 questions against two simpler systems
luatdo eval ablations            # the 23 questions with one layer removed at a time
```

`judge sample` writes the provision window and the proposed statement and withholds the verdict, which is in a second file.
Half the sample is drawn from statements the gate rejected, because a sample drawn from what a gate accepted measures the accepted half of its behaviour and nothing else.
A rejection that a person reads as correct is a correct statement the gate deleted, and that is the failure mode worth paying for.

Agreement is reported as raw agreement and Cohen's kappa together, with the Landis and Koch bands named as the convention they are.
When one label takes most of the sample the report says so in the line below the numbers.
That case is not a footnote here, it is the ordinary case: a gate that accepts nine statements in ten produces a set where raw agreement reads high and kappa reads near zero at the same time, and printing either one alone is arguing for a conclusion.

```text
agreement     raw 0.880 (44 of 50), kappa -0.034 (worse than chance)
              98% of the answers carry one label, so raw agreement reads high and kappa reads low, and neither number stands alone here
```

`judge rejudge` runs today's judge over the same sampled statements and writes its verdicts beside the original key rather than over it.
The labels stay where they are, `judge score` prints both keys under them, and it prints how many items moved and in which direction.
A prompt shown only by its own new number is indistinguishable from a prompt that was tuned until the number came out.

`eval gates` is the same code the export path runs, so a campaign that fails it cannot ship by accident.
`--force` exports anyway and says in as many words that the graph is not one to publish numbers from.

The baselines are the two systems this project would have to beat to be worth building: a search index with a citation table, and flat triple extraction over the same scope.
Both answer 2 of the 23 competency questions from a layer that means what it says.
The ablations remove one layer at a time from the full system, and the concept layer is worth 14 questions, the temporal layer 7, and conditions and exceptions 3.
Those are counts of questions the design can answer, not of answers checked against a gold set, and the report says which it is.

## Where the models come from

Reading two million norm units through a metered endpoint at list price is not a casual decision, and a project that only works when someone is willing to make it is a project that does not run.
So a route is not one vendor.
It is a named endpoint with a wire, a model, a rank, and the name of an environment variable holding its credential.

There are three wires because the capacity is spread across three.

- `chat` is `POST /v1/chat/completions`, which is what almost everything that is not OpenAI serves. The free tier in front of DeepSeek, Nemotron and Mimo speaks it, and so does a local Ollama.
- `responses` is `POST /v1/responses`, which is what OpenAI serves.
- `codex` is the backend the Codex CLI talks to, reached with the OAuth credential the CLI already left in `~/.codex/auth.json`. There is no URL to configure because it is not an endpoint anyone points at.

A route file holds no secret.
Credentials are read from the variable a route names in `api_key_env`, so the file can be committed, pasted into an issue, or copied to the other three machines with nothing to scrub.
The codex wire needs no variable at all, because the credential is already on the machine.

```json
{"routes": [
  {"name": "codex", "wire": "codex", "effort": "high", "rank": 10},
  {"name": "free-deepseek", "wire": "chat", "base_url": "https://opencode.ai/zen/v1",
   "model": "deepseek-v3.2-free", "api_key_env": "OPENCODE_API_KEY", "rank": 30}
]}
```

`luatdo doctor --suggest-routes` asks every route what models it serves, keeps every rank you already have, disables a route whose model the endpoint stopped listing rather than deleting the row, and offers any newly catalogued free model as a new route with `disabled` set.
It prints the suggestion to stdout.
`--write-routes` writes it to the routes file instead, and refuses to overwrite a file that is already there, because the ranks in it were measured rather than guessed.
A new route lands at the end of the free band so an unproven model never displaces one with evidence behind it, and it arrives disabled so a person decides whether it touches the graph at all.

Failover is per call and matched to the cause.
An authentication failure disables the route for the process, because retrying a bad credential is pointless.
A model the endpoint does not serve disables it too, since a free tier catalogue that rotated is a permanent failure wearing the clothes of a transient one.
A quota error cools the route until it clears, and when the provider states the reset, as the codex backend does for a plan window that can be days wide, that exact time is honoured rather than guessed at.
Everything else is treated as transport: thirty seconds the first time, doubling on each consecutive failure to a ceiling of thirty minutes, and reset the moment the route answers.

`luatdo doctor` probes every route and prints what it found, including the plan windows on a subscription route, which exist nowhere but the response headers.

```text
route codex          alive in 2.752s  plan plus, primary 6% of a 7d window, resets Sat 16:41
route free-deepseek  alive in 2.682s
route free-nemotron  failed: transport: chat stream error
ready 3 of 4 routes alive
```

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
