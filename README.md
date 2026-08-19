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

## Install

On Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/tamnd/luatdo/main/install.sh | sh
```

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/tamnd/luatdo/main/install.ps1 | iex
```

Both take a release binary, check it against the release checksums, and put it somewhere on your PATH that needs no administrator.
There is nothing to build and no Go toolchain to have.
If you would rather build it, `go install github.com/tamnd/luatdo/cmd/luatdo@latest` still works.

## The graph, without running the pipeline

The pipeline takes days and a few hundred dollars of model calls to run over the whole corpus.
The result of running it is published, so you do not have to:

```sh
luatdo neo4j install
```

That downloads the graph from [open-index/luatdo-graph](https://huggingface.co/datasets/open-index/luatdo-graph) on Hugging Face, checks it against a pinned checksum, imports it into a local Neo4j offline, and waits until the database will answer a query before telling you it is up.
It needs podman or docker, about 20GB of disk, and roughly ten minutes on a decent connection.
The same command works on all three platforms, and the archive is unpacked by the tool itself, so Windows needs no `tar` and no WSL.

When it finishes it prints the four environment variables the rest of the tool reads, in the syntax of the shell you are standing in.
Then open <http://localhost:7474>, or ask a competency question from the command line.

The pieces are separate commands as well, for when you want to do this in stages or over your own export:

```sh
luatdo neo4j fetch     # download and unpack the published graph, no runtime needed
luatdo neo4j load      # offline import, about a minute for eight million nodes
luatdo neo4j up        # start a server over what was imported
luatdo neo4j status    # runtime, export, container, and the node and edge counts
luatdo neo4j down      # stop the server and keep the graph
luatdo neo4j wipe      # throw the imported graph away
```

`load` is a separate step from `up` because the offline importer is the fast path and refuses to run against a live database.
Eight million nodes take about a minute that way and hours over Bolt.

### The same graph as tables

The archive above is the graph in the shape `neo4j-admin` wants, which is the wrong shape for anything that is not Neo4j.
The same graph is published as Parquet in the same repository, one directory per table, so you can page through it in the browser or read it without installing a database:

```python
from datasets import load_dataset

docs = load_dataset("open-index/luatdo-graph", "documents", split="train")
```

Node tables have an `id` and a `labels` list, relationship tables have a `start_id`, an `end_id` and a `type`, and the endpoints join to node `id`.
Everything is typed, so a missing value is null rather than an empty string.

To produce that from your own export:

```sh
luatdo export parquet
```

It reads the Neo4j export rather than the store, so what is in the dump is what gets converted, and it writes the dataset card alongside the tables.
The card is generated from the tables that were actually written, including the row counts and the node labels that are actually present, which is how the published card came to admit that the concept layer is empty.

## Quick start

To build the graph yourself instead:

```sh
luatdo fetch uts_vlc
luatdo parse
luatdo cite
luatdo anchor
luatdo subjects
luatdo terms
luatdo ontology init
luatdo export neo4j
```

Then ask the graph one of the twenty six competency questions, without writing any Cypher:

```sh
luatdo graph list                 # the twenty six, and the parameters each takes
luatdo graph ask --question 20    # which norms a later redefinition put in doubt
```

```text
20. A concept was redefined by a later instrument. Which earlier norms mentioning it are now potentially affected, and which of them were never amended?
asked with limit = 200

[1]
  concept             người lao động
  redefined_by        vn:law:2019:45-2019-qh14
  redefined_on        2021-01-01
  affected_document   vn:law:2014:58-2014-qh13
  affected_provision  vn:law:2014:58-2014-qh13:article-5
  action              đóng bảo hiểm xã hội
  never_revisited     yes

1 row
```

Every question ships with defaults you can replace one at a time, so asking about a different article does not mean retyping the rest:

```sh
luatdo graph ask --question 16 --param date=2023-01-01
luatdo graph query --question 16   # the Cypher it just ran, to edit and paste
```

The queries are in the export directory too, under `queries/`, so a dump answers these on a machine that has never seen this repository.
Open http://localhost:7474, drop `style.grass` on the window, and paste one in:

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
| `luatdo schema` | Find out what the closed registry is missing, and measure the finding out |
| `luatdo extract` | Schema-constrained LLM extraction of entity mentions under the closed registry |
| `luatdo link` | Resolve mentions against the registry and the defined term table, with scores |
| `luatdo norms` | Extract norm statements and verify them with the entailment judge |
| `luatdo verify` | Run a verification stage on its own, and train and measure the cheap entailment gate |
| `luatdo omission` | Audit what the pass never said anything about, by surface form and by re-extraction |
| `luatdo ask` | Answer the norm competency questions over the trusted store |
| `luatdo retrieve` | Rank components for a question in Vietnamese, scoped before anything is ranked |
| `luatdo answer` | Answer that question with claims a trusted statement supports, or refuse |
| `luatdo statute` | Run the committed statute benchmark and score construction, retrieval and generation apart |
| `luatdo conflicts` | Put trusted statements in a comparable form and find the pairs that cannot both be obeyed |
| `luatdo temporal` | Read amending instructions, build the version graph, and answer questions at a date |
| `luatdo prompt` | Print the exact prompt for a provision without calling a model |
| `luatdo review` | Work the human review queue for gated statements |
| `luatdo build` | Assemble verified statements into the trusted store |
| `luatdo export neo4j` | Project the trusted store into Neo4j, full import or incremental merge, scoped to a campaign |
| `luatdo export rdf` | Project that dump into N-Triples, with the vocabulary alignment shipped beside the data |
| `luatdo export legalruleml` | Write one measured campaign's trusted norms as LegalRuleML, with the release gates enforced |
| `luatdo graph` | Ask the projected graph one of the twenty six competency questions |
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

No identifier ever names two provisions, and that takes work because the instruments do not cooperate.
An amending law quotes the article it enacts, and the quoted text is full of lines that look exactly like structure, so the walk has to know it is inside a quotation.
It used to count quotation marks as it passed them, which meant one stray mark anywhere in a document changed the reading of everything after it: the 2005 customs amendment opens and closes all 24 of its quotations and carries one loose ASCII mark besides, and that one mark turned 27 provisions into 95 under 46 identifiers, twelve of them sharing one.
Marks are now paired over the whole body before the walk starts, so a quotation that never closes costs itself and nothing else.

That fixes the amending laws and not the drafters.
7,730 documents number one of their own provisions the same as an earlier one, 51,056 provisions in total, most often two adjacent points lettered the same and sometimes an annex that restarts at `Chương I`.
There is no identifier those provisions would both recognise, so the later ones carry their occurrence instead, and a reader who sees one knows the source repeated a label rather than that the parser invented one.

```text
vn:law:2011:07-2011-tt-bnv:article-1:clause-1:point-a
vn:law:2011:07-2011-tt-bnv:article-1:clause-1:point-a~2
```

An annex opens with its kind on a line, and the drafters qualify that kind on the same line as often as they leave it standing alone: `CHƯƠNG TRÌNH KHUNG`, `QUY CHẾ TỔ CHỨC VÀ HOẠT ĐỘNG`.
A header the walk does not read is worse than a document it skips, because the annexed instrument is not dropped, it is read as a continuation of whatever article of the parent decision came last.
Reading the qualified form put 39,960 provisions under the annex that carries them across 1,084 documents, and it is the qualifier having no lowercase letter in it that tells a header set in capitals apart from a sentence opening with the same word.

Not every annex numbers its parts the way the parent does.
A training programme runs `A.` and `I.` and `1.` and `a.`, a table of administrative procedures repeats `1. Trình tự thực hiện` once per procedure, and neither is an article, a clause or a point.
Those lines opened no provision and so went nowhere at all until the walk was given somewhere to put them, which is the annex itself: 14,719 annexes now carry text, 275 hold neither text nor a provision, and 5,758 provisions that had been read as clauses and points of the parent's last article are text on the annex they belong to instead.
Reading that lettering as structure is a separate question and is not answered here, because inventing a hierarchy from marks the drafter never labelled as one is how the misfiling happened in the first place.

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

## What the closed registry is missing

The class registry is closed and frozen on first use.
A model cannot add to it, because a fabricated predicate in a legal graph is a defect and a registry a model can extend is not a closed registry.
The cost of that decision is that the registry stays wrong in ways nobody can see from inside it, and for a long time the only thing done about that was to write the cost down.

`luatdo schema` is the machinery for finding out.
Seven passes, each measured, and none of them writes to the registry.

```sh
luatdo schema invariants    # which structural rules fire, and how often
luatdo schema blindspots    # what the corpus keeps asking for and does not get
luatdo schema define        # open extraction, then define, then match against definitions
luatdo schema taxonomy      # induce the hierarchy top down and bottom up, and compare
luatdo schema repair        # a bounded model loop over the records that broke a rule
luatdo schema conflicts     # decide the concepts the hierarchy puts under two parents
```

### Define before you canonicalize

The old pipeline proposed a class, compared the label against the registry, and dropped what did not match.
A near miss and a genuine gap look identical from there, because a label is not a meaning.

The define pass, following EDC, asks the model to define what it proposed before anything is compared, and then matches definitions against definitions.
Over 533 proposals it defined all 533 and matched 22 of them to an existing class.
The other 511 went into the queue carrying their definition, their counts, example quotes, and the nearest registry class with the model's reason for rejecting it, in Vietnamese, so a reviewer is ruling on a case rather than on a string.

That is 4 percent matched, which sounds like the registry is missing almost everything.
It is not, and the review cycle below is how that became clear.

### Induction, both directions

Taxonomy induction is run as its own pass because it fails in its own way.
The held out target is the hand written subject vocabulary, 144 children under known parents.

| Direction | Placed | Correct |
| --- | --- | --- |
| Bottom up | 144 of 144 | 137 |
| Top down | 143 of 144 | 143 of 143 |

Top down declines to place one child and gets everything it places right.
Bottom up places everything and gets seven wrong.
The two agree on 137 of 143, and the disagreements are the interesting rows: bottom up puts vocational education under education, and public investment under public finance, both of which are defensible readings that the hand written vocabulary happens not to take.

This is prompting, and OLLM predicts prompting alone will not produce a structurally sound ontology.
On a flat two level target with a fixed parent set it does well, and that is a weaker test than OLLM's.
It is not evidence against OLLM, and nothing here should be read as saying fine tuning is unnecessary.

### The concepts under two parents

Multiple inheritance is legitimate in a general ontology and is not legitimate here, because these edges are induced one provision at a time, so a second parent is nearly always two readings of one word.

The stored relation layer offers no conflict to resolve, because it holds no canonical `BROADER` edge at all.
That is not the same as having no conflicts, and the resolver says so rather than printing a zero.
It falls back to the children top down induction left contested, decides them by asking about meaning rather than by taking the higher count, and records which source it ran over.

```text
conflicts: 3 edges in the layer, 0 of them canonical BROADER, 0 concepts under more than one parent
note: the relation layer offers no conflict, so the resolver runs over the 1 children top down induction left contested
  lao-dong/giao-duc-nghe-nghiep        keeps giao-duc, drops lao-dong
```

No edge is changed.
The resolution is a proposal for the review queue, the same as everything else here.

### Repair, scored on whether it was right

The repair literature reports near perfect syntactic validity for model repairs, which quietly says the problem is semantic.
So this scores both, separately, and the gap between them is the finding.

Of 1,479 norm records, 311 broke at least one invariant.
The bounded loop cleared 168 of them, declined to change 133, drifted on 1, and introduced a new break in 9.
A second call then asked whether each repair was grounded in the provision it claims to come from, and 107 of 178 were.

So roughly 54 percent of broken records got fixed, and roughly 60 percent of the fixes are actually supported by the text.
Multiply those and about a third of the original breaks are genuinely repaired.
Reporting the first number alone would have been the mistake the literature warns about.

### The prediction the invariants test

The cyber threat intelligence work predicts that missing mandatory attributes are the most common schema violation from LLM extraction.
Our invariants are cardinality rules of exactly that shape, so the firing distribution is a direct test.

| Invariant | Records | Mandatory |
| --- | --- | --- |
| `bearer-missing` | 233 | yes |
| `bearer-not-marked-actor` | 49 | no |
| `evidence-quote-not-verbatim` | 22 | no |
| `bearer-class-not-legal-actor` | 7 | no |
| everything else | 13 | mixed |

235 of 324 breaks are mandatory attribute violations, and one rule accounts for 233 of them.
The prediction holds.
It also localises the problem: the extraction is not broadly sloppy, it is specifically bad at naming who the norm binds.

### A cycle actually worked

A queue nobody works is a queue that measures nothing.
The first cycle went over the 20 labels the corpus asked for most often, out of 530 distinct proposals.

| Decision | Count |
| --- | --- |
| Merged into an existing class | 16 |
| Rejected | 3 |
| Promoted | 1 |

Candidate precision on that slice is 1 in 20.
The one promotion is `Công trình xây dựng`, a construction work, because the registry has no class for a physical object that law regulates: not `Location`, which is where a thing is, and not `Amount`, which is how much of it there is, and the corpus asks about the height of the structure.

The 16 merges are the real result.
`Cơ quan nhà nước`, `Cơ quan hành chính nhà nước` and `Cơ quan chuyên môn thuộc Ủy ban nhân dân cấp tỉnh` are all authorities the registry already has under other names.
So the 4 percent match rate from the define pass was measuring label distance, not coverage.
The registry's class coverage is good and its naming is what the corpus keeps missing.

Every decision carries its reason, because the queue is append only and is the whole record of how the registry grew, and a promotion with no reason is indistinguishable later from a promotion nobody thought about.
510 labels are still pending.

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

## Two norms that cannot both be obeyed

Question 19 asks which provisions contradict each other, and the Cypher form of it says in its own comment that it will produce false positives because it compares a bearer and an action and ignores the condition that distinguishes them.
That comment is what this pass exists to delete.

```sh
luatdo conflicts parse            # every trusted statement into a comparable form
luatdo conflicts check            # the rules over the pairs, no model in the comparison
luatdo conflicts bench            # the generated gold set, with a judge behind it
luatdo conflicts baseline         # ask a model directly, over the same pairs
luatdo conflicts explain          # put the findings into Vietnamese, after the fact
```

No model is ever asked whether two norms conflict.
A model asked that question answers it, fluently, in both directions, and there is no way from the outside to tell a real contradiction from a plausible one.
So the strong model is a parser: it maps one trusted statement onto a comparable form, which is a party, an act, an object, a modality and the scope the norm applies in, and it never sees a second statement.
Four rules then run in code over the pairs: an obligation against a prohibition, a permission against a prohibition, two deadlines on one duty, and two consequences for one act.
Every finding carries the slots that had to agree and the slot where the pair parts, which is the minimal responsible set spelled out for a person rather than for a solver.
Lex superior, lex posterior and lex specialis are reported beside the finding as ranking information and are not applied, because deciding which provision wins is not a thing to do to somebody without telling them.

Scope intersection is arithmetic and stays in code: half open intervals, so a version that ends the day its successor starts does not overlap it, and a statement that stands down to another instrument is pointing at it rather than fighting it.
Conditions are the hard half.
Containment is sound and often silent, and a pair where neither condition set contains the other may describe one situation or exclude itself.
That gap was the entire error of the checker on the gold set, so those pairs, and only those pairs, go to a model that is shown the two sets of circumstances and the party they are about, and nothing else: no operator, no act, no deadline, no quote, nothing that would let it work out what either provision requires.
A model that is unsure is told to say the circumstances can hold together, so it can only ever remove a finding it is confident about, and one that is unreachable costs precision instead of conflicts.

The gold set is generated from real statements by changing exactly one thing, so the label falls out of the mutation instead of out of anybody's opinion.
Four mutations produce a conflict and six produce a near miss, and the two condition mutations pull against each other on purpose: one plants circumstances that exclude each other and one plants circumstances that differ and can both hold.
A judge that answers every question the same way scores on one of them and fails the other.

```text
180 generated pairs, 60 conflicting by construction and 120 near misses
precision 0.98, recall 0.98, f1 0.98
40 pairs went to the judge, 20 dropped as never triggered together, 0 it would not answer
```

Without the judge the same set scores 0.75 precision at the same recall, and all 20 of the false positives are the exclusive conditions mutation.
The two cases still wrong are worth more than the two decimal places.
Both are pairs where the generator grafted an employment condition onto a tax provision or onto a duty of the Government, and the model's answer about them is defensible while the label is not, which is a limit of generating a gold set rather than annotating one.
An earlier table cost eight more, and it deserved to: it called working in the country and working abroad mutually exclusive, which is true of one worker and false of an enterprise that employs hundreds.

The other half of the argument is what happens when nobody builds any of this and just asks the model.
`conflicts baseline` puts the same 180 pairs to the same endpoint, one call each, with both norms written back out as Vietnamese sentences and nothing from the pipeline in the prompt.

```text
asked the model directly about 180 pairs, 180 calls
precision 0.46, recall 0.70, f1 0.55
```

The shape of that is more useful than the score.
Asked directly the model is good at the things a person would call a contradiction on sight, 20 of 20 flipped operators and 20 of 20 clashes under conditions that can both hold, and it is poor at everything that is arithmetic: it caught 2 of 20 pairs with two different deadlines on one duty, and it called a conflict on 17 of 20 pairs whose provisions were never in force on the same day and on 19 of 20 where one norm expressly stands down to the other.
Those last two are 36 confident false positives out of 40 pairs, and they are the two tests the checker does in four lines of code without asking anybody.
That is the split the whole design is built on: reading is what the model is for, and comparing is not.

The first version of this baseline scored 0.00 and it was measuring my own harness.
The generated side of a pair has no sentence anywhere in the corpus and was carrying a note saying it came from the test suite, and several mutations plant a condition or an interval on the original side too, so the two quotes were not the two norms being compared.
Both sides are now written back out from the same fields, which also hands the baseline the norm rather than the paragraph it was extracted from, so it loses on cleaner input than the checker gets.

These pairs are generated and the report says so wherever it prints a number.
The measurement on real law is the noise floor, which needs no annotation: two statements from the same provision cannot contradict each other, because a drafter does not contradict themselves inside one clause, so a rule that fires there has fired on two readings of one norm.

On the labour and tax scope the detector reports nothing, over 671 comparable forms and 55 pairs, at a noise floor of zero.
A run that reports nothing is either a clean scope or a detector that could never have fired, and a funnel ending in zero cannot tell those apart, so the check prints what the scope gave the rules to work with: 467 obligations, 8 prohibitions, 79 permissions, 117 rights, 43 deadlines counted from an event and no sanction at all.
Both operator rules need a prohibition on one side and there are eight, and the sanction rule cannot fire anywhere in this scope whatever the norms say.
That is the honest reading of the zero, and it is printed beside it.

## What follows from what

The norm layer puts the act a provision is about into a slot, and that slot is a string that points at nothing.
So the graph knows that a service enterprise must hand its licence back, and it cannot walk from handing the licence back to what happens next, even though the next step is in the same law and was read at the same time.
This pass makes the acts themselves into nodes.

```sh
luatdo events extract --campaign labour-2025   # the acts each provision names, and their parties
luatdo events build                            # fold them corpus wide, with the chains and the norm links
luatdo events verify                           # read every chain again without showing it the claim
luatdo events ask 24 vn:event:issue:cap-giay-phep
luatdo events propose                          # the act types the registry did not hold
luatdo events ablate                           # what the layer would lose if identity stopped at the document
```

An act is a class from a closed registry and a short Vietnamese label, and its identifier is built from those two, so the same act named in a law and in the decree that implements it is one node without anybody matching strings afterwards.
The chains are the four a procedure needs: one act triggers another, comes before it, is a precondition of it, or rules it out.
Every chain is then read a second time by a model that is shown the two acts and the sentence and is never shown which way the first pass pointed the arrow, and the verdict is stored beside the chain rather than replacing it.
The links from a norm to its act and to its penalty are written while one provision is in view, so both came out of one paragraph instead of out of a label that happens to occur twice in the corpus.

```text
5 documents read, 167 provisions named an act
events         362, 0 canonical, 362 provisional, 191 invented classes
participants   0
chains         100, 0 canonical
norm links     247, 247 actions, 0 sanctions
chain direction 100 chains: 88 agreed, 2 flipped, 10 unclear, 0 unchecked
```

Sixty of those provisions were annotated by hand before the pass ran over them, and the pass scores 0.35 precision and 0.47 recall against them, with 0.65 of the matched acts given the class the annotation gives.
Acts are matched on the label alone, which is a hard ruler: 23 of the 53 acts scored as missed have an act on the same provision whose label contains the annotated one or is contained by it, such as ký kết hợp đồng against ký kết hợp đồng liên quan đến việc người lao động đi làm việc ở nước ngoài.
The pass writes the label the clause uses and the registry asks for the label the corpus uses, and nothing in this milestone folds one into the other.
Chains score worse than that, one match against 21 missed and 29 invented, because a chain is matched on both of its labels at once and both have to survive the same ruler.
Where the two agree the arrow is right, and the blind second pass puts the same reading on 88 of the 90 chains it would commit to.

The class registry is the part that did not hold.
Of 362 acts, 191 carry a class the registry does not have, and the tail is not a tail: the run invented ORGANIZE_COMPILATION, RECEIVE_DOSSIER and TRANG_BI_KIEN_THUC_NANG_LUC, and it wrote class names in Vietnamese and in English in the same document.
They are all in the candidates queue with their evidence and their counts, which is where the seed registry said they would go, and reading that queue is a milestone of its own rather than an argument for opening the registry to the model.

The ablation is the result this milestone turns on, and it is negative.
Corpus wide act identity merged nothing: no act in this campaign is named in more than one instrument, so the identifier that spans documents changed no answer to question 24 and lost no consequence.
The join from a penalty to the act it punishes changed nothing either, because the scope produced 247 action links and no sanction link at all.
Both are real results about a campaign of two instruments on two different subjects, one on workers abroad and one on vocational training, and neither says anything yet about a corpus where a law and its decree are both in scope.

Participants are zero for a reason worth saying plainly: the concept layer has no mention report for either instrument, so the pass was offered no concepts and could not fill a role even where the provision names the party.
The role edges are built and tested in the projection, and they are unmeasured on this campaign.

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

`run` works the norm queue, and the three other model passes take the same flags for the same reasons.

```sh
luatdo concepts read --campaign labour-2025 --parallel 6
luatdo relations extract --campaign labour-2025 --parallel 6
luatdo temporal read --campaign labour-2025 --parallel 6
```

`--campaign` narrows the queue to one named slice, `--parallel` takes a worker count or `auto`, and `--dry-run` prints the queue and calls no model.
That last one exists because the honest size of a pass is not obvious until it is measured: the concept reading pass has 7,221 anchored documents and 49,175 definition units in it, and at the rate the models actually answer that is weeks of wall clock rather than an afternoon.
Each pass writes one artifact per document and recomputes its queue from what is on disk, so an interrupted pass resumes where it stopped instead of starting again, and a document that failed halfway leaves nothing behind and comes back whole.

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

There are two of them.

`labour-2025` is the labour code and every national instrument under it, in force through 2025: 1,239 documents and 36,371 extractable provisions, out of 4,563 documents in the labour subject.
The cut is on the issuing body rather than the instrument type, because a decision is central when a ministry signs it and local when a province does, and the type says nothing either way.
The 2,119 provincial documents it drops are two thirds of the subject by count and almost none of it by normative content.
That is a statement about what the pass is for, and it is in the scope definition where a reader can argue with it.

`tax-2025` is the second one and it was picked for contrast rather than for size: 887 documents and 26,048 extractable provisions of tax and customs law, cut the same way on the issuing body.
A metric measured on one domain is a metric about that domain until somebody runs a second one, and everything above was measured on labour prose, which places duties on named parties in sentences shaped the way the norm reader was built to expect.
Tax law states rates, thresholds, filing deadlines and computation rules, most of its content sits in appended schedules rather than in numbered articles, and its instruments amend each other several times a year.
Each of those is a way this pipeline can be wrong that a second labour campaign would never have shown.

A campaign owns its gold set for the same reason.
`luatdo norms gold sample --campaign tax-2025` draws from the documents in scope and writes to a file named after it, and `score --campaign tax-2025` reads that file and no other, because a tax gold set appended to a labour one reports a precision figure about neither.

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

### A cheap gate in front of the judge

Every proposed statement costs a judge call, the judge is the most expensive call in the pipeline, and most of what it reads it accepts.
The obvious saving is a cheap local model that settles the easy cases and passes the rest on, trained on the verdicts the judge has already given.
That is distillation in the ordinary sense: the model manufactures the supervision and a small model does the inference, and there is a decade of results saying it works.

```sh
luatdo verify --stage entail train    # cross validate, calibrate the bands, write the gate
luatdo verify --stage entail report   # what the stored gate would do to what is on disk
luatdo verify --stage entail show     # the bands, the fingerprint, and the heaviest features
```

The student is an averaged perceptron over hashed features of the pair, so a run on Linux, macOS and Windows produces the same weights, and every weight is a phrase somebody can read.
The teacher is 1,173 judge-labelled statements over 619 provisions, 1,010 entailed and 163 not.
Training is five folds grouped by provision, with a sixth held aside inside each fold for calibration, because two statements from one clause share their evidence and a split that puts one in training and one in test measures memory.
The bands are calibrated and never chosen: the accept edge is the widest one whose error rate on held out data stays under the budget, the reject edge likewise, and a band that no fold could place is switched off rather than guessed at.

At the sign threshold the gate agrees with the judge on 84.4 percent of the pairs, at precision 87.8 and recall 95.0.
That is the part that sounds like a result. The part that matters is what the bands do when they have to be right:

```text
budget   calls saved   true statements deleted   false ones waved through
1%       5.1%          1.2%                      4.9%
2%       6.1%          1.9%                      4.9%
5%       18.5%         7.5%                      7.4%
10%      30.7%         12.4%                     10.4%
```

Under 10 percent the accept band does not survive cross validation at all: at least one fold could not place an edge inside the budget, so the shipped gate has no accept band and its only decision is rejection.
A gate that can only reject is a gate whose entire saving comes from deleting statements without a judge ever reading them, which is the one direction where being wrong is silent.
Against the 50 human labels it agrees 82.0 percent of the time, and that number is an upper bound rather than a result, because those 50 are in its training set.

So the gate ships off. `--gate` turns it on, prints what it is calibrated to and says on the run that a rejection deletes a statement nobody read, and ten percent of its own decisions go to the judge anyway so its error rate is measured on the corpus rather than assumed from the folds.

```sh
luatdo run --campaign tax-2025 --limit 20 --gate
```

On the first live run with it switched on, over 20 provisions and 143 proposed statements, it settled nothing.
Every score fell between the two edges, the judge was called 143 times exactly as it would have been without a gate, and those calls were 255,336 of the run's 456,542 tokens.
That last number is the one this project did not have before, and it is the reason the milestone was worth running even though the gate is not: judging is 56 percent of what a pass spends.

The heaviest features say why the whole thing is weaker than it looks.

```text
+9.183 claim_cover=all
-8.896 confidence=medium
+8.687 confidence=high
+8.581 type+claim_cover=definition/all
```

`claim_cover` is how much of the statement's vocabulary appears in the quote it cites, and `confidence` is what the extractor said about its own output.
A gate built on those two is a lexical overlap detector with a self-report bolted to it, and it is not reading the provision.
The eight stage design has a place for exactly this and the place is stage 5, cheapest first, with the strong judge behind it, so a stage 5 that abstains costs nothing but the time it took to find out.

### What the pass never read

Every number above is about statements that exist. A pass that reads a clause and says nothing about it produces no statement, no rejection, and no line in any report.

```sh
luatdo omission markers --campaign tax-2025    # free, over everything already extracted
luatdo omission recovery --campaign tax-2025   # costs model calls, re-extracts a sample
```

The marker audit splits every extracted provision into sentences and looks for the five surface forms Vietnamese drafters use to state an obligation, a prohibition or a right: phải, nghiêm cấm, không được, có trách nhiệm, có quyền.
A sentence carrying one of them is covered if any trusted statement quotes it, dropped if the only statements quoting it were thrown away, and missed if nothing was ever extracted from it at all.

```text
omission       378 provisions, 977 sentences, 201 carrying a modality marker
               143 covered by a trusted statement, 50 covered only by a statement verification threw away, 8 nothing was ever extracted from
```

The audit runs on every campaign report and the sentences themselves are written to the report file, because a rate is something to feel bad about and a list is something to work through.
It is a floor and not a measurement of recall: a sentence that states a rule without one of those five words is invisible to it, and plenty do.

`omission recovery` is the other half. It re-extracts provisions in slow mode, which draws several independent candidates and unions them, and compares what that finds against what the single fast pass already had on disk.
The fast side is read from the artifacts rather than re-run, because re-running it would compare two draws of one distribution instead of comparing the graph the project actually built.
The report prints the claims only slow mode found beside the claims the fast pass had and slow mode did not keep, because slow mode judges its union again and can lose things, and a recovery rate quoted without that column reads as a free improvement.

`eval gates` is the same code the export path runs, so a campaign that fails it cannot ship by accident.
`--force` exports the Neo4j dump anyway and says in as many words that the graph is not one to publish numbers from.
It exists there because CSV files say nothing about how good they are, and a graph that fails a gate is still worth loading locally to see what is wrong with it.
The LegalRuleML export takes the same flag and refuses it, which is the section below.

The same flag scopes the dump as well as the gates:

```sh
luatdo export neo4j --campaign labour-2025
```

The whole corpus is not the unit anybody works with.
A campaign dump holds the documents in scope and nothing that points outside them: the citations with one end elsewhere are gone, so are the term uses scoped to an excluded instrument, the concepts nothing left reaches, and the amendment events whose every effect landed outside.
That last part is not tidiness. `neo4j-admin` refuses an entire import over a single relationship row naming a node the node files do not declare, so an edge half in the dump is not a smaller edge, it is a dump that will not load.

There are two ways into a database and they are checked against each other rather than trusted separately.
`import.sh` and `import.cmd` run `neo4j-admin` over the CSV files, which is the fast path for a fresh database and wants the dump directory to be writable because the importer leaves its report there.
`--merge` writes the same projection over Bolt into a database that is already running, and `--check` counts what is there against what the store says should be there and names every counter that disagrees.
On the labour campaign the two paths land on the same graph, 136289 nodes and 144093 relationships, the offline one in 12 seconds and the incremental one in about two minutes.

The baselines are the two systems this project would have to beat to be worth building: a search index with a citation table, and flat triple extraction over the same scope.
Both answer 2 of the 23 competency questions from a layer that means what it says.
The ablations remove one layer at a time from the full system, and the concept layer is worth 14 questions, the temporal layer 7, and conditions and exceptions 3.
Those are counts of questions the design can answer, not of answers checked against a gold set, and the report says which it is.

## Asking it a question

Everything above builds a graph and measures how it was built.
This is the part where a person types a question in Vietnamese and gets sentences back, and the whole of it turns on one rule: the answerer may assert nothing that a trusted statement does not support.

```sh
luatdo retrieve --k 5 --explain "Người sử dụng lao động phải trả lương khi nào?"
luatdo answer --k 6 "Cơ quan thuế phải giải quyết khiếu nại về thuế trong thời hạn bao lâu?"
luatdo statute --mode graph --generate --out run.json
```

Scope is applied before anything is ranked, and that ordering is the point rather than an optimisation.
`--doc`, `--subject`, `--component`, `--kind`, `--date` and `--statements` each narrow the candidate set, and filtering after the ranking would spend k on components the caller already said were out of scope.
Every filter prints how many components it dropped and why, because a scope that quietly empties itself looks exactly like a corpus with nothing in it.

The index is multi aspect, and four of the nine aspects are built from the graph rather than from the words.
Text and heading are what the drafter wrote, term and citation come from the definition and citation layers, and bearer, action, condition, deadline and sanction are assembled from the trusted statements extracted out of that component.
That is what the graph buys here.
An article stem that says `phải` and never names who is bound is reachable through the bearer its own statements carry, and no lexical index over the prose alone can do that.
The weights are not tuned against the benchmark below, and they will not be, because tuning them on the same twenty six questions that score them turns a measurement into a fit.

Similarity is lexical.
It is BM25 over Vietnamese syllables and syllable bigrams, per aspect, combined by weight.
There is no embedding route in this repository and no embedding code anywhere in it, so nothing here should be read as semantic search, and the ceiling is visible in the results: a long question shares many ordinary words with a long provision, and BM25 will prefer that provision over the short clause that actually answers it.

The unit of retrieval is a component, and what may be quoted from it is its span, which is its own words followed by the words of everything nested under it.
That distinction cost six claims on the first generation run.
`Điều 16 khoản 2` of the customs law is the stem `Khi làm thủ tục hải quan, công chức hải quan phải:` and its four duties live in `điểm a` through `điểm d` as separate components, so a model handed the clause text alone quoted a duty that the clause does not contain and the grounding check deleted a correct sentence.
Ranking still uses the component's own words, quoting uses the span, and generation went from 0.682 to 0.818 with the drops going from six to zero.

A question can be asked at a date, and this is where the temporal layer says out loud that it does not know.

```text
scope  date  1670 to 4  2006-06-01  1125 of those had no recorded interval and were dropped for that rather than for being out of force; 541 rest on an amendment nobody has read, which is the temporal layer saying it does not know rather than saying no, and 0 of those were admitted
```

Of the 3563 validity intervals on this corpus, 3252 come from an amendment that was detected and never read, 311 come from a commencement date, and none come from the version graph.
A component whose only interval is an unread amendment is neither in force nor out of it as far as this layer is concerned, so a strict date filter drops it, which is why four components survive above.
`--assume-unread` admits those wordings and says in the same line that it is assuming an unread amendment left the wording alone, which is a guess wearing a label rather than a fact.
It takes the same query from 4 components in scope to 335 and puts `vn:law:2001:29-2001-qh10:article-17` first, which is the right answer at that date.

The answerer is handed the retrieved components, their titles, their spans and the trusted statements extracted from them, and it may write nothing else.
Every sentence it produces carries a component identifier, a statement identifier and a quote, and a sentence whose component was not retrieved, whose statement does not exist, or whose quote is not in that component's span is dropped by name rather than repaired.

```text
1. Cơ quan thuế phải giải quyết khiếu nại về thuế trong thời hạn mười lăm ngày, kể từ ngày nhận được khiếu nại.
   vn:law:1998:05-1998-qh10:article-22:clause-1
   trích: Cơ quan thuế nhận được khiếu nại về thuế phải giải quyết trong thời hạn mười lăm ngày, kể từ ngày nhận được khiếu nại;
2 of 2 sentences survived the check, 1 calls, 4729 tokens
```

Refusal is a first class answer and not a failure path.

```text
từ chối: Danh sách điều khoản không có quy định về mức lương tối thiểu vùng hiện nay nên không đủ cơ sở để trả lời.
0 of 0 sentences survived the check, 1 calls, 1697 tokens
```

### The statute benchmark

`eval/statute.json` is twenty six questions over the nine documents the trusted store holds statements for, and it is compiled into the binary so that a figure in a report cannot have come from a file somebody edited locally.
Twenty two are answerable across seven families, deadline, bearer, definition, prohibition, right, sanction and temporal, and four are not answerable from this corpus at all, so a system that never refuses is caught rather than rewarded.
Gold is a list of component identifiers and never an answer string, because what is being measured is whether the system reaches the provision a lawyer would cite, and comparing prose against prose measures the wording as much as the retrieval.
One question is asked twice, the same Vietnamese sentence at 2003-01-01 and at 2006-06-01, with different gold at each date.

Three scores are reported and there is nowhere to put a fourth that combines them.
Construction asks whether the graph even holds what the question needs, retrieval asks whether the ranking found it, and generation asks what the model did with it, and those three fail for different reasons and are repaired by different code.

```text
construction over 22 answerable questions, 24 gold components
  gold component carries a trusted statement  1.000 (24 of 24)
  gold component exists                       1.000 (24 of 24)
  note: 22 of 22 questions have every gold component built

retrieval over top 8, 22 answerable questions
  gold components retrieved                   0.833 (20 of 24)
  gold components retrieved, of those built   0.833 (20 of 24)
  questions with at least one gold component  0.818 (18 of 22)
  questions with every gold component         0.818 (18 of 22)
  note: mean reciprocal rank 0.677 over 22 questions
  note: 168 distinct components put in play, 7.6 per question at k=8
  note: no gold component at any rank for questions 10, 12, 21, 22

generation over 22 answerable, 4 unanswerable questions
  answerable questions with a grounded claim  0.818 (18 of 22)
  answered questions citing a gold component  0.889 (16 of 18)
  unanswerable questions refused              1.000 (4 of 4)
  note: 27 claims made, 27 survived the grounding check, 22 cited a gold component
  note: 0 claims cited a component or a statement that does not exist
```

Construction being perfect is what makes the other two readable.
Every gold component exists and every one of them carries a trusted statement, so each of those four retrieval misses is retrieval's own failure and cannot be an extraction gap wearing a retrieval costume.
Recall is still reported twice, over all gold and over built gold, because on the next corpus those two will differ and a single recall number would hide which layer to fix.

Two of the eighteen answered questions cited a component that is not gold, and every claim in both was a real quote from a real provision that does not answer the question.
That is the failure the citation rate exists to catch, it is invisible to a grounding check, and it is why both numbers are printed.

The six failures split three ways and each way wants different work.

Questions 10 and 12 are lexical misses.
Both ask about a short clause using long ordinary Vietnamese, BM25 prefers the long provisions that share those ordinary words, the gold clause never enters the top eight, and the answerer then refuses because nothing it was handed supports an answer.
The repair is an embedding route, not a weight nudged until the benchmark moves.

Questions 14 and 15 retrieved their gold and were refused anyway, which was not expected and is the most interesting result in the run.
Both ask what a party is prohibited from doing, both prohibitions are lists, and the trusted statements cover part of each list.
The model said so in as many words, that the provisions it was given carry no verified statement about the rest of the list, and declined to answer rather than answer from the part it had.
A partial answer to `what is a branch not allowed to do` is a wrong answer that reads as a complete one, so the refusal is the behaviour this design was asking for, and it also says that extraction recall inside a list is now the thing standing between this corpus and two more answers.

Questions 21 and 22 are the temporal pair, and they fail on the same cause from opposite sides.
Under a strict date filter only four components survive at either date, all of them clauses of the 1998 amending law, so the answerer confidently quoted a real provision about customs declarations that answers a different question.
`--assume-unread` fixes 21, lifting gold recall from 0.833 to 0.875 and mean reciprocal rank from 0.677 to 0.722, and that is the reason the flag prints its assumption on every use.
Question 22 stays broken with the flag on, because the version graph is empty: at 2006-06-01 the 2001 original and the 2005 clause amending it are both candidates, nothing recorded that the second replaced the first, and the original outranks the amendment on wording alone.
That is a finding about the temporal layer rather than a number for the retriever to fix.

### Two baselines

The same twenty six questions were asked with flat chunk retrieval over the same text, and with no retrieval at all.

```text
                              graph      flat       none
gold components retrieved     0.833      0.958      0.000
mean reciprocal rank          0.677      0.242      0.000
components put in play         7.6       53.1        0.0
grounded answers              0.818      0.091      0.000
claims made, claims kept      27, 27      6, 2       0, 0
unanswerable refused          4 of 4     4 of 4     4 of 4
```

Flat retrieval finds more gold than the graph does and that number is real, but it is not the same measurement.
Eight flat chunks straddle 53 components on average against the graph's 7.6, so the baseline is being handed seven times as many chances at the same k, which is what the components put in play row is there to say.
Its mean reciprocal rank is 0.242 against 0.677, which is the same fact stated from the other side: it covers the answer without locating it.

The generation column is the part that was not expected.
Flat retrieval produced a grounded answer on 2 of 22 questions because a chunk is a window rather than a provision, the trusted statements hang off components and not off windows, and an answerer that may only assert what a statement supports has nothing to assert and refuses.
That is not the baseline being handled unfairly, it is the shape of the claim constraint: the graph is what makes grounded generation possible at all, and without it the honest behaviour is silence.
With no retrieval the model made zero claims across all twenty six questions rather than answering from what it happens to know about Vietnamese law, which is the constraint working in the case where it would have been easiest to break.

Each of the three runs is 26 model calls.
Graph spent 146,840 tokens, flat 146,922, and no retrieval 9,279.
Cost reports as unavailable on all three, because the route these ran through is a subscription with no rate card rather than a metered endpoint.

Construction and retrieval need no model at all, so `luatdo statute` without `--generate` reproduces both of those tables on a laptop with no endpoint configured.

## Two other formats

Choosing a property graph took on an interoperability debt, and this is where it gets paid.
A norm has a bearer, an action, an object, conditions, exceptions, a deadline, a sanction, a modality, a confidence and an evidence quote, which is one node with properties in Neo4j and about a dozen triples in RDF, and carrying that shape through the working model would have been a cost with no return while nobody is federating with us.

```sh
luatdo export neo4j --campaign labour-2025
luatdo export rdf
```

`export rdf` reads the CSV dump and not the store, and that ordering is the design rather than a shortcut.
Generating RDF from the store would be a second working model, it would drift from the graph the moment either changed, and the first symptom would be somebody quoting a figure from the RDF that the database disagrees with.
Reading the dump means the RDF can hold nothing the graph does not, by construction rather than by discipline, and it also means there is no campaign flag here: the scoping decision was made one step earlier by the export that wrote the dump.
On the labour campaign that is 769,249 triples from 136,289 nodes and 144,093 edges across 27 files in two seconds, with 8,337 edges reified because they carry properties a plain triple has nowhere to put.

Two files come out and the split matters.
`graph.nt` is the data in our vocabulary, and `vocabulary.ttl` is the claim that some of our terms are somebody else's.
SKOS and Dublin Core are reused where the definition in the other vocabulary is the definition we would have written, and ELI is stated as `rdfs:subClassOf` rather than `owl:equivalentClass`, which is the weaker and the honest claim: every Document of ours is a legal resource, and we say nothing about whether every legal resource is one of ours.
Whether a Vietnamese thông tư is an `eli:LegalResource` is a reading of somebody else's specification, so it lives in the file a consumer can decline to load.

LegalRuleML is the other direction, and it is the one export that is gated.

```sh
luatdo export legalruleml --campaign labour-2025
```

A deontic operator in LegalRuleML is a claim that a rule engine may act on this.
Every statement here was read out of Vietnamese prose by a language model and checked by another one, the pass reports precision rather than proof, and wrapping that in an element named `lrml:Obligation` does not make it more certain than it was in the JSON it came from.
It does make it look more certain, and a formalism is read as a guarantee by people who never saw how it was produced.

So the release gates decide and there is no way past them.
`--force` is accepted by the command and does nothing, which is deliberate: the Neo4j dump says nothing on its face about how good it is and is loaded by the person who made it, and a rule base says something on every line and is read by somebody who was not there.
On the corpus as it stands this refuses, and the refusal is the correct answer rather than a placeholder.

```text
evidence           pass     1.000 (335 of 335)  floor 1.000
bearer-placement   pass     1.000 (273 of 273)  floor 0.900
judge-agreement    FAIL     -0.120 (50 of 50)  floor 0.400
export blocked:
  judge-agreement is -0.120 and the floor is 0.400, which protects: the precision figures in every report were produced by this judge
```

Three more rules hold in the writer itself.
Only records the judge entailed or a person approved are written, and an untrusted record is refused by identifier rather than dropped quietly, because a caller who asked for their campaign and got a shorter file would believe they exported it.
A definition becomes an `lrml:ConstitutiveStatement` and never gets a deontic operator, and sanctions, procedure steps and exceptions are counted and named as skipped rather than invented as statements, since each of those is part of another norm and is written on that norm.
What the pipeline did not formalise is carried as text under a named relation instead of being turned into a predicate: a condition becomes a `ruleml:Atom` over `luatdo:condition` holding the words the drafter used, an exception the same under `ruleml:Naf`, and the file says in its own header that a consumer cannot evaluate the qualification and must not treat the rule as unconditional.
A deadline is the one qualification that does come apart, and its calendar survives into the file, because five working days read as five days is a wrong answer that looks like a right one.

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
