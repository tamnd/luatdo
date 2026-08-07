---
license: cc-by-4.0
language:
  - vi
task_categories:
  - question-answering
  - text-retrieval
tags:
  - legal
  - knowledge-graph
  - neo4j
  - vietnamese
pretty_name: Vietnamese legal knowledge graph
size_categories:
  - 10M<n<100M
configs:
{{- range .Tables }}
  - config_name: {{ .Name }}
    data_files:
      - split: train
        path: data/{{ .Name }}/train-*.parquet
{{- end }}
---

# luatdo-graph

A knowledge graph over Vietnamese law, built from about 128,000 documents by [luatdo](https://github.com/tamnd/luatdo).

This is the result of running the pipeline, published so that nobody has to run it again.
The pipeline takes days and several hundred dollars of model calls, and the output is the same for everyone.

## What is in it

| | |
| --- | --- |
| Nodes | {{ .Nodes }} |
| Relationships | {{ .Relationships }} |
| Node tables | {{ .NodeTables }} |
| Relationship tables | {{ .RelationshipTables }} |
| Node labels | {{ .LabelCount }} |
| Parquet | {{ .ParquetSize }} across {{ .Files }} files |
| Neo4j archive | {{ .ArchiveSize }} gzipped, about {{ .UnpackedSize }} unpacked |

The labels present in the data are {{ .Labels }}.
That is counted from the rows rather than read off the schema, which defines more labels than the extraction has so far filled.

The graph is not a shadow of the document structure.
Definitions, normative statements, temporal versions and act chains are all extracted by language models reading the text, with the provision each claim came from recorded on the claim.

## Two shapes of the same graph

The Parquet files under `data/` are the graph as tables, one directory per table, and they are what the viewer above is showing.
Node tables have an `id` and a `labels` list.
Relationship tables have a `start_id`, an `end_id` and a `type`, and the endpoints join to node `id`.
Every table is typed, so numbers are numbers and an absent value is null rather than an empty string.

`{{ .Archive }}` is the same graph as a Neo4j offline import set, which is what to download if you want to run queries over it rather than read it as tables.
It is not a Neo4j database directory, so it does not depend on a Neo4j version.

## Reading the tables

```python
import pandas as pd

url = "hf://datasets/{{ .Repo }}/data"
docs = pd.read_parquet(f"{url}/documents/train-00000-of-00001.parquet")
```

Or with the datasets library, one config per table:

```python
from datasets import load_dataset

docs = load_dataset("{{ .Repo }}", "documents", split="train")
```

Or in duckdb, which will read the whole set of shards from a glob and join across tables:

```sql
select d.title, count(*) as citations
from 'data/documents/train-*.parquet' d
join 'data/cites/train-*.parquet' c on c.start_id = d.id
group by 1 order by 2 desc limit 10;
```

## Loading it into Neo4j

```sh
luatdo neo4j install
```

That downloads the archive, checks it against a pinned checksum, imports it into a local Neo4j, and waits until the database answers a query.
It works the same on Linux, macOS and Windows.

By hand, with any Neo4j 5 and a container runtime:

```sh
curl -fLO https://huggingface.co/datasets/{{ .Repo }}/resolve/main/{{ .Archive }}
tar -xzf {{ .Archive }}
docker volume create luatdo-neo4j-data
docker run --rm -v "$PWD/neo4j:/import:ro" -v luatdo-neo4j-data:/data -w /import \
  neo4j:5.26 sh ./import.sh --report-file=/data/import.report
docker run -d --name luatdo-neo4j -p 7474:7474 -p 7687:7687 \
  -v luatdo-neo4j-data:/data \
  -e NEO4J_AUTH=neo4j/luatdo-local \
  -e NEO4J_initial_dbms_default__database=luatdo \
  neo4j:5.26
```

`import.sh` passes anything you give it through to `neo4j-admin`, where a repeated option takes the last value.
That is how the report is moved off the read only mount above.
The export belongs to whoever downloaded it and the image runs as its own user, so on a rootful docker host the report is the one file the import cannot write.

The database name matters.
Neo4j Community runs one user database, and the import writes into `luatdo`, so a server started without that setting comes up healthy and holds nothing.

## The tables

Nodes:

| Table | Rows | Size | Labels |
| --- | --- | --- | --- |
{{- range .NodeList }}
| `{{ .Name }}` | {{ .Rows }} | {{ .Size }} | {{ .Labels }} |
{{- end }}

Relationships:

| Table | Rows | Size |
| --- | --- | --- |
{{- range .RelationshipList }}
| `{{ .Name }}` | {{ .Rows }} | {{ .Size }} |
{{- end }}

## Versions

| Version | What changed |
| --- | --- |
| 2026.08.1 | The import scripts pass their arguments through to `neo4j-admin`, so the report can be moved off a read only mount. Later republished as Parquet as well, with the archive unchanged, so there is nothing to download again |
| 2026.08 | First publication |

## Versioning

The dataset is versioned apart from the code, as `luatdo-graph-YYYY.MM.tar.gz`.
A corpus grows when somebody runs the pipeline over more of it, and the tool changes when somebody changes the tool, and tying the two together would mean re-uploading half a gigabyte to publish a one line fix.
Each luatdo release records the dataset version it was built against.

## Provenance and limits

The source documents come from public Vietnamese legal corpora on the Hub, and the extraction on top of them is machine made.
Every extracted claim carries the provision it was read out of, so anything here can be checked against its source text, and anything here can be wrong.
Do not treat it as legal advice.

Some layers are much further along than others, and the row counts above are the honest account of which.
The document, provision, text version, citation, definition, subject, norm, temporal and act layers are populated.
The concept layer is not.
Concept mentions, concept relations, instance links, concept merges and the recorded conflicts are all empty, and the concept table itself holds a few dozen rows rather than the thousands the pipeline is capable of producing.
Those layers sit behind a human review queue that has not been worked through, so a query about concepts will return nothing rather than something wrong.

A table with no rows is published as an empty table rather than left out.
A layer that came out empty and a layer that was never exported are different things, and the file list is the only place that difference shows.

## License

CC BY 4.0.
The underlying legal texts are Vietnamese government documents.
